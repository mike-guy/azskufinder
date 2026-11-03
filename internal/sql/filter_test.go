package sql

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParsers(t *testing.T) {
	t.Parallel()

	t.Run("tiers", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name    string
			parse   func(string) ([]Tier, error)
			in      string
			want    []Tier
			wantErr string
		}{
			{name: "empty", parse: ParseVCoreTiers, in: " , "},
			{name: "deduplicated, any case", parse: ParseVCoreTiers, in: "Hyperscale, general-purpose,hyperscale", want: []Tier{TierHyperscale, TierGeneralPurpose}},
			{name: "DTU tiers", parse: ParseDTUTiers, in: "basic,premium", want: []Tier{TierBasic, TierPremium}},
			{name: "a DTU tier is not a vCore tier", parse: ParseVCoreTiers, in: "standard", wantErr: `unknown tier "standard": use one of general-purpose, business-critical, hyperscale`},
			{name: "a vCore tier is not a DTU tier", parse: ParseDTUTiers, in: "hyperscale", wantErr: `unknown tier "hyperscale": use one of basic, standard, premium`},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				got, err := tc.parse(tc.in)
				checkErr(t, err, tc.wantErr)

				if diff := cmp.Diff(tc.want, got); diff != "" {
					t.Errorf("parse(%q) mismatch (-want +got):\n%s", tc.in, diff)
				}
			})
		}
	})

	t.Run("hardware", func(t *testing.T) {
		t.Parallel()

		if got, err := ParseHardware("dc-series,premium-series-memory-optimised"); err != nil || len(got) != 2 {
			t.Errorf("ParseHardware() = %v, %v, want two values", got, err)
		}

		if _, err := ParseHardware("fsv2"); err == nil {
			t.Error("ParseHardware(fsv2) error = nil, want an error")
		}
	})

	t.Run("single values", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name    string
			parse   func(string) (string, error)
			in      string
			want    string
			wantErr string
		}{
			{name: "compute any", parse: str(ParseCompute), in: "any", want: ""},
			{name: "compute", parse: str(ParseCompute), in: " Serverless ", want: "serverless"},
			{name: "compute unknown", parse: str(ParseCompute), in: "spot", wantErr: `unknown compute model "spot": use one of provisioned, serverless, any`},
			{name: "backup any", parse: str(ParseBackupRedundancy), in: "", want: ""},
			{name: "backup", parse: str(ParseBackupRedundancy), in: "geo-zone", want: "geo-zone"},
			{name: "licence default", parse: str(ParseLicence), in: "", want: "included"},
			{name: "licence ahb", parse: str(ParseLicence), in: "AHB", want: "ahb"},
			{name: "licence unknown", parse: str(ParseLicence), in: "byol", wantErr: `unknown licence "byol": use one of included, ahb`},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				got, err := tc.parse(tc.in)
				checkErr(t, err, tc.wantErr)

				if got != tc.want {
					t.Errorf("parse(%q) = %q, want %q", tc.in, got, tc.want)
				}
			})
		}
	})
}

// str adapts a parser of a string type to return a plain string.
func str[T ~string](parse func(string) (T, error)) func(string) (string, error) {
	return func(s string) (string, error) {
		v, err := parse(s)

		return string(v), err
	}
}

func checkErr(t *testing.T, err error, want string) {
	t.Helper()

	switch {
	case want == "" && err != nil:
		t.Fatalf("error = %v, want none", err)
	case want != "" && (err == nil || !strings.Contains(err.Error(), want)):
		t.Fatalf("error = %v, want one containing %q", err, want)
	}
}

var allBackups = []BackupRedundancy{BackupLocal, BackupZone, BackupGeo, BackupGeoZone}

func TestParseVCore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		in     serviceObjective
		want   VCoreDatabase
		wantOK bool
	}{
		{
			name: "provisioned", in: objective("GP_Gen5_3", 3), wantOK: true,
			want: VCoreDatabase{
				Name: "GP_Gen5_3", SKU: "GP_Gen5", Tier: TierGeneralPurpose, Compute: ComputeProvisioned,
				Hardware: HardwareStandard, Family: "Gen5", VCores: 3, MemoryGiB: new(15.3), MaxStorageGiB: new(1024.0), ZoneRedundant: true,
				BackupRedundancy: allBackups, QuotaNeeded: 3, Quota: []QuotaHeadroom{},
			},
		},
		{
			name: "serverless memory is not known",
			in: objective("GP_S_Gen5_2", 2, func(o *serviceObjective) {
				o.skuName, o.computeModel = "GP_S_Gen5", "Serverless"
			}),
			wantOK: true,
			want: VCoreDatabase{
				Name: "GP_S_Gen5_2", SKU: "GP_S_Gen5", Tier: TierGeneralPurpose, Compute: ComputeServerless,
				Hardware: HardwareStandard, Family: "Gen5", VCores: 2, MaxStorageGiB: new(1024.0), ZoneRedundant: true,
				BackupRedundancy: allBackups, QuotaNeeded: 2, Quota: []QuotaHeadroom{},
			},
		},
		{
			name: "hyperscale storage is fixed",
			in: objective("HS_DC_2", 2, func(o *serviceObjective) {
				o.skuName, o.edition, o.family, o.maxStorageGiB, o.backup = "HS_DC", "Hyperscale", "DC", nil, nil
			}),
			wantOK: true,
			want: VCoreDatabase{
				Name: "HS_DC_2", SKU: "HS_DC", Tier: TierHyperscale, Compute: ComputeProvisioned,
				Hardware: HardwareDC, Family: "DC", VCores: 2, MemoryGiB: new(9.0), MaxStorageGiB: new(131072.0), ZoneRedundant: true,
				BackupRedundancy: []BackupRedundancy{}, QuotaNeeded: 2, Quota: []QuotaHeadroom{},
			},
		},
		{
			name: "premium-series memory optimised", in: objective("HS_MOPRMS_4", 4, func(o *serviceObjective) {
				o.skuName, o.edition, o.family = "HS_MOPRMS", "Hyperscale", "8IH"
			}),
			wantOK: true,
			want: VCoreDatabase{
				Name: "HS_MOPRMS_4", SKU: "HS_MOPRMS", Tier: TierHyperscale, Compute: ComputeProvisioned,
				Hardware: HardwarePremiumMemoryOp, Family: "8IH", VCores: 4, MemoryGiB: new(40.8), MaxStorageGiB: new(1024.0), ZoneRedundant: true,
				BackupRedundancy: allBackups, QuotaNeeded: 4, Quota: []QuotaHeadroom{},
			},
		},
		{
			name: "unknown hardware has no memory", in: objective("GP_Fsv2_8", 8, func(o *serviceObjective) { o.family = "Fsv2" }), wantOK: true,
			want: VCoreDatabase{
				Name: "GP_Fsv2_8", SKU: "GP_Gen5", Tier: TierGeneralPurpose, Compute: ComputeProvisioned,
				Hardware: hardwareOther, Family: "Fsv2", VCores: 8, MaxStorageGiB: new(1024.0), ZoneRedundant: true,
				BackupRedundancy: allBackups, QuotaNeeded: 8, Quota: []QuotaHeadroom{},
			},
		},
		{name: "a DTU objective", in: dtuObjective("S3", "Standard", 100)},
		{name: "edition not offered", in: objective("System0", 1, func(o *serviceObjective) { o.edition = "System" })},
		{name: "DTU unit in a vCore tier", in: objective("GP_odd", 1, func(o *serviceObjective) { o.unit = "DTU" })},
		{name: "no size", in: objective("GP_Gen5_0", 0)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := parseVCore(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("parseVCore(%s) ok = %t, want %t", tc.in.name, ok, tc.wantOK)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("parseVCore(%s) mismatch (-want +got):\n%s", tc.in.name, diff)
			}
		})
	}
}

func TestParseDTU(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		in     serviceObjective
		want   DTUDatabase
		wantOK bool
	}{
		{
			name: "quota rounds up", in: dtuObjective("Basic", "Basic", 5), wantOK: true,
			want: DTUDatabase{
				Name: "Basic", Tier: TierBasic, DTUs: 5, MaxStorageGiB: new(1024.0),
				BackupRedundancy: allBackups, QuotaNeeded: 1, Quota: []QuotaHeadroom{},
			},
		},
		{
			name: "premium", in: dtuObjective("P2", "Premium", 250), wantOK: true,
			want: DTUDatabase{
				Name: "P2", Tier: TierPremium, DTUs: 250, MaxStorageGiB: new(1024.0), ZoneRedundant: true,
				BackupRedundancy: allBackups, QuotaNeeded: 3, Quota: []QuotaHeadroom{},
			},
		},
		{name: "a vCore objective", in: objective("GP_Gen5_2", 2)},
		{name: "vCore unit in a DTU tier", in: objective("S_odd", 2, func(o *serviceObjective) { o.edition = "Standard" })},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := parseDTU(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("parseDTU(%s) ok = %t, want %t", tc.in.name, ok, tc.wantOK)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("parseDTU(%s) mismatch (-want +got):\n%s", tc.in.name, diff)
			}
		})
	}
}

func TestMatchesVCore(t *testing.T) {
	t.Parallel()

	gp4, _ := parseVCore(objective("GP_Gen5_4", 4))
	serverless, _ := parseVCore(objective("GP_S_Gen5_4", 4, func(o *serviceObjective) { o.computeModel = "Serverless" }))

	tests := []struct {
		name string
		db   VCoreDatabase
		f    VCoreFilter
		want bool
	}{
		{name: "no requirements", db: gp4, want: true},
		{name: "tier", db: gp4, f: VCoreFilter{Tiers: []Tier{TierHyperscale, TierGeneralPurpose}}, want: true},
		{name: "other tier", db: gp4, f: VCoreFilter{Tiers: []Tier{TierHyperscale}}},
		{name: "compute", db: serverless, f: VCoreFilter{Compute: ComputeProvisioned}},
		{name: "hardware", db: gp4, f: VCoreFilter{Hardware: []Hardware{HardwareStandard}}, want: true},
		{name: "other hardware", db: gp4, f: VCoreFilter{Hardware: []Hardware{HardwareDC}}},
		{name: "vCores minimum", db: gp4, f: VCoreFilter{VCores: new(2)}, want: true},
		{name: "vCores exact", db: gp4, f: VCoreFilter{VCores: new(2), ExactMatch: true}},
		{name: "memory exact", db: gp4, f: VCoreFilter{MemoryGiB: new(20.4), ExactMatch: true}, want: true},
		{name: "unknown memory never matches", db: serverless, f: VCoreFilter{MemoryGiB: new(1.0)}},
		{name: "storage fits", db: gp4, f: VCoreFilter{StorageGiB: new(1024.0)}, want: true},
		{name: "storage too big", db: gp4, f: VCoreFilter{StorageGiB: new(2048.0)}},
		{name: "zone redundant", db: gp4, f: VCoreFilter{ZoneRedundant: true}, want: true},
		{name: "backup redundancy", db: gp4, f: VCoreFilter{BackupRedundancy: BackupGeoZone}, want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := matchesVCore(tc.db, tc.f); got != tc.want {
				t.Errorf("matchesVCore(%s, %+v) = %t, want %t", tc.db.Name, tc.f, got, tc.want)
			}
		})
	}
}

func TestMatchesDTU(t *testing.T) {
	t.Parallel()

	s3, _ := parseDTU(dtuObjective("S3", "Standard", 100))
	unknownStorage, _ := parseDTU(dtuObjective("S4", "Standard", 200))
	unknownStorage.MaxStorageGiB = nil

	tests := []struct {
		name string
		db   DTUDatabase
		f    DTUFilter
		want bool
	}{
		{name: "no requirements", db: s3, want: true},
		{name: "tier", db: s3, f: DTUFilter{Tiers: []Tier{TierPremium}}},
		{name: "DTUs minimum", db: s3, f: DTUFilter{DTUs: new(50)}, want: true},
		{name: "DTUs exact", db: s3, f: DTUFilter{DTUs: new(50), ExactMatch: true}},
		{name: "unknown storage never matches", db: unknownStorage, f: DTUFilter{StorageGiB: new(1.0)}},
		{name: "zone redundancy not offered", db: s3, f: DTUFilter{ZoneRedundant: true}},
		{name: "backup redundancy", db: s3, f: DTUFilter{BackupRedundancy: BackupLocal}, want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := matchesDTU(tc.db, tc.f); got != tc.want {
				t.Errorf("matchesDTU(%s, %+v) = %t, want %t", tc.db.Name, tc.f, got, tc.want)
			}
		})
	}
}

// vcoreModel is the model FindVCore uses, without prices.
func vcoreModel(f VCoreFilter) model[VCoreDatabase] {
	return model[VCoreDatabase]{
		parse:   parseVCore,
		matches: func(d VCoreDatabase) bool { return matchesVCore(d, f) },
		quota:   func(d *VCoreDatabase) (int64, *[]QuotaHeadroom) { return d.QuotaNeeded, &d.Quota },
		name:    func(d VCoreDatabase) string { return d.Name },
		price:   func(*VCoreDatabase) {},
	}
}

func TestAssess(t *testing.T) {
	t.Parallel()

	caps := dbCaps{
		location: deployable,
		objectives: []serviceObjective{
			objective("GP_Gen5_2", 2),
			objective("GP_Gen5_8", 8),
			objective("GP_Gen5_4", 4, func(o *serviceObjective) { o.availability = availability{reason: "disabled"} }),
			objective("System0", 1, func(o *serviceObjective) { o.edition = "System" }),
			dtuObjective("S3", "Standard", 100),
		},
	}

	quotas := map[string]quota{regionalVCores: {Used: 145, Limit: 150}}

	var summary Summary

	got, warnings := assess(caps, quotas, vcoreModel(VCoreFilter{}), &summary, discard)

	names := make([]string, len(got))
	for i, d := range got {
		names[i] = d.Name
	}

	if diff := cmp.Diff([]string{"GP_Gen5_2"}, names); diff != "" {
		t.Errorf("assess() names mismatch (-want +got):\n%s", diff)
	}

	// System0 and S3 are not vCore objectives, so they are not considered.
	wantSummary := Summary{Considered: 3, Matched: 3, Restricted: 1, NoQuota: 1}
	if diff := cmp.Diff(wantSummary, summary); diff != "" {
		t.Errorf("assess() summary mismatch (-want +got):\n%s", diff)
	}

	if len(warnings) != 0 {
		t.Errorf("assess() warnings = %v, want none", warnings)
	}

	if diff := cmp.Diff([]QuotaHeadroom{{Name: regionalVCores, Limit: 150, Used: 145, Free: 5}}, got[0].Quota); diff != "" {
		t.Errorf("assess() quota mismatch (-want +got):\n%s", diff)
	}
}

func TestAssess_quotaMissing(t *testing.T) {
	t.Parallel()

	caps := dbCaps{location: deployable, objectives: []serviceObjective{objective("GP_Gen5_2", 2)}}

	var summary Summary

	got, warnings := assess(caps, map[string]quota{}, vcoreModel(VCoreFilter{}), &summary, discard)

	if len(got) != 1 || len(got[0].Quota) != 0 {
		t.Errorf("assess() = %+v, want the objective kept, unchecked", got)
	}

	if len(warnings) != 1 || warnings[0].Code != WarningQuotaMissing || !strings.Contains(warnings[0].Message, regionalVCores) {
		t.Errorf("assess() warnings = %v, want one quota-missing warning", warnings)
	}
}

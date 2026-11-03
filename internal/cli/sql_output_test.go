package cli

import (
	"bytes"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/mike-guy/azskufinder/internal/output"
	"github.com/mike-guy/azskufinder/internal/sql"
)

func sqlCommon(format string) commonOptions {
	return commonOptions{SubscriptionID: testSubscriptionID, Location: "uksouth", Output: format, Currency: "GBP", Limit: 4}
}

func regionalQuota(free int64) []sql.QuotaHeadroom {
	return []sql.QuotaHeadroom{{Name: "RegionalVCoreQuotaForSQLDBAndDW", Limit: 150, Used: 150 - free, Free: free}}
}

var sampleBackup = []sql.BackupRedundancy{sql.BackupLocal, sql.BackupZone, sql.BackupGeo}

// sampleVCoreResult covers each way a cell can be shown: provisioned and
// serverless memory, Hyperscale storage in TiB, no quota entry and no price.
func sampleVCoreResult() sql.DBResult[sql.VCoreDatabase] {
	dbs := []sql.VCoreDatabase{
		{
			Name: "GP_Gen5_4", SKU: "GP_Gen5", Tier: sql.TierGeneralPurpose, Compute: sql.ComputeProvisioned,
			Hardware: sql.HardwareStandard, Family: "Gen5", VCores: 4, MemoryGiB: new(20.4), MaxStorageGiB: new(1024.0), ZoneRedundant: true,
			BackupRedundancy: sampleBackup, QuotaNeeded: 4, Quota: regionalQuota(146),
			Pricing: sql.Pricing{Hourly: new(0.574), Monthly: new(419.02), ComputeHourly: new(0.574)},
		},
		{
			Name: "HS_S_Gen5_4", SKU: "HS_S_Gen5", Tier: sql.TierHyperscale, Compute: sql.ComputeServerless,
			Hardware: sql.HardwareStandard, Family: "Gen5", VCores: 4, MaxStorageGiB: new(131072.0), ZoneRedundant: true,
			BackupRedundancy: sampleBackup, QuotaNeeded: 4, Quota: []sql.QuotaHeadroom{},
			Pricing: sql.Pricing{Hourly: new(1.4135), Monthly: new(1031.84), ComputeHourly: new(1.4135)},
		},
		{
			Name: "BC_DC_4", SKU: "BC_DC", Tier: sql.TierBusinessCritical, Compute: sql.ComputeProvisioned,
			Hardware: sql.HardwareDC, Family: "DC", VCores: 4, MemoryGiB: new(18.0), MaxStorageGiB: new(768.0),
			BackupRedundancy: sampleBackup, QuotaNeeded: 4, Quota: regionalQuota(146),
		},
	}

	return sql.DBResult[sql.VCoreDatabase]{
		Location:    "uksouth",
		Summary:     sql.Summary{Considered: 170, Matched: 12, Restricted: 2, NoQuota: 3, Eligible: 7, Shown: len(dbs)},
		ServerQuota: &sql.QuotaHeadroom{Name: "ServerQuota", Limit: 250, Used: 1, Free: 249},
		Databases:   dbs,
		Warnings:    []sql.Warning{},
	}
}

func sampleDTUResult() sql.DBResult[sql.DTUDatabase] {
	dbs := []sql.DTUDatabase{
		{
			Name: "S3", Tier: sql.TierStandard, DTUs: 100, MaxStorageGiB: new(1024.0),
			BackupRedundancy: sampleBackup, QuotaNeeded: 1, Quota: regionalQuota(146),
			Pricing: sql.DTUPricing{Hourly: new(0.1895), Monthly: new(138.31)},
		},
		{
			Name: "P1", Tier: sql.TierPremium, DTUs: 125, MaxStorageGiB: new(1024.0), ZoneRedundant: true,
			BackupRedundancy: sampleBackup, QuotaNeeded: 2, Quota: regionalQuota(146),
			Pricing: sql.DTUPricing{Hourly: new(0.5874), Monthly: new(428.79)},
		},
		{
			Name: "S12", Tier: sql.TierStandard, DTUs: 3000, MaxStorageGiB: new(1024.0),
			BackupRedundancy: sampleBackup, QuotaNeeded: 30, Quota: regionalQuota(146),
		},
	}

	return sql.DBResult[sql.DTUDatabase]{
		Location:    "uksouth",
		Summary:     sql.Summary{Considered: 16, Matched: 5, Eligible: 5, Shown: len(dbs)},
		ServerQuota: &sql.QuotaHeadroom{Name: "ServerQuota", Limit: 250, Used: 1, Free: 249},
		Databases:   dbs,
		Warnings:    []sql.Warning{},
	}
}

func TestWriteSQLVCoreResult_golden(t *testing.T) {
	t.Parallel()

	tableOpts := sqlVCoreOptions{
		commonOptions: sqlCommon(output.FormatTable),
		Filter: sql.VCoreFilter{
			VCores: new(4), Tiers: []sql.Tier{sql.TierGeneralPurpose, sql.TierHyperscale}, ZoneRedundant: true,
			Licence: sql.LicenceAHB, Limit: 4,
		},
	}

	jsonOpts := tableOpts
	jsonOpts.Output = output.FormatJSON

	withWarning := sampleVCoreResult()
	withWarning.Warnings = []sql.Warning{{Code: sql.WarningQuotaMissing, Message: "no quota entry"}}

	tests := []struct {
		name string
		st   output.Styles
		opts sqlVCoreOptions
		res  sql.DBResult[sql.VCoreDatabase]
	}{
		{name: "sql_db_vcore_table", opts: tableOpts, res: sampleVCoreResult()},
		{name: "sql_db_vcore_table_colour", st: output.NewStyles(true), opts: tableOpts, res: sampleVCoreResult()},
		{name: "sql_db_vcore_json", opts: jsonOpts, res: withWarning},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			if err := writeSQLVCoreResult(&buf, tc.st, tc.opts, tc.res); err != nil {
				t.Fatalf("writeSQLVCoreResult() error = %v", err)
			}

			golden(t, tc.name, buf.Bytes())
		})
	}
}

func TestWriteSQLDTUResult_golden(t *testing.T) {
	t.Parallel()

	tableOpts := sqlDTUOptions{
		commonOptions: sqlCommon(output.FormatTable),
		Filter:        sql.DTUFilter{DTUs: new(100), Limit: 4},
	}

	jsonOpts := tableOpts
	jsonOpts.Output = output.FormatJSON

	tests := []struct {
		name string
		opts sqlDTUOptions
	}{
		{name: "sql_db_dtu_table", opts: tableOpts},
		{name: "sql_db_dtu_json", opts: jsonOpts},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			if err := writeSQLDTUResult(&buf, output.Styles{}, tc.opts, sampleDTUResult()); err != nil {
				t.Fatalf("writeSQLDTUResult() error = %v", err)
			}

			golden(t, tc.name, buf.Bytes())
		})
	}
}

func TestWriteSQLResult_empty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		summary sql.Summary
		want    string
	}{
		{
			name: "nothing offered", summary: sql.Summary{},
			want: "\nNo DTU-based SQL Database SKUs were returned for uksouth. Check the location name.\nFilters:\n - DTUs >= 100\n",
		},
		{
			name: "nothing matches", summary: sql.Summary{Considered: 16},
			want: "\nNo DTU-based SQL Database SKUs in uksouth meet your requirements (16 checked).\nFilters:\n - DTUs >= 100\n",
		},
		{
			name: "all restricted", summary: sql.Summary{Considered: 16, Matched: 9, Restricted: 9},
			want: "\nNone of the matching DTU-based SQL Database SKUs in uksouth can be deployed: 9 restricted for this subscription.\nFilters:\n - DTUs >= 100\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			res := sql.DBResult[sql.DTUDatabase]{Location: "uksouth", Summary: tc.summary, Databases: []sql.DTUDatabase{}}
			opts := sqlDTUOptions{commonOptions: sqlCommon(output.FormatTable), Filter: sql.DTUFilter{DTUs: new(100)}}

			var buf bytes.Buffer
			if err := writeSQLDTUResult(&buf, output.Styles{}, opts, res); err != nil {
				t.Fatalf("writeSQLDTUResult() error = %v", err)
			}

			if got := buf.String(); got != tc.want {
				t.Errorf("writeSQLDTUResult() wrote %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWriteSQLResult_writeError(t *testing.T) {
	t.Parallel()

	for _, format := range output.Formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()

			vcoreOpts := sqlVCoreOptions{commonOptions: sqlCommon(format)}
			if err := writeSQLVCoreResult(errWriter{}, output.Styles{}, vcoreOpts, sampleVCoreResult()); !errors.Is(err, errWrite) {
				t.Errorf("writeSQLVCoreResult() error = %v, want one wrapping %v", err, errWrite)
			}

			dtuOpts := sqlDTUOptions{commonOptions: sqlCommon(format)}
			if err := writeSQLDTUResult(errWriter{}, output.Styles{}, dtuOpts, sampleDTUResult()); !errors.Is(err, errWrite) {
				t.Errorf("writeSQLDTUResult() error = %v, want one wrapping %v", err, errWrite)
			}
		})
	}
}

func TestDescribeFilters(t *testing.T) {
	t.Parallel()

	gotVCore := describeVCoreFilter(sql.VCoreFilter{
		Tiers:            []sql.Tier{sql.TierGeneralPurpose, sql.TierHyperscale},
		Compute:          sql.ComputeServerless,
		VCores:           new(4),
		MemoryGiB:        new(0.0),
		StorageGiB:       new(512.0),
		ZoneRedundant:    true,
		BackupRedundancy: sql.BackupGeoZone,
	})

	wantVCore := []criterion{
		{Name: "tier", Value: "general-purpose or hyperscale"},
		{Name: "compute", Value: "serverless"},
		{Name: "vCores", Value: ">= 4"},
		{Name: "storage", Value: ">= 512 GiB"},
		{Name: "zone redundant", Value: "yes"},
		{Name: "backup redundancy", Value: "geo-zone"},
	}

	if diff := cmp.Diff(wantVCore, gotVCore); diff != "" {
		t.Errorf("describeVCoreFilter() mismatch (-want +got):\n%s", diff)
	}

	gotDTU := describeDTUFilter(sql.DTUFilter{DTUs: new(0), ExactMatch: true})
	if diff := cmp.Diff([]criterion{{Name: "DTUs", Value: "= 0"}}, gotDTU); diff != "" {
		t.Errorf("describeDTUFilter() mismatch (-want +got):\n%s", diff)
	}

	if got := describeDTUFilter(sql.DTUFilter{}); got == nil || len(got) != 0 {
		t.Errorf("describeDTUFilter(empty) = %#v, want an empty, non-nil list", got)
	}
}

func TestMemoryText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		db   sql.VCoreDatabase
		want string
	}{
		{db: sql.VCoreDatabase{Compute: sql.ComputeProvisioned, MemoryGiB: new(20.4)}, want: "20.4 GiB"},
		{db: sql.VCoreDatabase{Compute: sql.ComputeServerless}, want: "auto"},
		{db: sql.VCoreDatabase{Compute: sql.ComputeProvisioned}, want: "-"},
	}

	for _, tc := range tests {
		if got := memoryText(tc.db); got != tc.want {
			t.Errorf("memoryText(%+v) = %q, want %q", tc.db, got, tc.want)
		}
	}
}

func TestStorageText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   *float64
		want string
	}{
		{in: nil, want: "-"},
		{in: new(768.0), want: "768 GiB"},
		{in: new(1024.0), want: "1 TiB"},
		{in: new(1536.0), want: "1536 GiB"},
		{in: new(131072.0), want: "128 TiB"},
	}

	for _, tc := range tests {
		if got := storageText(tc.in); got != tc.want {
			t.Errorf("storageText(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

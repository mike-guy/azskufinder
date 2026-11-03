package cli

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/mike-guy/azskufinder/internal/sql"
)

const sortHint = "Values differ between commands, and AZSKUFINDER_SORT_BY or sort-by in the config file applies to every command."

func TestReadSQLVCoreOptions(t *testing.T) {
	t.Parallel()

	got, err := readSQLVCoreOptions(bindViperFlags(t, with(validCommon(), map[string]any{
		"tier":              "General-Purpose, hyperscale",
		"hardware":          "standard-series",
		"compute":           "provisioned",
		"vcores":            4,
		"memory":            16,
		"storage":           512,
		"exact-match":       true,
		"zone-redundant":    true,
		"backup-redundancy": "zone",
		"licence":           "ahb",
		"sort-by":           "vcores:desc",
		"limit":             5,
	}), "sql", "db", "vcore"))
	if err != nil {
		t.Fatalf("readSQLVCoreOptions() error = %v", err)
	}

	want := sql.VCoreFilter{
		Tiers:            []sql.Tier{sql.TierGeneralPurpose, sql.TierHyperscale},
		Compute:          sql.ComputeProvisioned,
		Hardware:         []sql.Hardware{sql.HardwareStandard},
		VCores:           new(4),
		MemoryGiB:        new(16.0),
		StorageGiB:       new(512.0),
		ExactMatch:       true,
		ZoneRedundant:    true,
		BackupRedundancy: sql.BackupZone,
		Licence:          sql.LicenceAHB,
		Sort:             sql.Sort{Key: sql.SortVCores, Desc: true},
		Limit:            5,
	}

	if diff := cmp.Diff(want, got.Filter); diff != "" {
		t.Errorf("readSQLVCoreOptions() filter mismatch (-want +got):\n%s", diff)
	}
}

func TestReadSQLVCoreOptions_defaults(t *testing.T) {
	t.Parallel()

	got, err := readSQLVCoreOptions(bindViperFlags(t, validCommon(), "sql", "db", "vcore"))
	if err != nil {
		t.Fatalf("readSQLVCoreOptions() error = %v", err)
	}

	want := sql.VCoreFilter{Licence: sql.LicenceIncluded, Sort: sql.Sort{Key: sql.SortMonthly}, Limit: defaultLimit}

	if diff := cmp.Diff(want, got.Filter); diff != "" {
		t.Errorf("readSQLVCoreOptions() filter mismatch (-want +got):\n%s", diff)
	}
}

func TestReadSQLVCoreOptions_errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		set      map[string]any
		wantMsg  string
		wantHint string
	}{
		{
			name:    "common options are checked",
			set:     map[string]any{"subscription-id": ""},
			wantMsg: "--subscription-id is required", wantHint: "Use --subscription-id or set AZSKUFINDER_SUBSCRIPTION_ID.",
		},
		{
			name:     "a DTU sort column",
			set:      map[string]any{"sort-by": "dtus"},
			wantMsg:  `--sort-by: unknown column "dtus", choose one of: name, tier, compute, hardware, vcores, memory, maxstorage, freequota, hourly, monthly`,
			wantHint: sortHint,
		},
		{
			name:     "a DTU tier",
			set:      map[string]any{"tier": "standard"},
			wantMsg:  `--tier: unknown tier "standard": use one of general-purpose, business-critical, hyperscale`,
			wantHint: "Values differ between commands, and AZSKUFINDER_TIER or tier in the config file applies to every command.",
		},
		{name: "compute", set: map[string]any{"compute": "spot"}, wantMsg: `--compute: unknown compute model "spot": use one of provisioned, serverless, any`},
		{name: "hardware", set: map[string]any{"hardware": "fsv2"}, wantMsg: `--hardware: unknown hardware "fsv2": use one of standard-series, premium-series, premium-series-memory-optimised, dc-series`},
		{name: "licence", set: map[string]any{"licence": "byol"}, wantMsg: `--licence: unknown licence "byol": use one of included, ahb`},
		{name: "licence any", set: map[string]any{"licence": "any"}, wantMsg: `--licence: unknown licence "any": use one of included, ahb`},
		{name: "backup redundancy", set: map[string]any{"backup-redundancy": "tape"}, wantMsg: `--backup-redundancy: unknown backup redundancy "tape": use one of local, zone, geo, geo-zone, any`},
		{name: "negative vcores", set: map[string]any{"vcores": -1}, wantMsg: "--vcores must not be negative, got -1"},
		{name: "storage not a number", set: map[string]any{"storage": "lots"}, wantMsg: `--storage must be a number of at least 0, got "lots"`},
		{
			name:    "memory with serverless",
			set:     map[string]any{"compute": "serverless", "memory": 8},
			wantMsg: "--memory cannot be used with --compute serverless, whose memory scales with the workload",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := readSQLVCoreOptions(bindViperFlags(t, with(validCommon(), tc.set), "sql", "db", "vcore"))
			checkUsageError(t, err, tc.wantMsg, tc.wantHint)
		})
	}
}

func TestReadSQLDTUOptions(t *testing.T) {
	t.Parallel()

	got, err := readSQLDTUOptions(bindViperFlags(t, with(validCommon(), map[string]any{
		"tier":           "standard,premium",
		"dtus":           100,
		"storage":        250,
		"zone-redundant": true,
		"sort-by":        "dtus:desc",
	}), "sql", "db", "dtu"))
	if err != nil {
		t.Fatalf("readSQLDTUOptions() error = %v", err)
	}

	want := sql.DTUFilter{
		Tiers:         []sql.Tier{sql.TierStandard, sql.TierPremium},
		DTUs:          new(100),
		StorageGiB:    new(250.0),
		ZoneRedundant: true,
		Sort:          sql.Sort{Key: sql.SortDTUs, Desc: true},
		Limit:         defaultLimit,
	}

	if diff := cmp.Diff(want, got.Filter); diff != "" {
		t.Errorf("readSQLDTUOptions() filter mismatch (-want +got):\n%s", diff)
	}
}

func TestReadSQLDTUOptions_errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		set      map[string]any
		wantMsg  string
		wantHint string
	}{
		{
			name:     "a vCore sort column",
			set:      map[string]any{"sort-by": "vcores"},
			wantMsg:  `--sort-by: unknown column "vcores", choose one of: name, tier, dtus, maxstorage, freequota, hourly, monthly`,
			wantHint: sortHint,
		},
		{
			name:     "a vCore tier",
			set:      map[string]any{"tier": "hyperscale"},
			wantMsg:  `--tier: unknown tier "hyperscale": use one of basic, standard, premium`,
			wantHint: "Values differ between commands, and AZSKUFINDER_TIER or tier in the config file applies to every command.",
		},
		{name: "dtus not a number", set: map[string]any{"dtus": "many"}, wantMsg: `--dtus must be a whole number, got "many"`},
		{name: "exact match", set: map[string]any{"exact-match": "yes"}, wantMsg: `--exact-match must be true or false, got "yes"`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := readSQLDTUOptions(bindViperFlags(t, with(validCommon(), tc.set), "sql", "db", "dtu"))
			checkUsageError(t, err, tc.wantMsg, tc.wantHint)
		})
	}
}

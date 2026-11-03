package sql

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/mike-guy/azskufinder/internal/retail"
)

// approx compares prices made by multiplying and adding floats.
var approx = cmpopts.EquateApprox(0, 1e-9)

func TestPriceFilter(t *testing.T) {
	t.Parallel()

	want := "serviceName eq 'SQL Database' and (armRegionName eq 'uksouth' or armRegionName eq 'Global') and priceType eq 'Consumption'"

	if got := priceFilter(dbPricingService, "uksouth"); got != want {
		t.Errorf("priceFilter() = %q, want %q", got, want)
	}
}

// pricing is the Pricing of compute, licence and zone surcharges, each per
// hour.
func pricing(compute float64, licence, zone *float64) Pricing {
	return total(compute, licence, zone)
}

// TestPriceIndex_vcore prices vCore databases from real uksouth rows, in USD.
func TestPriceIndex_vcore(t *testing.T) {
	t.Parallel()

	idx := indexPrices(loadPrices(t, "prices_sqldb.json"), "uksouth", discard)

	vcore := func(name string, tier Tier, hw Hardware, compute Compute, vcores int) VCoreDatabase {
		return VCoreDatabase{Name: name, Tier: tier, Compute: compute, Hardware: hw, VCores: vcores}
	}

	tests := []struct {
		name    string
		db      VCoreDatabase
		licence Licence
		zone    bool
		want    Pricing
	}{
		{
			name: "general purpose with licence", db: vcore("GP_Gen5_4", TierGeneralPurpose, HardwareStandard, ComputeProvisioned, 4),
			licence: LicenceIncluded, want: pricing(4*0.190271, new(4*0.099966), nil),
		},
		{
			name: "general purpose with Azure Hybrid Benefit", db: vcore("GP_Gen5_4", TierGeneralPurpose, HardwareStandard, ComputeProvisioned, 4),
			licence: LicenceAHB, want: pricing(4*0.190271, nil, nil),
		},
		{
			name: "general purpose zone redundant", db: vcore("GP_Gen5_4", TierGeneralPurpose, HardwareStandard, ComputeProvisioned, 4),
			licence: LicenceAHB, zone: true, want: pricing(4*0.190271, nil, new(4*0.114163)),
		},
		{
			name: "business critical zone redundancy is free", db: vcore("BC_Gen5_4", TierBusinessCritical, HardwareStandard, ComputeProvisioned, 4),
			licence: LicenceIncluded, zone: true, want: pricing(4*0.380544, new(4*0.375), nil),
		},
		{
			name: "hyperscale has no licence fee", db: vcore("HS_Gen5_4", TierHyperscale, HardwareStandard, ComputeProvisioned, 4),
			licence: LicenceIncluded, want: pricing(4*0.228325, nil, nil),
		},
		{
			name: "hyperscale premium-series memory optimised", db: vcore("HS_MOPRMS_2", TierHyperscale, HardwarePremiumMemoryOp, ComputeProvisioned, 2),
			licence: LicenceIncluded, want: pricing(2*0.319655, nil, nil),
		},
		{
			name: "dc-series", db: vcore("GP_DC_2", TierGeneralPurpose, HardwareDC, ComputeProvisioned, 2),
			licence: LicenceIncluded, want: pricing(2*0.381, new(2*0.099966), nil),
		},
		{
			name: "serverless includes the licence", db: vcore("GP_S_Gen5_2", TierGeneralPurpose, HardwareStandard, ComputeServerless, 2),
			licence: LicenceIncluded, want: pricing(2*0.652198, nil, nil),
		},
		{
			name: "serverless zone redundant takes the latest row", db: vcore("GP_S_Gen5_2", TierGeneralPurpose, HardwareStandard, ComputeServerless, 2),
			licence: LicenceAHB, zone: true, want: pricing(2*0.652198, nil, new(2*0.391319)),
		},
		{
			// 0.47 from 2023-11 replaces 0.731 from 2021, the price before
			// Hyperscale lost its licence fee.
			name: "hyperscale serverless takes the latest row", db: vcore("HS_S_Gen5_2", TierHyperscale, HardwareStandard, ComputeServerless, 2),
			licence: LicenceIncluded, want: pricing(2*0.47, nil, nil),
		},
		{
			name: "hardware with no price", db: vcore("GP_Fsv2_8", TierGeneralPurpose, hardwareOther, ComputeProvisioned, 8),
			licence: LicenceIncluded,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := idx.vcore(tc.db, tc.licence, tc.zone)

			if diff := cmp.Diff(tc.want, got, approx); diff != "" {
				t.Errorf("vcore(%s) mismatch (-want +got):\n%s", tc.db.Name, diff)
			}
		})
	}
}

// TestPriceIndex_dtu prices DTU databases from real uksouth rows, in USD.
func TestPriceIndex_dtu(t *testing.T) {
	t.Parallel()

	idx := indexPrices(loadPrices(t, "prices_sqldb.json"), "uksouth", discard)

	price := func(hourly float64) DTUPricing {
		return DTUPricing{Hourly: new(hourly), Monthly: new(hourly * HoursPerMonth)}
	}

	tests := []struct {
		name string
		db   DTUDatabase
		want DTUPricing
	}{
		// The Global S3 row, at 4.8387 a day, is not the uksouth price.
		{name: "standard is a daily price", db: DTUDatabase{Name: "S3", Tier: TierStandard}, want: price(6.048 / 24)},
		{name: "basic is priced as B", db: DTUDatabase{Name: "Basic", Tier: TierBasic}, want: price(0.201 / 24)},
		{name: "premium", db: DTUDatabase{Name: "P2", Tier: TierPremium}, want: price(37.5 / 24)},
		{name: "no price", db: DTUDatabase{Name: "S12", Tier: TierStandard}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tc.want, idx.dtu(tc.db), approx); diff != "" {
				t.Errorf("dtu(%s) mismatch (-want +got):\n%s", tc.db.Name, diff)
			}
		})
	}
}

// TestPriceIndex_missingLicence checks that a total is not made up when the
// licence price is missing: the compute price is kept, the total is unknown.
func TestPriceIndex_missingLicence(t *testing.T) {
	t.Parallel()

	idx := indexPrices([]retail.Price{
		priceRow("uksouth", "SQL Database Single/Elastic Pool General Purpose - Compute Gen5", "vCore", "vCore", "1 Hour", 0.2, "2025-01-01"),
	}, "uksouth", discard)

	got := idx.vcore(VCoreDatabase{Tier: TierGeneralPurpose, Compute: ComputeProvisioned, Hardware: HardwareStandard, VCores: 2},
		LicenceIncluded, false)

	want := Pricing{ComputeHourly: new(0.4)}

	if diff := cmp.Diff(want, got, approx); diff != "" {
		t.Errorf("database() mismatch (-want +got):\n%s", diff)
	}
}

func TestIndexPrices_skips(t *testing.T) {
	t.Parallel()

	gp := "SQL Database Single/Elastic Pool General Purpose - Compute Gen5"

	rows := []retail.Price{
		priceRow("uksouth", gp, "4 vCore", "vCore", "1 Hour", 0.8, "2025-01-01"),
		priceRow("ukwest", gp, "vCore", "vCore", "1 Hour", 0.3, "2025-01-01"),
		priceRow("Global", "SQL Database Single Standard", "S3", "S3 DTUs", "1/Day", 4.8, ""),
		priceRow("uksouth", "SQL Database Single Standard", "S3 Secondary", "S3 Secondary DTUs", "1/Day", 4.5, ""),
		priceRow("uksouth", "SQL Database General Purpose - Serverless - Compute Gen5", "1 vCore", "1 vCore - Free", "1 Hour", 0, ""),
		priceRow("uksouth", "SQL Database SingleDB Hyperscale - Serverless - Read Repl - Compute Gen5", "1 vCore", "vCore", "1 Hour", 0.4, ""),
		priceRow("uksouth", "SQL Database Single/Elastic Pool General Purpose - Storage", "General Purpose", "Data Stored", "1 GB/Month", 0.1, ""),
	}

	reserved := priceRow("uksouth", gp, "vCore", "vCore", "1 Hour", 0.1, "")
	reserved.Type = "Reservation"
	rows = append(rows, reserved)

	idx := indexPrices(rows, "uksouth", discard)

	if len(idx.compute)+len(idx.zoneRedundancy)+len(idx.licence)+len(idx.dtus) != 0 {
		t.Errorf("indexPrices() = %+v, want every row skipped", idx)
	}
}

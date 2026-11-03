package sql

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// mixedCaps offers both purchasing models, as Azure does.
func mixedCaps() dbCaps {
	return dbCaps{location: deployable, objectives: []serviceObjective{
		objective("GP_Gen5_2", 2),
		objective("GP_Gen5_4", 4),
		objective("GP_Gen5_8", 8),
		dtuObjective("S0", "Standard", 10),
		dtuObjective("S3", "Standard", 100),
		dtuObjective("P2", "Premium", 250),
	}}
}

func TestService_FindVCore(t *testing.T) {
	t.Parallel()

	arm := &fakeARM{
		db: mixedCaps(),
		quotas: map[string]quota{
			regionalVCores: {Used: 0, Limit: 150},
			serverQuota:    {Used: 250, Limit: 250},
		},
	}
	prices := &fakePrices{rows: loadPrices(t, "prices_sqldb.json")}

	got, err := NewService(arm, prices, nil).FindVCore(t.Context(), "uksouth",
		VCoreFilter{VCores: new(4), Licence: LicenceIncluded, Sort: Sort{Key: SortMonthly, Desc: true}, Limit: 1})
	if err != nil {
		t.Fatalf("FindVCore() error = %v", err)
	}

	if !strings.Contains(prices.filter, "'SQL Database'") {
		t.Errorf("FindVCore() price filter = %q, want the SQL Database service", prices.filter)
	}

	if len(got.Databases) != 1 || got.Databases[0].Name != "GP_Gen5_8" || got.Databases[0].Pricing.Hourly == nil {
		t.Errorf("FindVCore() = %+v, want the priced GP_Gen5_8 only", got.Databases)
	}

	// The DTU objectives are not considered.
	if diff := cmp.Diff(Summary{Considered: 3, Matched: 2, Eligible: 2, Shown: 1}, got.Summary); diff != "" {
		t.Errorf("FindVCore() summary mismatch (-want +got):\n%s", diff)
	}

	if diff := cmp.Diff(&QuotaHeadroom{Name: serverQuota, Limit: 250, Used: 250, Free: 0}, got.ServerQuota); diff != "" {
		t.Errorf("FindVCore() server quota mismatch (-want +got):\n%s", diff)
	}

	if codes := warningCodes(got.Warnings); !cmp.Equal(codes, []string{WarningServerQuotaExhausted}) {
		t.Errorf("FindVCore() warnings = %v, want only %s", got.Warnings, WarningServerQuotaExhausted)
	}
}

func TestService_FindDTU(t *testing.T) {
	t.Parallel()

	arm := &fakeARM{db: mixedCaps(), quotas: map[string]quota{regionalVCores: {Used: 148, Limit: 150}}}
	prices := &fakePrices{rows: loadPrices(t, "prices_sqldb.json")}

	got, err := NewService(arm, prices, nil).FindDTU(t.Context(), "uksouth", DTUFilter{DTUs: new(50)})
	if err != nil {
		t.Fatalf("FindDTU() error = %v", err)
	}

	// P2 needs 3 vCores of quota, and only 2 are left.
	if len(got.Databases) != 1 || got.Databases[0].Name != "S3" || got.Databases[0].Pricing.Hourly == nil {
		t.Errorf("FindDTU() = %+v, want the priced S3 only", got.Databases)
	}

	if diff := cmp.Diff(Summary{Considered: 3, Matched: 2, NoQuota: 1, Eligible: 1, Shown: 1}, got.Summary); diff != "" {
		t.Errorf("FindDTU() summary mismatch (-want +got):\n%s", diff)
	}

	if got.ServerQuota != nil || len(got.Warnings) != 0 {
		t.Errorf("FindDTU() server quota = %v, warnings = %v, want neither", got.ServerQuota, got.Warnings)
	}
}

func TestService_locationRestricted(t *testing.T) {
	t.Parallel()

	restricted := availability{reason: "location West Europe is Visible: Provisioning is restricted in this region."}

	arm := &fakeARM{
		db: dbCaps{location: restricted, objectives: []serviceObjective{
			dtuObjective("S3", "Standard", 100),
		}},
		quotas: map[string]quota{},
	}
	arm.db.objectives[0].availability = restricted

	got, err := NewService(arm, &fakePrices{err: errors.New("throttled")}, nil).FindDTU(t.Context(), "westeurope", DTUFilter{})
	if err != nil {
		t.Fatalf("FindDTU() error = %v", err)
	}

	if len(got.Databases) != 0 || got.Summary.Restricted != 1 {
		t.Errorf("FindDTU() = %+v, want none, one restricted", got)
	}

	want := []string{WarningPricingUnavailable, WarningLocationUnavailable, WarningQuotaMissing}
	if codes := warningCodes(got.Warnings); !cmp.Equal(codes, want) {
		t.Errorf("FindDTU() warning codes = %v, want %v", codes, want)
	}

	if !strings.Contains(got.Warnings[1].Message, "Provisioning is restricted") {
		t.Errorf("FindDTU() warning = %q, want Azure's reason", got.Warnings[1].Message)
	}
}

func TestService_errors(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()

	tests := []struct {
		name     string
		ctx      context.Context
		arm      *fakeARM
		location string
		want     string
	}{
		{name: "invalid location", ctx: t.Context(), arm: &fakeARM{}, location: "uk south'", want: "not a valid location name"},
		{name: "capabilities", ctx: t.Context(), arm: &fakeARM{capsErr: boom}, location: "uksouth", want: "boom"},
		{name: "quota", ctx: t.Context(), arm: &fakeARM{quotaErr: boom}, location: "uksouth", want: "boom"},
		{name: "cancelled", ctx: cancelled, arm: &fakeARM{}, location: "uksouth", want: "context canceled"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := NewService(tc.arm, &fakePrices{}, nil)

			if _, err := svc.FindVCore(tc.ctx, tc.location, VCoreFilter{}); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("FindVCore() error = %v, want %q", err, tc.want)
			}

			if _, err := svc.FindDTU(tc.ctx, tc.location, DTUFilter{}); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("FindDTU() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func warningCodes(warnings []Warning) []string {
	codes := []string{}
	for _, w := range warnings {
		codes = append(codes, w.Code)
	}

	return codes
}

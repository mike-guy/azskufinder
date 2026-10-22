package vm

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/mike-guy/azskufinder/internal/retail"
)

const (
	dsv5Family = "standardDSv5Family"
	esv5Family = "standardESv5Family"
)

// plentyOfQuota has room for anything the tests deploy.
func plentyOfQuota() map[string]quota {
	return map[string]quota{
		dsv5Family:    {Limit: 100},
		esv5Family:    {Limit: 100},
		regionalCores: {Limit: 100},
		spotCores:     {Limit: 50, Used: 10},
	}
}

func TestService_Find(t *testing.T) {
	t.Parallel()

	arm := &fakeARM{
		skus: []sku{
			skuOf("Standard_D4s_v5", dsv5Family, 4, 16),
			skuOf("Standard_D2s_v5", dsv5Family, 2, 8, withSpot(), withZones("2", "1")),
		},
		quotas: plentyOfQuota(),
		rates:  map[string]string{"standard_d2s_v5": "0-5"},
	}
	prices := &fakePrices{rows: []retail.Price{
		priceRow("Standard_D2s_v5", "D2s v5", "Virtual Machines Dsv5 Series", 0.1),
		priceRow("Standard_D2s_v5", "D2s v5 Spot", "Virtual Machines Dsv5 Series", 0.02),
		priceRow("Standard_D4s_v5", "D4s v5", "Virtual Machines Dsv5 Series", 0.2),
	}}

	got, err := NewService(arm, prices, nil).Find(t.Context(), "uksouth", Filter{IncludeSpot: true})
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}

	d2Hourly, d4Hourly := 0.1, 0.2
	headroom := []QuotaHeadroom{
		{Name: dsv5Family, Limit: 100, Free: 100},
		{Name: regionalCores, Limit: 100, Free: 100},
	}

	want := Result{
		Location:  "uksouth",
		Summary:   Summary{Considered: 2, Matched: 2, Eligible: 2, Shown: 2},
		SpotQuota: &QuotaHeadroom{Name: spotCores, Limit: 50, Used: 10, Free: 40},
		VMs: []VM{
			{
				Name: "Standard_D2s_v5", Family: dsv5Family, VCPUs: 2, VCPUsAvailable: 2, MemoryGiB: 8,
				Arch: ArchX64, Category: CategoryGeneral, Zones: []string{"1", "2"}, SpotCapable: true,
				Quota:      headroom,
				Pricing:    Pricing{OnDemandHourly: new(d2Hourly), OnDemandMonthly: new(d2Hourly * HoursPerMonth)},
				Spot:       &Spot{HourlyPrice: new(0.02), EvictionRate: new("0-5")},
				categories: []Category{CategoryGeneral},
			},
			{
				Name: "Standard_D4s_v5", Family: dsv5Family, VCPUs: 4, VCPUsAvailable: 4, MemoryGiB: 16,
				Arch: ArchX64, Category: CategoryGeneral, Zones: []string{},
				Quota:      headroom,
				Pricing:    Pricing{OnDemandHourly: new(d4Hourly), OnDemandMonthly: new(d4Hourly * HoursPerMonth)},
				categories: []Category{CategoryGeneral},
			},
		},
		Warnings: []Warning{},
	}

	if diff := cmp.Diff(want, got, cmp.AllowUnexported(VM{})); diff != "" {
		t.Errorf("Find() mismatch (-want +got):\n%s", diff)
	}

	if want := priceFilter("uksouth"); prices.filter != want {
		t.Errorf("Find() queried prices with %q, want %q", prices.filter, want)
	}
}

func TestService_Find_empty(t *testing.T) {
	t.Parallel()

	got, err := NewService(&fakeARM{}, &fakePrices{}, nil).Find(t.Context(), "uksouth", Filter{})
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}

	// Never nil, so that JSON shows [] rather than null.
	if got.VMs == nil || got.Warnings == nil {
		t.Errorf("Find() VMs = %#v, Warnings = %#v, want both non-nil", got.VMs, got.Warnings)
	}
}

func TestService_Find_spotQuota(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		includeSpot bool
		quotas      map[string]quota
		want        *QuotaHeadroom
	}{
		{name: "asked for", includeSpot: true, quotas: plentyOfQuota(), want: &QuotaHeadroom{Name: spotCores, Limit: 50, Used: 10, Free: 40}},
		{name: "not asked for", includeSpot: false, quotas: plentyOfQuota(), want: nil},
		{name: "not reported", includeSpot: true, quotas: map[string]quota{regionalCores: {Limit: 100}}, want: nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := NewService(&fakeARM{quotas: tc.quotas}, &fakePrices{}, nil).
				Find(t.Context(), "uksouth", Filter{IncludeSpot: tc.includeSpot})
			if err != nil {
				t.Fatalf("Find() error = %v", err)
			}

			if diff := cmp.Diff(tc.want, got.SpotQuota); diff != "" {
				t.Errorf("Find() SpotQuota mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestService_Find_invalidInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		location string
		filter   Filter
		wantErr  string
	}{
		{name: "location with a space", location: "uk south", wantErr: "not a valid location"},
		{name: "location with a quote", location: "x'; drop", wantErr: "not a valid location"},
		{name: "spot sort without spot", location: "uksouth", filter: Filter{Sort: Sort{Key: SortEviction}}, wantErr: "Spot must be asked for"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			arm, prices := &fakeARM{}, &fakePrices{}

			_, err := NewService(arm, prices, nil).Find(t.Context(), tc.location, tc.filter)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Find(%q) error = %v, want one containing %q", tc.location, err, tc.wantErr)
			}

			if arm.calls != 0 || prices.calls != 0 {
				t.Errorf("Find(%q) made %d ARM and %d price calls, want none", tc.location, arm.calls, prices.calls)
			}
		})
	}
}

func TestService_Find_requiredSourceErrors(t *testing.T) {
	t.Parallel()

	errBoom := errors.New("boom")

	tests := []struct {
		name string
		arm  *fakeARM
	}{
		{name: "SKUs", arm: &fakeARM{skusErr: errBoom, quotas: plentyOfQuota()}},
		{name: "quota", arm: &fakeARM{quotaErr: errBoom}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := NewService(tc.arm, &fakePrices{}, nil).Find(t.Context(), "uksouth", Filter{})
			if !errors.Is(err, errBoom) {
				t.Errorf("Find() error = %v, want %v", err, errBoom)
			}
		})
	}
}

func TestService_Find_optionalSourceErrors(t *testing.T) {
	t.Parallel()

	skus := []sku{skuOf("Standard_D2s_v5", dsv5Family, 2, 8, withSpot())}

	t.Run("prices", func(t *testing.T) {
		t.Parallel()

		arm := &fakeARM{skus: skus, quotas: plentyOfQuota()}

		got, err := NewService(arm, &fakePrices{err: errors.New("boom")}, nil).Find(t.Context(), "uksouth", Filter{})
		if err != nil {
			t.Fatalf("Find() error = %v", err)
		}

		want := []Warning{{Code: WarningPricingUnavailable, Message: "prices could not be fetched, so none are shown: boom"}}
		if diff := cmp.Diff(want, got.Warnings); diff != "" {
			t.Errorf("Find() warnings mismatch (-want +got):\n%s", diff)
		}

		if len(got.VMs) != 1 || got.VMs[0].Pricing != (Pricing{}) {
			t.Errorf("Find() VMs = %+v, want one without prices", got.VMs)
		}
	})

	t.Run("eviction rates", func(t *testing.T) {
		t.Parallel()

		arm := &fakeARM{skus: skus, quotas: plentyOfQuota(), ratesErr: errors.New("boom")}

		got, err := NewService(arm, &fakePrices{}, nil).Find(t.Context(), "uksouth", Filter{IncludeSpot: true})
		if err != nil {
			t.Fatalf("Find() error = %v", err)
		}

		want := []Warning{{Code: WarningSpotUnavailable, Message: "Spot eviction rates could not be fetched, so none are shown: boom"}}
		if diff := cmp.Diff(want, got.Warnings); diff != "" {
			t.Errorf("Find() warnings mismatch (-want +got):\n%s", diff)
		}

		if len(got.VMs) != 1 || got.VMs[0].Spot == nil || got.VMs[0].Spot.EvictionRate != nil {
			t.Errorf("Find() VMs = %+v, want one with Spot and no eviction rate", got.VMs)
		}
	})
}

func TestService_Find_evictionLookups(t *testing.T) {
	t.Parallel()

	skus := []sku{
		skuOf("Standard_D2s_v5", dsv5Family, 2, 8, withSpot()),
		skuOf("Standard_D4s_v5", dsv5Family, 4, 16, withSpot()),
		skuOf("Standard_E2s_v5", esv5Family, 2, 16),
	}
	rows := []retail.Price{
		priceRow("Standard_E2s_v5", "E2s v5", "Virtual Machines Esv5 Series", 0.05),
		priceRow("Standard_D2s_v5", "D2s v5", "Virtual Machines Dsv5 Series", 0.1),
		priceRow("Standard_D4s_v5", "D4s v5", "Virtual Machines Dsv5 Series", 0.2),
	}

	tests := []struct {
		name   string
		skus   []sku
		filter Filter
		want   [][]string
	}{
		{
			// Monthly order is E2, D2, D4: only D2 is shown and Spot capable.
			name:   "shortlist only",
			skus:   skus,
			filter: Filter{IncludeSpot: true, Limit: 2},
			want:   [][]string{{"Standard_D2s_v5"}},
		},
		{
			name:   "every eligible VM when sorting by Spot data",
			skus:   skus,
			filter: Filter{IncludeSpot: true, Sort: Sort{Key: SortEviction}, Limit: 1},
			want:   [][]string{{"Standard_D2s_v5", "Standard_D4s_v5"}},
		},
		{
			name:   "none Spot capable",
			skus:   skus[2:],
			filter: Filter{IncludeSpot: true},
			want:   nil,
		},
		{
			name:   "Spot not asked for",
			skus:   skus,
			filter: Filter{},
			want:   nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			arm := &fakeARM{skus: tc.skus, quotas: plentyOfQuota()}

			if _, err := NewService(arm, &fakePrices{rows: rows}, nil).Find(t.Context(), "uksouth", tc.filter); err != nil {
				t.Fatalf("Find() error = %v", err)
			}

			if diff := cmp.Diff(tc.want, arm.evictionCalls); diff != "" {
				t.Errorf("Find() eviction lookups mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestService_Find_cancelled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		arm    *fakeARM
		prices *fakePrices
	}{
		{name: "while reading SKUs", arm: &fakeARM{waitSKUs: true}, prices: &fakePrices{}},
		{name: "while reading quota", arm: &fakeARM{waitQuota: true}, prices: &fakePrices{}},
		// Prices only add a warning when they fail, but a timeout must still
		// fail the search.
		{name: "while reading prices", arm: &fakeARM{}, prices: &fakePrices{wait: true}},
		// Likewise eviction rates, which are read after the other three.
		{
			name:   "while reading eviction rates",
			arm:    &fakeARM{skus: []sku{skuOf("Standard_D2s_v5", dsv5Family, 2, 8, withSpot())}, quotas: plentyOfQuota(), waitRates: true},
			prices: &fakePrices{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
				t.Cleanup(cancel)

				_, err := NewService(tc.arm, tc.prices, nil).Find(ctx, "uksouth", Filter{IncludeSpot: true})
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Errorf("Find() error = %v, want %v", err, context.DeadlineExceeded)
				}
			})
		})
	}
}

func TestService_Find_requiredErrorCancelsOthers(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		errBoom := errors.New("boom")

		// The SKU call only returns once the quota error cancels it.
		arm := &fakeARM{waitSKUs: true, quotaErr: errBoom}

		_, err := NewService(arm, &fakePrices{wait: true}, nil).Find(t.Context(), "uksouth", Filter{})
		if !errors.Is(err, errBoom) {
			t.Errorf("Find() error = %v, want %v", err, errBoom)
		}
	})
}

func TestService_Find_logging(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	if _, err := NewService(&fakeARM{}, &fakePrices{}, log).Find(t.Context(), "uksouth", Filter{}); err != nil {
		t.Fatalf("Find() error = %v", err)
	}

	for _, want := range []string{`msg="search complete"`, "component=vm"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("logged %q, want it to contain %q", buf.String(), want)
		}
	}
}

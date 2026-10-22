package vm

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/mike-guy/azskufinder/internal/retail"
)

func TestPriceFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		location string
		want     string
	}{
		{
			location: "uksouth",
			want:     "serviceName eq 'Virtual Machines' and armRegionName eq 'uksouth' and priceType eq 'Consumption'",
		},
		{
			location: "o'brien",
			want:     "serviceName eq 'Virtual Machines' and armRegionName eq 'o''brien' and priceType eq 'Consumption'",
		},
	}

	for _, tc := range tests {
		t.Run(tc.location, func(t *testing.T) {
			t.Parallel()

			if got := priceFilter(tc.location); got != tc.want {
				t.Errorf("priceFilter(%q) = %q, want %q", tc.location, got, tc.want)
			}
		})
	}
}

func TestIndexPrices(t *testing.T) {
	t.Parallel()

	const (
		d2      = "Standard_D2s_v5"
		linux   = "Virtual Machines Dsv5 Series"
		windows = "Virtual Machines Dsv5 Series Windows"
	)

	tests := []struct {
		name string
		rows []retail.Price
		os   OS
		want map[string]priceEntry
	}{
		{
			name: "linux on demand",
			rows: []retail.Price{priceRow(d2, "D2s v5", linux, 0.1)},
			os:   OSLinux,
			want: map[string]priceEntry{d2: {onDemand: new(0.1)}},
		},
		{
			name: "windows row ignored for linux",
			rows: []retail.Price{priceRow(d2, "D2s v5", linux, 0.1), priceRow(d2, "D2s v5", windows, 0.2)},
			os:   OSLinux,
			want: map[string]priceEntry{d2: {onDemand: new(0.1)}},
		},
		{
			name: "windows row used for windows",
			rows: []retail.Price{priceRow(d2, "D2s v5", linux, 0.1), priceRow(d2, "D2s v5", windows, 0.2)},
			os:   OSWindows,
			want: map[string]priceEntry{d2: {onDemand: new(0.2)}},
		},
		{
			name: "spot and low priority",
			rows: []retail.Price{
				priceRow(d2, "D2s v5", linux, 0.1),
				priceRow(d2, "D2s v5 Spot", linux, 0.02),
				priceRow(d2, "D2s v5 Low Priority", linux, 0.01),
			},
			os:   OSLinux,
			want: map[string]priceEntry{d2: {onDemand: new(0.1), spot: new(0.02)}},
		},
		{
			name: "only low priority leaves no entry",
			rows: []retail.Price{priceRow(d2, "D2s v5 Low Priority", linux, 0.01)},
			os:   OSLinux,
			want: map[string]priceEntry{},
		},
		{
			name: "not virtual machine products",
			rows: []retail.Price{
				priceRow(d2, "D2s v5", "Cloud Services Dsv5 Series", 0.01),
				priceRow(d2, "D2s v5", "CloudServices Dsv5 Series", 0.01),
				priceRow(d2, "D2s v5", "Dedicated Host Dsv5", 0.01),
				priceRow(d2, "D2s v5", "dedicatedhost Dsv5", 0.01),
				priceRow(d2, "D2s v5", linux, 0.1),
			},
			os:   OSLinux,
			want: map[string]priceEntry{d2: {onDemand: new(0.1)}},
		},
		{
			name: "not hourly consumption",
			rows: []retail.Price{
				func() retail.Price { r := priceRow(d2, "D2s v5", linux, 0.01); r.UnitOfMeasure = "1 Month"; return r }(),
				func() retail.Price { r := priceRow(d2, "D2s v5", linux, 0.01); r.Type = "Reservation"; return r }(),
				priceRow("", "D2s v5", linux, 0.01),
			},
			os:   OSLinux,
			want: map[string]priceEntry{},
		},
		{
			name: "cheapest row kept",
			rows: []retail.Price{
				priceRow(d2, "D2s v5", linux, 0.3),
				priceRow(d2, "D2s v5", linux, 0.1),
				priceRow(d2, "D2s v5", linux, 0.2),
			},
			os:   OSLinux,
			want: map[string]priceEntry{d2: {onDemand: new(0.1)}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := indexPrices(tc.rows, tc.os, discard)
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(priceEntry{})); diff != "" {
				t.Errorf("indexPrices() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCheaper(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		current *float64
		next    float64
		want    float64
	}{
		{name: "no current", current: nil, next: 2, want: 2},
		{name: "next lower", current: new(3.0), next: 2, want: 2},
		{name: "next higher", current: new(1.0), next: 2, want: 1},
		{name: "equal", current: new(2.0), next: 2, want: 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := cheaper(tc.current, tc.next); got == nil || *got != tc.want {
				t.Errorf("cheaper(%v, %g) = %v, want %g", tc.current, tc.next, got, tc.want)
			}
		})
	}
}

func TestApplyPrices(t *testing.T) {
	t.Parallel()

	index := map[string]priceEntry{
		"Standard_D2s_v5": {onDemand: new(0.1), spot: new(0.02)},
		"Standard_D4s_v5": {onDemand: new(0.2)},
	}

	tests := []struct {
		name        string
		vm          VM
		includeSpot bool
		want        VM
	}{
		{
			name: "on demand",
			vm:   VM{Name: "Standard_D2s_v5"},
			want: VM{Name: "Standard_D2s_v5", Pricing: Pricing{OnDemandHourly: new(0.1), OnDemandMonthly: new(0.1 * HoursPerMonth)}},
		},
		{
			name: "no price row stays nil",
			vm:   VM{Name: "Standard_E2s_v5"},
			want: VM{Name: "Standard_E2s_v5"},
		},
		{
			name:        "spot capable with spot",
			vm:          VM{Name: "Standard_D2s_v5", SpotCapable: true},
			includeSpot: true,
			want: VM{
				Name: "Standard_D2s_v5", SpotCapable: true,
				Pricing: Pricing{OnDemandHourly: new(0.1), OnDemandMonthly: new(0.1 * HoursPerMonth)},
				Spot:    &Spot{HourlyPrice: new(0.02)},
			},
		},
		{
			name:        "spot capable without a spot price",
			vm:          VM{Name: "Standard_D4s_v5", SpotCapable: true},
			includeSpot: true,
			want: VM{
				Name: "Standard_D4s_v5", SpotCapable: true,
				Pricing: Pricing{OnDemandHourly: new(0.2), OnDemandMonthly: new(0.2 * HoursPerMonth)},
				Spot:    &Spot{},
			},
		},
		{
			name:        "not spot capable",
			vm:          VM{Name: "Standard_D2s_v5"},
			includeSpot: true,
			want:        VM{Name: "Standard_D2s_v5", Pricing: Pricing{OnDemandHourly: new(0.1), OnDemandMonthly: new(0.1 * HoursPerMonth)}},
		},
		{
			name: "spot not asked for",
			vm:   VM{Name: "Standard_D2s_v5", SpotCapable: true},
			want: VM{Name: "Standard_D2s_v5", SpotCapable: true, Pricing: Pricing{OnDemandHourly: new(0.1), OnDemandMonthly: new(0.1 * HoursPerMonth)}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			vms := []VM{tc.vm}
			applyPrices(vms, index, tc.includeSpot, discard)

			if diff := cmp.Diff(tc.want, vms[0], cmp.AllowUnexported(VM{})); diff != "" {
				t.Errorf("applyPrices() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

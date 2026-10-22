package vm

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseArch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want Arch
	}{
		{in: "", want: ""},
		{in: "any", want: ""},
		{in: "ANY", want: ""},
		{in: " x64 ", want: ArchX64},
		{in: "Arm64", want: ArchARM64},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()

			got, err := ParseArch(tc.in)
			if err != nil || got != tc.want {
				t.Errorf("ParseArch(%q) = %q, %v, want %q, nil", tc.in, got, err, tc.want)
			}
		})
	}

	t.Run("unknown", func(t *testing.T) {
		t.Parallel()

		_, err := ParseArch("x86")
		if err == nil || !strings.Contains(err.Error(), "any, x64, arm64") {
			t.Errorf("ParseArch(%q) error = %v, want one listing the architectures", "x86", err)
		}
	})
}

func TestParseCategories(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want []Category
	}{
		{name: "empty", in: "", want: nil},
		{name: "two", in: "memory,gpu", want: []Category{CategoryMemory, CategoryGPU}},
		{name: "spaces and case", in: " Memory , GPU ", want: []Category{CategoryMemory, CategoryGPU}},
		{name: "duplicates removed in order", in: "gpu,memory,gpu", want: []Category{CategoryGPU, CategoryMemory}},
		{name: "empty parts", in: "memory,,gpu,", want: []Category{CategoryMemory, CategoryGPU}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseCategories(tc.in)
			if err != nil {
				t.Fatalf("ParseCategories(%q) error = %v", tc.in, err)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("ParseCategories(%q) mismatch (-want +got):\n%s", tc.in, diff)
			}
		})
	}

	t.Run("unknown", func(t *testing.T) {
		t.Parallel()

		_, err := ParseCategories("memory,Fast")
		if err == nil || !strings.Contains(err.Error(), `"fast"`) {
			t.Errorf("ParseCategories(%q) error = %v, want one naming %q", "memory,Fast", err, "fast")
		}
	})
}

func TestParseOS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want OS
	}{
		{in: "", want: OSLinux},
		{in: "linux", want: OSLinux},
		{in: " Windows ", want: OSWindows},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()

			got, err := ParseOS(tc.in)
			if err != nil || got != tc.want {
				t.Errorf("ParseOS(%q) = %q, %v, want %q, nil", tc.in, got, err, tc.want)
			}
		})
	}

	t.Run("unknown", func(t *testing.T) {
		t.Parallel()

		if _, err := ParseOS("mac"); err == nil {
			t.Errorf("ParseOS(%q) error = nil, want an error", "mac")
		}
	})
}

func TestFilter_LogValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		filter Filter
		want   []string
	}{
		{
			name:   "zero",
			filter: Filter{},
			want:   []string{"f.cpus=any", "f.memory_gib=any", "f.gpus=any", "f.arch=any", "f.categories=any", "f.limit=0"},
		},
		{
			name: "full",
			filter: Filter{
				CPUs: new(4), MemoryGiB: new(3.5), GPUs: new(1), ExactMatch: true,
				Arch: ArchARM64, Categories: []Category{CategoryMemory, CategoryGPU}, OS: OSWindows,
				IncludeSpot: true, Sort: Sort{Key: SortMemory, Desc: true}, Limit: 5,
			},
			want: []string{
				"f.cpus=4", "f.memory_gib=3.5", "f.gpus=1", "f.exact_match=true", "f.arch=arm64",
				"f.categories=memory,gpu", "f.os=windows", "f.include_spot=true", "f.sort=memory:desc", "f.limit=5",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			slog.New(slog.NewTextHandler(&buf, nil)).Info("search", "f", tc.filter)

			for _, w := range tc.want {
				if !strings.Contains(buf.String(), w) {
					t.Errorf("logged %q, want it to contain %q", buf.String(), w)
				}
			}
		})
	}
}

func TestParseVM(t *testing.T) {
	t.Parallel()

	// base is what a plain Standard_D4s_v5 parses to; cases change what differs.
	base := func(change func(*VM)) VM {
		v := VM{
			Name: "Standard_D4s_v5", Family: "standardDSv5Family",
			VCPUs: 4, VCPUsAvailable: 4, MemoryGiB: 16, Arch: ArchX64,
			Category: CategoryGeneral, Zones: []string{}, Quota: []QuotaHeadroom{},
			categories: []Category{CategoryGeneral},
		}
		if change != nil {
			change(&v)
		}

		return v
	}

	d4 := func(opts ...func(*sku)) sku { return skuOf("Standard_D4s_v5", "standardDSv5Family", 4, 16, opts...) }

	tests := []struct {
		name string
		sku  sku
		want VM
	}{
		{name: "plain", sku: d4(), want: base(nil)},
		{
			name: "constrained core",
			sku:  skuOf("Standard_E8-4s_v5", "standardESv5Family", 8, 64, withAvailable(4)),
			want: VM{
				Name: "Standard_E8-4s_v5", Family: "standardESv5Family",
				VCPUs: 8, VCPUsAvailable: 4, MemoryGiB: 64, Arch: ArchX64,
				Category: CategoryMemory, Zones: []string{}, Quota: []QuotaHeadroom{},
				categories: []Category{CategoryMemory},
			},
		},
		{name: "zero GPUs allowed", sku: d4(withGPUs(0)), want: base(nil)},
		{name: "arm64", sku: d4(withCap("CpuArchitectureType", "Arm64")), want: base(func(v *VM) { v.Arch = ArchARM64 })},
		{name: "arch missing", sku: d4(withCap("CpuArchitectureType", "")), want: base(func(v *VM) { v.Arch = archUnknown })},
		{name: "arch not known", sku: d4(withCap("CpuArchitectureType", "Power")), want: base(func(v *VM) { v.Arch = archUnknown })},
		{name: "spot capable", sku: d4(withCap("LowPriorityCapable", "True")), want: base(func(v *VM) { v.SpotCapable = true })},
		{name: "not spot capable", sku: d4(withCap("LowPriorityCapable", "false")), want: base(nil)},
		{
			name: "burstable",
			sku:  skuOf("Standard_B2s", "standardBSFamily", 2, 4),
			want: VM{
				Name: "Standard_B2s", Family: "standardBSFamily", VCPUs: 2, VCPUsAvailable: 2, MemoryGiB: 4,
				Arch: ArchX64, Category: CategoryBurst, Zones: []string{}, Quota: []QuotaHeadroom{},
				categories: []Category{CategoryGeneral, CategoryBurst},
			},
		},
		{
			name: "GPU series",
			sku:  skuOf("Standard_NC6s_v3", "standardNCSv3Family", 6, 112, withGPUs(1)),
			want: VM{
				Name: "Standard_NC6s_v3", Family: "standardNCSv3Family", VCPUs: 6, VCPUsAvailable: 6, MemoryGiB: 112,
				GPUs: 1, Arch: ArchX64, Category: CategoryGPU, Zones: []string{}, Quota: []QuotaHeadroom{},
				categories: []Category{CategoryGPU},
			},
		},
		{
			name: "GPUs in another series",
			sku:  d4(withGPUs(2)),
			want: base(func(v *VM) {
				v.GPUs = 2
				v.Category = CategoryGPU
				v.categories = []Category{CategoryGeneral, CategoryGPU}
			}),
		},
		{
			name: "series not known",
			sku:  skuOf("Standard_PB6s", "standardPBSFamily", 6, 112),
			want: VM{
				Name: "Standard_PB6s", Family: "standardPBSFamily", VCPUs: 6, VCPUsAvailable: 6, MemoryGiB: 112,
				Arch: ArchX64, Category: categoryOther, Zones: []string{}, Quota: []QuotaHeadroom{},
			},
		},
		{
			// The series pattern needs the Standard_ prefix as Azure writes it.
			name: "lower case prefix",
			sku:  skuOf("standard_d2", "", 2, 8),
			want: VM{
				Name: "standard_d2", VCPUs: 2, VCPUsAvailable: 2, MemoryGiB: 8,
				Arch: ArchX64, Category: categoryOther, Zones: []string{}, Quota: []QuotaHeadroom{},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := parseVM(tc.sku)
			if !ok {
				t.Fatalf("parseVM(%q) = false, want true", tc.sku.Name)
			}

			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(VM{})); diff != "" {
				t.Errorf("parseVM(%q) mismatch (-want +got):\n%s", tc.sku.Name, diff)
			}
		})
	}
}

func TestParseVM_unreadable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opt  func(*sku)
	}{
		{name: "vCPUs missing", opt: func(s *sku) { delete(s.Capabilities, "vCPUs") }},
		{name: "vCPUs zero", opt: withCap("vCPUs", "0")},
		{name: "vCPUs not a number", opt: withCap("vCPUs", "abc")},
		{name: "vCPUsAvailable zero", opt: withCap("vCPUsAvailable", "0")},
		{name: "memory missing", opt: func(s *sku) { delete(s.Capabilities, "MemoryGB") }},
		{name: "memory negative", opt: withCap("MemoryGB", "-1")},
		{name: "memory NaN", opt: withCap("MemoryGB", "NaN")},
		{name: "memory Inf", opt: withCap("MemoryGB", "Inf")},
		{name: "GPUs negative", opt: withCap("GPUs", "-1")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := skuOf("Standard_D4s_v5", "standardDSv5Family", 4, 16, tc.opt)
			if _, ok := parseVM(s); ok {
				t.Errorf("parseVM(%v) = true, want false", s.Capabilities)
			}
		})
	}
}

func TestMatches(t *testing.T) {
	t.Parallel()

	d4 := vmOf(t, skuOf("Standard_D4s_v5", "standardDSv5Family", 4, 16))
	e84 := vmOf(t, skuOf("Standard_E8-4s_v5", "standardESv5Family", 8, 64, withAvailable(4)))
	b2 := vmOf(t, skuOf("Standard_B2s", "standardBSFamily", 2, 3.5))
	nc := vmOf(t, skuOf("Standard_NC6s_v3", "standardNCSv3Family", 6, 112, withGPUs(1)))
	arm := vmOf(t, skuOf("Standard_D4ps_v5", "standardDPSv5Family", 4, 16, withCap("CpuArchitectureType", "Arm64")))
	unknown := vmOf(t, skuOf("Standard_D4x", "", 4, 16, withCap("CpuArchitectureType", "")))

	tests := []struct {
		name   string
		vm     VM
		filter Filter
		want   bool
	}{
		{name: "no requirements", vm: d4, filter: Filter{}, want: true},
		{name: "arch matches", vm: arm, filter: Filter{Arch: ArchARM64}, want: true},
		{name: "arch differs", vm: d4, filter: Filter{Arch: ArchARM64}, want: false},
		{name: "unknown arch never matches", vm: unknown, filter: Filter{Arch: ArchX64}, want: false},
		{name: "category matches", vm: e84, filter: Filter{Categories: []Category{CategoryMemory}}, want: true},
		{name: "any listed category", vm: e84, filter: Filter{Categories: []Category{CategoryGPU, CategoryMemory}}, want: true},
		{name: "category differs", vm: d4, filter: Filter{Categories: []Category{CategoryMemory}}, want: false},
		{name: "burstable matches B", vm: b2, filter: Filter{Categories: []Category{CategoryBurst}}, want: true},
		{name: "burstable excludes D", vm: d4, filter: Filter{Categories: []Category{CategoryBurst}}, want: false},
		{name: "general includes B", vm: b2, filter: Filter{Categories: []Category{CategoryGeneral}}, want: true},
		{name: "minimum CPUs met", vm: d4, filter: Filter{CPUs: new(2)}, want: true},
		{name: "minimum CPUs not met", vm: d4, filter: Filter{CPUs: new(8)}, want: false},
		{name: "CPUs are those available", vm: e84, filter: Filter{CPUs: new(8)}, want: false},
		{name: "exact CPUs", vm: e84, filter: Filter{CPUs: new(4), ExactMatch: true}, want: true},
		{name: "exact CPUs differ", vm: d4, filter: Filter{CPUs: new(2), ExactMatch: true}, want: false},
		{name: "minimum GPUs met", vm: nc, filter: Filter{GPUs: new(1)}, want: true},
		{name: "minimum GPUs not met", vm: d4, filter: Filter{GPUs: new(1)}, want: false},
		{name: "exact zero GPUs", vm: d4, filter: Filter{GPUs: new(0), ExactMatch: true}, want: true},
		{name: "exact GPUs differ", vm: nc, filter: Filter{GPUs: new(0), ExactMatch: true}, want: false},
		{name: "minimum memory met", vm: d4, filter: Filter{MemoryGiB: new(16.0)}, want: true},
		{name: "minimum memory not met", vm: d4, filter: Filter{MemoryGiB: new(16.5)}, want: false},
		{name: "exact memory within tolerance", vm: b2, filter: Filter{MemoryGiB: new(3.505), ExactMatch: true}, want: true},
		{name: "exact memory outside tolerance", vm: b2, filter: Filter{MemoryGiB: new(3.52), ExactMatch: true}, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := matches(tc.vm, tc.filter); got != tc.want {
				t.Errorf("matches(%s, %v) = %t, want %t", tc.vm.Name, tc.filter.LogValue(), got, tc.want)
			}
		})
	}
}

func TestUsableZones(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sku  sku
		want []string
	}{
		{name: "no zones", sku: sku{}, want: []string{}},
		{name: "sorted", sku: sku{Zones: []string{"3", "1", "2"}}, want: []string{"1", "2", "3"}},
		{name: "restricted removed", sku: sku{Zones: []string{"3", "1", "2"}, RestrictedZones: []string{"2"}}, want: []string{"1", "3"}},
		{name: "all restricted", sku: sku{Zones: []string{"1"}, RestrictedZones: []string{"1"}}, want: []string{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// cmp tells nil from empty, which matters: JSON shows [] not null.
			if diff := cmp.Diff(tc.want, usableZones(tc.sku)); diff != "" {
				t.Errorf("usableZones() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCandidates(t *testing.T) {
	t.Parallel()

	skus := []sku{
		skuOf("Standard_Bad", "", 4, 16, withCap("vCPUs", "x")),
		skuOf("Standard_D2s_v5", "standardDSv5Family", 2, 8),
		skuOf("Standard_D2as_v5", "standardDASv5Family", 2, 8, locationRestricted()),
		skuOf("Standard_D4s_v5", "standardDSv5Family", 4, 16, locationRestricted()),
		skuOf("Standard_D4as_v5", "standardDASv5Family", 4, 16, withZones("3", "1", "2"), restrictedZones("2")),
	}

	var summary Summary

	got := candidates(skus, Filter{CPUs: new(4)}, &summary, discard)

	if diff := cmp.Diff([]string{"Standard_D4as_v5"}, names(got)); diff != "" {
		t.Errorf("candidates() names mismatch (-want +got):\n%s", diff)
	}

	if len(got) == 1 {
		if diff := cmp.Diff([]string{"1", "3"}, got[0].Zones); diff != "" {
			t.Errorf("candidates() zones mismatch (-want +got):\n%s", diff)
		}
	}

	// Considered counts the unreadable SKU, and Restricted only the one that
	// matched.
	want := Summary{Considered: 5, Matched: 2, Restricted: 1}
	if diff := cmp.Diff(want, summary); diff != "" {
		t.Errorf("candidates() summary mismatch (-want +got):\n%s", diff)
	}
}

func TestCandidates_empty(t *testing.T) {
	t.Parallel()

	var summary Summary

	// Never nil, so that an empty result is [] in JSON.
	if got := candidates(nil, Filter{}, &summary, discard); got == nil || len(got) != 0 {
		t.Errorf("candidates(nil) = %#v, want an empty slice", got)
	}
}

func TestApplyQuota(t *testing.T) {
	t.Parallel()

	const (
		dsv5 = "standardDSv5Family"
		esv5 = "standardESv5Family"
	)

	d4 := vmOf(t, skuOf("Standard_D4s_v5", dsv5, 4, 16, withSpot()))
	d8 := vmOf(t, skuOf("Standard_D8s_v5", dsv5, 8, 32, withSpot()))
	e84 := vmOf(t, skuOf("Standard_E8-4s_v5", esv5, 8, 64, withAvailable(4)))
	noFamily := vmOf(t, skuOf("Standard_D2", "", 2, 8))

	plenty := quota{Limit: 100}

	missing := func(key string) Warning {
		return Warning{
			Code:    WarningQuotaMissing,
			Message: `Azure reported no quota entry "` + key + `", so that limit could not be checked`,
		}
	}

	tests := []struct {
		name         string
		vms          []VM
		quotas       map[string]quota
		includeSpot  bool
		wantKept     []string
		wantSpotOnly []string
		wantSummary  Summary
		wantWarnings []Warning
	}{
		{
			name:     "fits family and regional",
			vms:      []VM{d4},
			quotas:   map[string]quota{dsv5: plenty, regionalCores: plenty},
			wantKept: []string{"Standard_D4s_v5"},
		},
		{
			name:        "family short",
			vms:         []VM{d4},
			quotas:      map[string]quota{dsv5: {Limit: 10, Used: 8}, regionalCores: plenty},
			wantKept:    []string{},
			wantSummary: Summary{NoQuota: 1},
		},
		{
			name:        "regional short",
			vms:         []VM{d4},
			quotas:      map[string]quota{dsv5: plenty, regionalCores: {Limit: 10, Used: 7}},
			wantKept:    []string{},
			wantSummary: Summary{NoQuota: 1},
		},
		{
			name:     "exactly enough",
			vms:      []VM{d4},
			quotas:   map[string]quota{dsv5: {Limit: 10, Used: 6}, regionalCores: {Limit: 4}},
			wantKept: []string{"Standard_D4s_v5"},
		},
		{
			name:     "no family checks regional only",
			vms:      []VM{noFamily},
			quotas:   map[string]quota{regionalCores: plenty},
			wantKept: []string{"Standard_D2"},
		},
		{
			name:        "billed vCPUs count, not available",
			vms:         []VM{e84},
			quotas:      map[string]quota{esv5: {Limit: 4}, regionalCores: plenty},
			wantKept:    []string{},
			wantSummary: Summary{NoQuota: 1},
		},
		{
			name:         "family entry missing is kept with a warning",
			vms:          []VM{d4, d8},
			quotas:       map[string]quota{regionalCores: plenty},
			wantKept:     []string{"Standard_D4s_v5", "Standard_D8s_v5"},
			wantWarnings: []Warning{missing(dsv5)},
		},
		{
			name:         "warnings sorted by key",
			vms:          []VM{e84, d4},
			quotas:       map[string]quota{},
			wantKept:     []string{"Standard_E8-4s_v5", "Standard_D4s_v5"},
			wantWarnings: []Warning{missing(regionalCores), missing(dsv5), missing(esv5)},
		},
		{
			name:         "spot only",
			vms:          []VM{d4},
			quotas:       map[string]quota{dsv5: {Limit: 2}, regionalCores: plenty, spotCores: {Limit: 10, Used: 6}},
			includeSpot:  true,
			wantKept:     []string{"Standard_D4s_v5"},
			wantSpotOnly: []string{"Standard_D4s_v5"},
			wantSummary:  Summary{SpotOnly: 1},
		},
		{
			name:        "fits with spot asked for is not spot only",
			vms:         []VM{d4},
			quotas:      map[string]quota{dsv5: plenty, regionalCores: plenty, spotCores: plenty},
			includeSpot: true,
			wantKept:    []string{"Standard_D4s_v5"},
		},
		{
			name:        "short and not spot capable",
			vms:         []VM{e84},
			quotas:      map[string]quota{esv5: {Limit: 2}, regionalCores: plenty, spotCores: plenty},
			includeSpot: true,
			wantKept:    []string{},
			wantSummary: Summary{NoQuota: 1},
		},
		{
			name:         "short and spot entry missing",
			vms:          []VM{d4},
			quotas:       map[string]quota{dsv5: {Limit: 2}, regionalCores: plenty},
			includeSpot:  true,
			wantKept:     []string{},
			wantSummary:  Summary{NoQuota: 1},
			wantWarnings: []Warning{missing(spotCores)},
		},
		{
			name:        "short and spot full",
			vms:         []VM{d4},
			quotas:      map[string]quota{dsv5: {Limit: 2}, regionalCores: plenty, spotCores: {Limit: 10, Used: 8}},
			includeSpot: true,
			wantKept:    []string{},
			wantSummary: Summary{NoQuota: 1},
		},
		{
			name:        "short without spot asked for",
			vms:         []VM{d4},
			quotas:      map[string]quota{dsv5: {Limit: 2}, regionalCores: plenty},
			wantKept:    []string{},
			wantSummary: Summary{NoQuota: 1},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var summary Summary

			kept, warnings := applyQuota(tc.vms, tc.quotas, tc.includeSpot, &summary, discard)

			if diff := cmp.Diff(tc.wantKept, names(kept)); diff != "" {
				t.Errorf("applyQuota() kept mismatch (-want +got):\n%s", diff)
			}

			var spotOnly []string

			for _, v := range kept {
				if v.SpotOnly {
					spotOnly = append(spotOnly, v.Name)
				}
			}

			if diff := cmp.Diff(tc.wantSpotOnly, spotOnly); diff != "" {
				t.Errorf("applyQuota() spot only mismatch (-want +got):\n%s", diff)
			}

			if diff := cmp.Diff(tc.wantSummary, summary); diff != "" {
				t.Errorf("applyQuota() summary mismatch (-want +got):\n%s", diff)
			}

			if diff := cmp.Diff(tc.wantWarnings, warnings); diff != "" {
				t.Errorf("applyQuota() warnings mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestApplyQuota_headroom(t *testing.T) {
	t.Parallel()

	d4 := vmOf(t, skuOf("Standard_D4s_v5", "standardDSv5Family", 4, 16))
	quotas := map[string]quota{
		"standardDSv5Family": {Limit: 10, Used: 2},
		regionalCores:        {Limit: 100, Used: 40},
	}

	var summary Summary

	kept, _ := applyQuota([]VM{d4}, quotas, false, &summary, discard)
	if len(kept) != 1 {
		t.Fatalf("applyQuota() kept %d VMs, want 1", len(kept))
	}

	want := []QuotaHeadroom{
		{Name: "standardDSv5Family", Limit: 10, Used: 2, Free: 8},
		{Name: regionalCores, Limit: 100, Used: 40, Free: 60},
	}
	if diff := cmp.Diff(want, kept[0].Quota); diff != "" {
		t.Errorf("applyQuota() quota mismatch (-want +got):\n%s", diff)
	}
}

func TestAssess(t *testing.T) {
	t.Parallel()

	skus := []sku{
		skuOf("Standard_D2s_v5", "standardDSv5Family", 2, 8),
		skuOf("Standard_D4s_v5", "standardDSv5Family", 4, 16),
		skuOf("Standard_E2s_v5", "standardESv5Family", 2, 16),
	}
	quotas := map[string]quota{
		"standardDSv5Family": {Limit: 10, Used: 8},
		regionalCores:        {Limit: 100},
	}
	prices := map[string]priceEntry{"Standard_D2s_v5": {onDemand: new(0.1)}}

	var summary Summary

	vms, warnings := assess(skus, quotas, prices, Filter{Categories: []Category{CategoryGeneral}}, &summary, discard)

	if diff := cmp.Diff([]string{"Standard_D2s_v5"}, names(vms)); diff != "" {
		t.Fatalf("assess() names mismatch (-want +got):\n%s", diff)
	}

	if got := vms[0].Pricing.OnDemandHourly; got == nil || *got != 0.1 {
		t.Errorf("assess() hourly price = %v, want 0.1", got)
	}

	if diff := cmp.Diff(Summary{Considered: 3, Matched: 2, NoQuota: 1}, summary); diff != "" {
		t.Errorf("assess() summary mismatch (-want +got):\n%s", diff)
	}

	if len(warnings) != 0 {
		t.Errorf("assess() warnings = %v, want none", warnings)
	}
}

func TestRank(t *testing.T) {
	t.Parallel()

	monthly := func(name string, price float64) VM {
		return VM{Name: name, Pricing: Pricing{OnDemandHourly: new(price), OnDemandMonthly: new(price * HoursPerMonth)}}
	}

	tests := []struct {
		name        string
		limit       int
		wantNames   []string
		wantSummary Summary
	}{
		{name: "no limit", limit: 0, wantNames: []string{"a", "b", "c"}, wantSummary: Summary{Eligible: 3, Shown: 3}},
		{name: "limit applied after sorting", limit: 2, wantNames: []string{"a", "b"}, wantSummary: Summary{Eligible: 3, Shown: 2}},
		{name: "limit above count", limit: 5, wantNames: []string{"a", "b", "c"}, wantSummary: Summary{Eligible: 3, Shown: 3}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			vms := []VM{monthly("c", 3), monthly("a", 1), monthly("b", 2)}

			var summary Summary

			got := rank(vms, Filter{Limit: tc.limit}, &summary, discard)

			if diff := cmp.Diff(tc.wantNames, names(got)); diff != "" {
				t.Errorf("rank() names mismatch (-want +got):\n%s", diff)
			}

			if diff := cmp.Diff(tc.wantSummary, summary); diff != "" {
				t.Errorf("rank() summary mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

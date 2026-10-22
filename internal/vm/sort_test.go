package vm

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseSort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want Sort
	}{
		{in: "", want: Sort{Key: SortMonthly}},
		{in: "  ", want: Sort{Key: SortMonthly}},
		{in: "memory", want: Sort{Key: SortMemory}},
		{in: "MEMORY:DESC", want: Sort{Key: SortMemory, Desc: true}},
		{in: " name : asc ", want: Sort{Key: SortName}},
		{in: "name:", want: Sort{Key: SortName}},
		{in: "eviction:desc", want: Sort{Key: SortEviction, Desc: true}},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()

			got, err := ParseSort(tc.in)
			if err != nil || got != tc.want {
				t.Errorf("ParseSort(%q) = %+v, %v, want %+v, nil", tc.in, got, err, tc.want)
			}
		})
	}

	errTests := []struct {
		in      string
		wantErr string
	}{
		{in: "zones", wantErr: "choose one of: name, vcpus"},
		{in: "name:up", wantErr: `unknown direction "up"`},
	}

	for _, tc := range errTests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()

			_, err := ParseSort(tc.in)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("ParseSort(%q) error = %v, want one containing %q", tc.in, err, tc.wantErr)
			}
		})
	}
}

func TestSort_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		sort Sort
		want string
	}{
		{sort: Sort{Key: SortMonthly}, want: "monthly"},
		{sort: Sort{Key: SortMemory, Desc: true}, want: "memory:desc"},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()

			if got := tc.sort.String(); got != tc.want {
				t.Errorf("%+v.String() = %q, want %q", tc.sort, got, tc.want)
			}
		})
	}
}

func TestSort_NeedsSpot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key  SortKey
		want bool
	}{
		{key: SortMonthly, want: false},
		{key: SortHourly, want: false},
		{key: SortSpot, want: true},
		{key: SortEviction, want: true},
	}

	for _, tc := range tests {
		t.Run(string(tc.key), func(t *testing.T) {
			t.Parallel()

			if got := (Sort{Key: tc.key}).NeedsSpot(); got != tc.want {
				t.Errorf("Sort{Key: %q}.NeedsSpot() = %t, want %t", tc.key, got, tc.want)
			}
		})
	}
}

func TestSortKeyNames(t *testing.T) {
	t.Parallel()

	// The table headers in order, which the help text lists.
	want := []string{"name", "vcpus", "memory", "nics", "gpus", "arch", "category", "freequota", "hourly", "monthly", "spot", "eviction"}

	if diff := cmp.Diff(want, SortKeyNames()); diff != "" {
		t.Errorf("SortKeyNames() mismatch (-want +got):\n%s", diff)
	}
}

func TestSortVMs(t *testing.T) {
	t.Parallel()

	priced := func(name string, hourly float64) VM {
		return VM{Name: name, Pricing: Pricing{OnDemandHourly: new(hourly), OnDemandMonthly: new(hourly * HoursPerMonth)}}
	}

	unpriced := func(name string) VM { return VM{Name: name} }

	// spot makes a VM with Spot information; an empty rate means none.
	spot := func(name string, price *float64, rate string) VM {
		v := VM{Name: name, Spot: &Spot{HourlyPrice: price}}
		if rate != "" {
			v.Spot.EvictionRate = &rate
		}

		return v
	}

	sized := func(name string, vcpus int, mem float64, gpus int) VM {
		return VM{Name: name, VCPUsAvailable: vcpus, MemoryGiB: mem, GPUs: gpus}
	}

	quota := func(name string, free ...int64) VM {
		v := VM{Name: name}
		for _, f := range free {
			v.Quota = append(v.Quota, QuotaHeadroom{Free: f})
		}

		return v
	}

	// spotOnly has no free on-demand quota, which would sort first ascending.
	spotOnly := func(name string) VM {
		v := quota(name, 0)
		v.SpotOnly = true

		return v
	}

	tests := []struct {
		name string
		sort Sort
		vms  []VM
		want []string
	}{
		{
			name: "monthly ascending, unpriced last",
			sort: Sort{Key: SortMonthly},
			vms:  []VM{unpriced("x"), priced("dear", 3), priced("cheap", 1)},
			want: []string{"cheap", "dear", "x"},
		},
		{
			name: "monthly descending, unpriced still last",
			sort: Sort{Key: SortMonthly, Desc: true},
			vms:  []VM{unpriced("x"), priced("cheap", 1), priced("dear", 3)},
			want: []string{"dear", "cheap", "x"},
		},
		{
			name: "hourly",
			sort: Sort{Key: SortHourly},
			vms:  []VM{priced("b", 2), unpriced("c"), priced("a", 1)},
			want: []string{"a", "b", "c"},
		},
		{
			name: "zero value is monthly",
			sort: Sort{},
			vms:  []VM{priced("dear", 3), priced("cheap", 1)},
			want: []string{"cheap", "dear"},
		},
		{
			name: "unknown key falls back to monthly",
			sort: Sort{Key: "zones"},
			vms:  []VM{priced("dear", 3), priced("cheap", 1)},
			want: []string{"cheap", "dear"},
		},
		{
			name: "name ascending",
			sort: Sort{Key: SortName},
			vms:  []VM{unpriced("b"), unpriced("c"), unpriced("a")},
			want: []string{"a", "b", "c"},
		},
		{
			name: "name descending",
			sort: Sort{Key: SortName, Desc: true},
			vms:  []VM{unpriced("b"), unpriced("c"), unpriced("a")},
			want: []string{"c", "b", "a"},
		},
		{
			name: "vcpus descending",
			sort: Sort{Key: SortVCPUs, Desc: true},
			vms:  []VM{sized("two", 2, 0, 0), sized("eight", 8, 0, 0), sized("four", 4, 0, 0)},
			want: []string{"eight", "four", "two"},
		},
		{
			name: "memory",
			sort: Sort{Key: SortMemory},
			vms:  []VM{sized("big", 0, 64, 0), sized("small", 0, 3.5, 0)},
			want: []string{"small", "big"},
		},
		{
			name: "nics, unknown last",
			sort: Sort{Key: SortNICs},
			vms:  []VM{{Name: "unknown"}, {Name: "eight", MaxNICs: new(8)}, {Name: "two", MaxNICs: new(2)}},
			want: []string{"two", "eight", "unknown"},
		},
		{
			name: "nics descending, unknown still last",
			sort: Sort{Key: SortNICs, Desc: true},
			vms:  []VM{{Name: "unknown"}, {Name: "two", MaxNICs: new(2)}, {Name: "eight", MaxNICs: new(8)}},
			want: []string{"eight", "two", "unknown"},
		},
		{
			name: "gpus descending",
			sort: Sort{Key: SortGPUs, Desc: true},
			vms:  []VM{sized("none", 0, 0, 0), sized("two", 0, 0, 2), sized("one", 0, 0, 1)},
			want: []string{"two", "one", "none"},
		},
		{
			name: "arch, unknown last",
			sort: Sort{Key: SortArch},
			vms:  []VM{{Name: "a", Arch: ArchX64}, {Name: "b", Arch: ArchARM64}, {Name: "c", Arch: archUnknown}},
			want: []string{"b", "a", "c"},
		},
		{
			name: "arch descending, unknown still last",
			sort: Sort{Key: SortArch, Desc: true},
			vms:  []VM{{Name: "c", Arch: archUnknown}, {Name: "b", Arch: ArchARM64}, {Name: "a", Arch: ArchX64}},
			want: []string{"a", "b", "c"},
		},
		{
			name: "category, other last",
			sort: Sort{Key: SortCategory},
			vms:  []VM{{Name: "a", Category: categoryOther}, {Name: "b", Category: CategoryStorage}, {Name: "c", Category: CategoryMemory}},
			want: []string{"c", "b", "a"},
		},
		{
			name: "category descending, other still last",
			sort: Sort{Key: SortCategory, Desc: true},
			vms:  []VM{{Name: "a", Category: categoryOther}, {Name: "b", Category: CategoryStorage}, {Name: "c", Category: CategoryMemory}},
			want: []string{"b", "c", "a"},
		},
		{
			name: "category",
			sort: Sort{Key: SortCategory},
			vms:  []VM{{Name: "a", Category: CategoryMemory}, {Name: "b", Category: CategoryBurst}},
			want: []string{"b", "a"},
		},
		{
			name: "free quota by the smallest, none last",
			sort: Sort{Key: SortQuota},
			vms:  []VM{quota("none"), quota("eight", 8, 20), quota("two", 50, 2)},
			want: []string{"two", "eight", "none"},
		},
		{
			name: "free quota descending, none last",
			sort: Sort{Key: SortQuota, Desc: true},
			vms:  []VM{quota("none"), quota("two", 2), quota("eight", 8)},
			want: []string{"eight", "two", "none"},
		},
		{
			name: "free quota, Spot only last",
			sort: Sort{Key: SortQuota},
			vms:  []VM{spotOnly("spot-only"), quota("eight", 8), quota("two", 2)},
			want: []string{"two", "eight", "spot-only"},
		},
		{
			name: "free quota descending, Spot only still last",
			sort: Sort{Key: SortQuota, Desc: true},
			vms:  []VM{spotOnly("spot-only"), quota("two", 2), quota("eight", 8)},
			want: []string{"eight", "two", "spot-only"},
		},
		{
			name: "spot, no Spot or no price last",
			sort: Sort{Key: SortSpot},
			vms:  []VM{unpriced("no-spot"), spot("no-price", nil, ""), spot("dear", new(0.5), ""), spot("cheap", new(0.1), "")},
			want: []string{"cheap", "dear", "no-price", "no-spot"},
		},
		{
			name: "eviction by band, unknown last",
			sort: Sort{Key: SortEviction},
			vms:  []VM{unpriced("no-spot"), spot("unknown", nil, ""), spot("high", nil, "20+"), spot("low", nil, "0-5"), spot("mid", nil, "5-10")},
			want: []string{"low", "mid", "high", "no-spot", "unknown"},
		},
		{
			name: "eviction descending, unknown last",
			sort: Sort{Key: SortEviction, Desc: true},
			vms:  []VM{spot("unknown", nil, ""), spot("low", nil, "0-5"), spot("high", nil, "20+")},
			want: []string{"high", "low", "unknown"},
		},
		{
			// Ties go to the cheapest hourly price whatever the direction.
			name: "ties broken by cheapest hourly when descending",
			sort: Sort{Key: SortCategory, Desc: true},
			vms:  []VM{priced("dear", 3), priced("cheap", 1), unpriced("unpriced")},
			want: []string{"cheap", "dear", "unpriced"},
		},
		{
			name: "then by fewest vCPUs, then name",
			sort: Sort{Key: SortCategory},
			vms:  []VM{sized("b", 4, 0, 0), sized("c", 2, 0, 0), sized("a", 4, 0, 0)},
			want: []string{"c", "a", "b"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sortVMs(tc.vms, tc.sort)

			if diff := cmp.Diff(tc.want, names(tc.vms)); diff != "" {
				t.Errorf("sortVMs(%s) mismatch (-want +got):\n%s", tc.sort, diff)
			}
		})
	}
}

func TestEvictionLow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in     string
		want   float64
		wantOK bool
	}{
		{in: "0-5", want: 0, wantOK: true},
		{in: "5-10", want: 5, wantOK: true},
		{in: "20+", want: 20, wantOK: true},
		{in: "2.5-5", want: 2.5, wantOK: true},
		{in: "15", want: 15, wantOK: true},
		{in: "", want: 0, wantOK: false},
		{in: "n/a", want: 0, wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()

			got, ok := evictionLow(tc.in)
			if got != tc.want || ok != tc.wantOK {
				t.Errorf("evictionLow(%q) = %g, %t, want %g, %t", tc.in, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

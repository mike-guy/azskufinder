package vm

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// SortKey names a column the results can be sorted by. The names are the table
// headers in lower case, so what a person sees is what they type.
type SortKey string

// The columns that can be sorted by. Zones is not one of them: a list of zones
// has no natural order.
const (
	SortName     SortKey = "name"
	SortVCPUs    SortKey = "vcpus"
	SortMemory   SortKey = "memory"
	SortNICs     SortKey = "nics"
	SortGPUs     SortKey = "gpus"
	SortArch     SortKey = "arch"
	SortCategory SortKey = "category"
	SortQuota    SortKey = "freequota"
	SortHourly   SortKey = "hourly"
	SortMonthly  SortKey = "monthly"
	SortSpot     SortKey = "spot"
	SortEviction SortKey = "eviction"
)

// DefaultSortKey is what the results are sorted by when nothing else is asked.
const DefaultSortKey = SortMonthly

const (
	dirAsc  = "asc"
	dirDesc = "desc"
)

// sortColumn says how to order by one column, compared as a number or as text.
// A VM may have no value for a column, such as an unknown price or an
// architecture Azure did not report, and then it sorts last.
type sortColumn struct {
	key    SortKey
	number func(VM) (float64, bool)
	text   func(VM) (string, bool)
}

// sortColumns are the columns that can be sorted by, in table order.
var sortColumns = []sortColumn{
	{key: SortName, text: func(v VM) (string, bool) { return v.Name, true }},
	{key: SortVCPUs, number: func(v VM) (float64, bool) { return float64(v.VCPUsAvailable), true }},
	{key: SortMemory, number: func(v VM) (float64, bool) { return v.MemoryGiB, true }},
	{key: SortNICs, number: func(v VM) (float64, bool) {
		if v.MaxNICs == nil {
			return 0, false
		}

		return float64(*v.MaxNICs), true
	}},
	{key: SortGPUs, number: func(v VM) (float64, bool) { return float64(v.GPUs), true }},
	{key: SortArch, text: func(v VM) (string, bool) { return string(v.Arch), v.Arch != archUnknown }},
	{key: SortCategory, text: func(v VM) (string, bool) { return string(v.Category), v.Category != categoryOther }},
	{key: SortQuota, number: func(v VM) (float64, bool) {
		// A Spot-only VM has no on-demand quota to speak of, and the table shows
		// "Spot only" rather than a number, so it sorts with the VMs that have none.
		if v.SpotOnly {
			return 0, false
		}

		free, ok := v.MinQuotaFree()

		return float64(free), ok
	}},
	{key: SortHourly, number: func(v VM) (float64, bool) { return price(v.Pricing.OnDemandHourly) }},
	{key: SortMonthly, number: func(v VM) (float64, bool) { return price(v.Pricing.OnDemandMonthly) }},
	{key: SortSpot, number: func(v VM) (float64, bool) {
		if v.Spot == nil {
			return 0, false
		}

		return price(v.Spot.HourlyPrice)
	}},
	{key: SortEviction, number: func(v VM) (float64, bool) {
		if v.Spot == nil || v.Spot.EvictionRate == nil {
			return 0, false
		}

		return evictionLow(*v.Spot.EvictionRate)
	}},
}

// columnFor returns the sort column of key.
func columnFor(key SortKey) (sortColumn, bool) {
	i := slices.IndexFunc(sortColumns, func(c sortColumn) bool { return c.key == key })
	if i < 0 {
		return sortColumn{}, false
	}

	return sortColumns[i], true
}

// SortKeyNames lists the columns that can be sorted by, in table order.
func SortKeyNames() []string {
	names := make([]string, len(sortColumns))
	for i, c := range sortColumns {
		names[i] = string(c.key)
	}

	return names
}

// Sort is the order of the results: one column, ascending unless Desc is set.
// The zero value is the default, by monthly price, cheapest first.
type Sort struct {
	Key  SortKey
	Desc bool
}

// String returns the sort as it is written on the command line, such as
// "monthly" or "memory:desc".
func (s Sort) String() string {
	if s.Desc {
		return string(s.Key) + ":" + dirDesc
	}

	return string(s.Key)
}

// NeedsSpot reports whether the sort uses Spot data, which is then needed for
// every eligible VM and not only the ones shown.
func (s Sort) NeedsSpot() bool {
	return s.Key == SortSpot || s.Key == SortEviction
}

// ParseSort reads a sort such as "monthly", "memory:desc" or "name:asc". Empty
// means the default. Keys and directions are not case sensitive.
func ParseSort(s string) (Sort, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return Sort{Key: DefaultSortKey}, nil
	}

	key, dir, _ := strings.Cut(s, ":")

	sort := Sort{Key: SortKey(strings.TrimSpace(key))}

	if _, ok := columnFor(sort.Key); !ok {
		return Sort{}, fmt.Errorf("unknown column %q, choose one of: %s", key, strings.Join(SortKeyNames(), ", "))
	}

	switch strings.TrimSpace(dir) {
	case "", dirAsc:
	case dirDesc:
		sort.Desc = true
	default:
		return Sort{}, fmt.Errorf("unknown direction %q, use %s or %s, as in %s:%s", dir, dirAsc, dirDesc, sort.Key, dirDesc)
	}

	return sort, nil
}

// sortVMs orders the VMs. VMs without a value for the column come last, in
// either direction, so that an unknown price never looks like the cheapest or
// the dearest. Ties are broken the same way whatever the direction: cheapest
// hourly price, then fewest vCPUs, then name. Names are unique, so no two VMs
// tie and the order is always the same.
func sortVMs(vms []VM, by Sort) {
	column, ok := columnFor(by.Key)
	if !ok {
		column, _ = columnFor(DefaultSortKey)
	}

	hourly, _ := columnFor(SortHourly)

	slices.SortFunc(vms, func(a, b VM) int {
		return cmp.Or(
			compareColumn(column, a, b, by.Desc),
			compareColumn(hourly, a, b, false),
			cmp.Compare(a.VCPUsAvailable, b.VCPUsAvailable),
			strings.Compare(a.Name, b.Name),
		)
	})
}

func compareColumn(c sortColumn, a, b VM, desc bool) int {
	if c.text != nil {
		return compareValues(c.text, a, b, desc)
	}

	return compareValues(c.number, a, b, desc)
}

// compareValues orders a and b by value. A VM with no value comes last in
// either direction.
func compareValues[T cmp.Ordered](value func(VM) (T, bool), a, b VM, desc bool) int {
	av, aok := value(a)
	bv, bok := value(b)

	switch {
	case !aok && !bok:
		return 0
	case !aok:
		return 1
	case !bok:
		return -1
	case desc:
		return cmp.Compare(bv, av)
	default:
		return cmp.Compare(av, bv)
	}
}

func price(p *float64) (float64, bool) {
	if p == nil {
		return 0, false
	}

	return *p, true
}

// evictionLow reads the bottom of an eviction rate band such as "5-10" or "20+",
// which orders the bands. A rate that is missing or not understood has no value.
func evictionLow(rate string) (float64, bool) {
	end := strings.IndexFunc(rate, func(r rune) bool { return !unicode.IsDigit(r) && r != '.' })
	if end < 0 {
		end = len(rate)
	}

	low, err := strconv.ParseFloat(rate[:end], 64)

	return low, err == nil
}

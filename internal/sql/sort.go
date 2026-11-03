package sql

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// SortKey names a column the results can be sorted by. The names are the table
// headers in lower case, so what a person sees is what they type.
type SortKey string

// The columns that can be sorted by. Not every command has every column:
// VCoreSortKeyNames and DTUSortKeyNames list each command's.
const (
	SortName       SortKey = "name"
	SortTier       SortKey = "tier"
	SortCompute    SortKey = "compute"
	SortHardware   SortKey = "hardware"
	SortVCores     SortKey = "vcores"
	SortDTUs       SortKey = "dtus"
	SortMemory     SortKey = "memory"
	SortMaxStorage SortKey = "maxstorage"
	SortQuota      SortKey = "freequota"
	SortHourly     SortKey = "hourly"
	SortMonthly    SortKey = "monthly"
)

// DefaultSortKey is what the results are sorted by when nothing else is asked.
const DefaultSortKey = SortMonthly

const (
	dirAsc  = "asc"
	dirDesc = "desc"
)

// sortColumn says how to order rows of T by one column, compared as a number
// or as text. A row may have no value for a column, such as an unknown price,
// and then it sorts last.
type sortColumn[T any] struct {
	key    SortKey
	number func(T) (float64, bool)
	text   func(T) (string, bool)
}

// vcoreSortColumns are the columns vCore databases can be sorted by, in table
// order.
var vcoreSortColumns = []sortColumn[VCoreDatabase]{
	{key: SortName, text: func(d VCoreDatabase) (string, bool) { return d.Name, true }},
	{key: SortTier, text: func(d VCoreDatabase) (string, bool) { return string(d.Tier), true }},
	{key: SortCompute, text: func(d VCoreDatabase) (string, bool) { return string(d.Compute), true }},
	{key: SortHardware, text: func(d VCoreDatabase) (string, bool) { return string(d.Hardware), d.Hardware != hardwareOther }},
	{key: SortVCores, number: func(d VCoreDatabase) (float64, bool) { return float64(d.VCores), true }},
	{key: SortMemory, number: func(d VCoreDatabase) (float64, bool) { return floatValue(d.MemoryGiB) }},
	{key: SortMaxStorage, number: func(d VCoreDatabase) (float64, bool) { return floatValue(d.MaxStorageGiB) }},
	{key: SortQuota, number: func(d VCoreDatabase) (float64, bool) {
		free, ok := d.MinQuotaFree()

		return float64(free), ok
	}},
	{key: SortHourly, number: func(d VCoreDatabase) (float64, bool) { return floatValue(d.Pricing.Hourly) }},
	{key: SortMonthly, number: func(d VCoreDatabase) (float64, bool) { return floatValue(d.Pricing.Monthly) }},
}

// dtuSortColumns are the columns DTU databases can be sorted by, in table
// order.
var dtuSortColumns = []sortColumn[DTUDatabase]{
	{key: SortName, text: func(d DTUDatabase) (string, bool) { return d.Name, true }},
	{key: SortTier, text: func(d DTUDatabase) (string, bool) { return string(d.Tier), true }},
	{key: SortDTUs, number: func(d DTUDatabase) (float64, bool) { return float64(d.DTUs), true }},
	{key: SortMaxStorage, number: func(d DTUDatabase) (float64, bool) { return floatValue(d.MaxStorageGiB) }},
	{key: SortQuota, number: func(d DTUDatabase) (float64, bool) {
		free, ok := d.MinQuotaFree()

		return float64(free), ok
	}},
	{key: SortHourly, number: func(d DTUDatabase) (float64, bool) { return floatValue(d.Pricing.Hourly) }},
	{key: SortMonthly, number: func(d DTUDatabase) (float64, bool) { return floatValue(d.Pricing.Monthly) }},
}

// VCoreSortKeyNames lists the columns vCore databases can be sorted by, in
// table order.
func VCoreSortKeyNames() []string { return keyNames(vcoreSortColumns) }

// DTUSortKeyNames lists the columns DTU databases can be sorted by, in table
// order.
func DTUSortKeyNames() []string { return keyNames(dtuSortColumns) }

func keyNames[T any](columns []sortColumn[T]) []string {
	out := make([]string, len(columns))
	for i, c := range columns {
		out[i] = string(c.key)
	}

	return out
}

func columnFor[T any](columns []sortColumn[T], key SortKey) (sortColumn[T], bool) {
	i := slices.IndexFunc(columns, func(c sortColumn[T]) bool { return c.key == key })
	if i < 0 {
		return sortColumn[T]{}, false
	}

	return columns[i], true
}

// Sort is the order of the results: one column, ascending unless Desc is set.
type Sort struct {
	Key  SortKey
	Desc bool
}

// String returns the sort as it is written on the command line, such as
// "monthly" or "vcores:desc".
func (s Sort) String() string {
	if s.Desc {
		return string(s.Key) + ":" + dirDesc
	}

	return string(s.Key)
}

// ParseVCoreSort reads a vCore sort such as "monthly" or "vcores:desc".
// Empty means the default.
func ParseVCoreSort(s string) (Sort, error) { return parseSort(s, vcoreSortColumns) }

// ParseDTUSort reads a DTU sort such as "monthly" or "dtus:desc". Empty means
// the default.
func ParseDTUSort(s string) (Sort, error) { return parseSort(s, dtuSortColumns) }

// parseSort reads a sort. Keys and directions are not case sensitive.
func parseSort[T any](s string, columns []sortColumn[T]) (Sort, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return Sort{Key: DefaultSortKey}, nil
	}

	key, dir, _ := strings.Cut(s, ":")

	sort := Sort{Key: SortKey(strings.TrimSpace(key))}

	if _, ok := columnFor(columns, sort.Key); !ok {
		return Sort{}, fmt.Errorf("unknown column %q, choose one of: %s", key, strings.Join(keyNames(columns), ", "))
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

// sortRows orders the rows by one column. Rows without a value for it come
// last in either direction, so that an unknown price never looks like the
// cheapest or the dearest. Ties are broken the same way whatever the
// direction: cheapest hourly price, then tieBreak, which must leave no two rows
// equal so that the order is always the same.
func sortRows[T any](rows []T, by Sort, columns []sortColumn[T], tieBreak func(a, b T) int) {
	column, ok := columnFor(columns, by.Key)
	if !ok {
		column, _ = columnFor(columns, DefaultSortKey)
	}

	hourly, _ := columnFor(columns, SortHourly)

	slices.SortFunc(rows, func(a, b T) int {
		return cmp.Or(
			compareColumn(column, a, b, by.Desc),
			compareColumn(hourly, a, b, false),
			tieBreak(a, b),
		)
	})
}

// vcoreTieBreak orders vCore databases by vCores, then name. Objective names
// are unique.
func vcoreTieBreak(a, b VCoreDatabase) int {
	return cmp.Or(cmp.Compare(a.VCores, b.VCores), strings.Compare(a.Name, b.Name))
}

// dtuTieBreak orders DTU databases by DTUs, then name.
func dtuTieBreak(a, b DTUDatabase) int {
	return cmp.Or(cmp.Compare(a.DTUs, b.DTUs), strings.Compare(a.Name, b.Name))
}

func compareColumn[T any](c sortColumn[T], a, b T, desc bool) int {
	if c.text != nil {
		return compareValues(c.text, a, b, desc)
	}

	return compareValues(c.number, a, b, desc)
}

// compareValues orders a and b by value. A row with no value comes last in
// either direction.
func compareValues[T any, V cmp.Ordered](value func(T) (V, bool), a, b T, desc bool) int {
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

func floatValue(p *float64) (float64, bool) {
	if p == nil {
		return 0, false
	}

	return *p, true
}

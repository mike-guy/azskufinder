package sql

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseSort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		parse   func(string) (Sort, error)
		in      string
		want    Sort
		wantErr string
	}{
		{name: "default", parse: ParseVCoreSort, in: "", want: Sort{Key: SortMonthly}},
		{name: "descending", parse: ParseVCoreSort, in: "VCores:Desc", want: Sort{Key: SortVCores, Desc: true}},
		{name: "ascending", parse: ParseDTUSort, in: "dtus:asc", want: Sort{Key: SortDTUs}},
		{name: "dtus is not a vCore column", parse: ParseVCoreSort, in: "dtus", wantErr: `unknown column "dtus", choose one of: name, tier, compute, hardware, vcores, memory, maxstorage, freequota, hourly, monthly`},
		{name: "vcores is not a DTU column", parse: ParseDTUSort, in: "vcores", wantErr: `unknown column "vcores", choose one of: name, tier, dtus, maxstorage, freequota, hourly, monthly`},
		{name: "unknown direction", parse: ParseDTUSort, in: "name:up", wantErr: `unknown direction "up"`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := tc.parse(tc.in)
			checkErr(t, err, tc.wantErr)

			if got != tc.want {
				t.Errorf("parse(%q) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestSortKeyNames(t *testing.T) {
	t.Parallel()

	wantVCore := []string{"name", "tier", "compute", "hardware", "vcores", "memory", "maxstorage", "freequota", "hourly", "monthly"}
	if diff := cmp.Diff(wantVCore, VCoreSortKeyNames()); diff != "" {
		t.Errorf("VCoreSortKeyNames() mismatch (-want +got):\n%s", diff)
	}

	wantDTU := []string{"name", "tier", "dtus", "maxstorage", "freequota", "hourly", "monthly"}
	if diff := cmp.Diff(wantDTU, DTUSortKeyNames()); diff != "" {
		t.Errorf("DTUSortKeyNames() mismatch (-want +got):\n%s", diff)
	}
}

func TestSortRows(t *testing.T) {
	t.Parallel()

	db := func(name string, vcores int, hw Hardware, monthly *float64) VCoreDatabase {
		d := VCoreDatabase{Name: name, VCores: vcores, Hardware: hw, Pricing: Pricing{Monthly: monthly}}
		if monthly != nil {
			d.Pricing.Hourly = new(*monthly / HoursPerMonth)
		}

		return d
	}

	rows := func() []VCoreDatabase {
		return []VCoreDatabase{
			db("GP_Gen5_8", 8, HardwareStandard, new(800.0)),
			db("unpriced", 2, hardwareOther, nil),
			db("GP_DC_2", 2, HardwareDC, new(300.0)),
			db("GP_Gen5_2", 2, HardwareStandard, new(200.0)),
		}
	}

	tests := []struct {
		name string
		by   Sort
		want []string
	}{
		{name: "cheapest first, unpriced last", by: Sort{Key: SortMonthly}, want: []string{"GP_Gen5_2", "GP_DC_2", "GP_Gen5_8", "unpriced"}},
		{name: "dearest first, unpriced still last", by: Sort{Key: SortMonthly, Desc: true}, want: []string{"GP_Gen5_8", "GP_DC_2", "GP_Gen5_2", "unpriced"}},
		{name: "most vCores first, ties by price", by: Sort{Key: SortVCores, Desc: true}, want: []string{"GP_Gen5_8", "GP_Gen5_2", "GP_DC_2", "unpriced"}},
		{name: "unknown hardware last", by: Sort{Key: SortHardware}, want: []string{"GP_DC_2", "GP_Gen5_2", "GP_Gen5_8", "unpriced"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := rows()
			sortRows(got, tc.by, vcoreSortColumns, vcoreTieBreak)

			names := make([]string, len(got))
			for i, d := range got {
				names[i] = d.Name
			}

			if diff := cmp.Diff(tc.want, names); diff != "" {
				t.Errorf("sortRows(%s) mismatch (-want +got):\n%s", tc.by, diff)
			}
		})
	}
}

func TestDTUTieBreak(t *testing.T) {
	t.Parallel()

	rows := []DTUDatabase{{Name: "S3", DTUs: 100}, {Name: "P1", DTUs: 125}, {Name: "S2", DTUs: 50}}
	sortRows(rows, Sort{Key: SortHourly}, dtuSortColumns, dtuTieBreak)

	want := []DTUDatabase{{Name: "S2", DTUs: 50}, {Name: "S3", DTUs: 100}, {Name: "P1", DTUs: 125}}
	if diff := cmp.Diff(want, rows); diff != "" {
		t.Errorf("sortRows() mismatch (-want +got):\n%s", diff)
	}
}

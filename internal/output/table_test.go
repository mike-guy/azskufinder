package output

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

type tableRow struct {
	name  string
	zones string
	price string
}

func tableColumns(nameStyle func(string) string) []Column[tableRow] {
	return []Column[tableRow]{
		{Header: "NAME", Value: func(r tableRow) string { return r.name }, Style: nameStyle},
		{Header: "ZONES", Value: func(r tableRow) string { return r.zones }},
		{Header: "PRICE", Value: func(r tableRow) string { return r.price }, Align: AlignRight},
	}
}

func TestTable(t *testing.T) {
	t.Parallel()

	rows := []tableRow{
		{name: "Standard_D2s_v5", zones: "1,2,3", price: "£0.0960"},
		{name: "Standard_B1s", zones: "-", price: "£0.0104"},
		{name: "Standard_E64ds_v5", zones: "1", price: "£4.6080"},
	}

	colour := NewStyles(true)

	tests := []struct {
		name   string
		styles Styles
		cols   []Column[tableRow]
		rows   []tableRow
	}{
		{name: "plain", styles: Styles{}, cols: tableColumns(nil), rows: rows},
		{name: "colour", styles: colour, cols: tableColumns(colour.Bold), rows: rows},
		{
			name:   "sanitised cells",
			styles: colour,
			cols:   tableColumns(colour.Bold),
			rows: []tableRow{
				{name: "\x1b]0;title\aStandard_D2s_v5", zones: "1\r2", price: "£0.0960"},
			},
		},
		{name: "no rows", styles: Styles{}, cols: tableColumns(nil), rows: nil},
		{
			name:   "wide runes",
			styles: Styles{},
			cols:   tableColumns(nil),
			rows: []tableRow{
				{name: "日本東部", zones: "1", price: "¥12.50"},
				{name: "Standard_B1s", zones: "-", price: "£0.0104"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			if err := Table(&buf, tc.styles, tc.cols, tc.rows); err != nil {
				t.Fatalf("Table() error = %v", err)
			}

			golden(t, "table_"+strings.ReplaceAll(tc.name, " ", "_"), buf.String())
		})
	}
}

func TestTable_writeError(t *testing.T) {
	t.Parallel()

	err := Table(errWriter{}, Styles{}, tableColumns(nil), nil)
	if !errors.Is(err, errWrite) {
		t.Errorf("Table(errWriter) error = %v, want %v", err, errWrite)
	}

	if err != nil && !strings.HasPrefix(err.Error(), "writing table") {
		t.Errorf("Table(errWriter) error = %q, want it to start with %q", err, "writing table")
	}
}

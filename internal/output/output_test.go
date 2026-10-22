package output

import (
	"strings"
	"testing"
)

func TestParseFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "table", in: "table", want: FormatTable},
		{name: "upper case", in: "JSON", want: FormatJSON},
		{name: "surrounding space", in: "  table ", want: FormatTable},
		{name: "empty", in: "", wantErr: true},
		{name: "unknown", in: "yaml", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseFormat(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseFormat(%q) = %q, want an error", tc.in, got)
				}

				for _, s := range []string{`"` + tc.in + `"`, "table, json"} {
					if !strings.Contains(err.Error(), s) {
						t.Errorf("ParseFormat(%q) error = %q, want it to contain %q", tc.in, err, s)
					}
				}

				return
			}

			if err != nil {
				t.Fatalf("ParseFormat(%q) error = %v", tc.in, err)
			}

			if got != tc.want {
				t.Errorf("ParseFormat(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

package cli

import (
	"bytes"
	"testing"

	"github.com/mike-guy/azskufinder/internal/output"
	"github.com/mike-guy/azskufinder/internal/vm"
)

func TestPrintWarnings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		warnings []vm.Warning
		want     string
	}{
		{name: "none", warnings: []vm.Warning{}, want: ""},
		{
			name: "two",
			warnings: []vm.Warning{
				{Code: vm.WarningPricingUnavailable, Message: "prices could not be fetched"},
				{Code: vm.WarningQuotaMissing, Message: "no quota for standardDSv5Family"},
			},
			want: "warning: prices could not be fetched\nwarning: no quota for standardDSv5Family\n",
		},
		{
			name:     "control characters removed",
			warnings: []vm.Warning{{Message: "bad\x1b[2Jtext\r"}},
			want:     "warning: bad[2Jtext\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			printWarnings(&buf, output.Styles{}, tc.warnings)

			if got := buf.String(); got != tc.want {
				t.Errorf("printWarnings() wrote %q, want %q", got, tc.want)
			}
		})
	}
}

package cli

import (
	"bytes"
	"testing"

	"github.com/mike-guy/azskufinder/internal/output"
)

func TestPrintWarning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		messages []string
		want     string
	}{
		{name: "none", messages: []string{}, want: ""},
		{
			name:     "two",
			messages: []string{"prices could not be fetched", "no quota for standardDSv5Family"},
			want:     "warning: prices could not be fetched\nwarning: no quota for standardDSv5Family\n",
		},
		{
			name:     "control characters removed",
			messages: []string{"bad\x1b[2Jtext\r"},
			want:     "warning: bad[2Jtext\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			for _, m := range tc.messages {
				printWarning(&buf, output.Styles{}, m)
			}

			if got := buf.String(); got != tc.want {
				t.Errorf("printWarning() wrote %q, want %q", got, tc.want)
			}
		})
	}
}

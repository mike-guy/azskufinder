package vm

import "testing"

func TestVM_MinQuotaFree(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		quota  []QuotaHeadroom
		want   int64
		wantOK bool
	}{
		{name: "no quota", want: 0, wantOK: false},
		{name: "one entry", quota: []QuotaHeadroom{{Free: 8}}, want: 8, wantOK: true},
		{name: "smallest wins", quota: []QuotaHeadroom{{Free: 8}, {Free: 2}, {Free: 4}}, want: 2, wantOK: true},
		{name: "over used", quota: []QuotaHeadroom{{Free: 8}, {Free: -4}}, want: -4, wantOK: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := VM{Quota: tc.quota}.MinQuotaFree()
			if got != tc.want || ok != tc.wantOK {
				t.Errorf("MinQuotaFree() = %d, %t, want %d, %t", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

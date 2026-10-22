package cli

import (
	"testing"
	"testing/synctest"
	"time"
)

func TestWithTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		timeout time.Duration
		want    time.Duration // 0 means no deadline
	}{
		{name: "zero is no limit", timeout: 0, want: 0},
		{name: "negative is no limit", timeout: -time.Second, want: 0},
		{name: "duration", timeout: 90 * time.Second, want: 90 * time.Second},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := withTimeout(t.Context(), tc.timeout)
				t.Cleanup(cancel)

				deadline, ok := ctx.Deadline()

				switch {
				case tc.want == 0 && ok:
					t.Errorf("withTimeout(%v) deadline = %v, want none", tc.timeout, deadline)
				case tc.want != 0 && !ok:
					t.Errorf("withTimeout(%v) has no deadline, want one %v from now", tc.timeout, tc.want)
				case tc.want != 0 && deadline != time.Now().Add(tc.want):
					t.Errorf("withTimeout(%v) deadline = %v, want %v", tc.timeout, deadline, time.Now().Add(tc.want))
				}
			})
		})
	}
}

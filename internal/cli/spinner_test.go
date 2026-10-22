package cli

import "testing"

func TestSpinnerOn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		terminal bool
		debug    bool
		env      map[string]string
		want     bool
	}{
		{name: "not a terminal", terminal: false, env: map[string]string{"TERM": "xterm"}, want: false},
		{name: "debug logs on", terminal: true, debug: true, env: map[string]string{"TERM": "xterm"}, want: false},
		{name: "dumb terminal", terminal: true, env: map[string]string{"TERM": "dumb"}, want: false},
		{name: "terminal", terminal: true, env: map[string]string{"TERM": "xterm"}, want: true},
		{name: "TERM unset", terminal: true, want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := spinnerOn(tc.terminal, tc.debug, mapEnv(tc.env)); got != tc.want {
				t.Errorf("spinnerOn(%t, %t, %v) = %t, want %t", tc.terminal, tc.debug, tc.env, got, tc.want)
			}
		})
	}
}

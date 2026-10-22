package cli

import "testing"

func TestColourFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		terminal   bool
		noColour   bool
		env        map[string]string
		want       bool
		wantReason string
	}{
		{name: "terminal", terminal: true, env: map[string]string{"TERM": "xterm-256color"}, want: true},
		{name: "no-colour wins in a terminal", terminal: true, noColour: true, wantReason: "no-colour"},
		{name: "not a terminal", terminal: false, wantReason: "not a terminal"},
		{name: "NO_COLOR set", terminal: true, env: map[string]string{"NO_COLOR": "1"}, wantReason: "NO_COLOR"},
		{name: "NO_COLOR empty", terminal: true, env: map[string]string{"NO_COLOR": ""}, want: true},
		{name: "dumb terminal", terminal: true, env: map[string]string{"TERM": "dumb"}, wantReason: "TERM is dumb"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, reason := colourFor(tc.terminal, tc.noColour, mapEnv(tc.env))
			if got != tc.want || reason != tc.wantReason {
				t.Errorf("colourFor(%t, %t, %v) = %t, %q, want %t, %q",
					tc.terminal, tc.noColour, tc.env, got, reason, tc.want, tc.wantReason)
			}
		})
	}
}

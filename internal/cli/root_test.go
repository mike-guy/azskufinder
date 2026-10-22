package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestExitCode(t *testing.T) {
	t.Parallel()

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()

	tests := []struct {
		name string
		ctx  context.Context
		err  error
		want int
	}{
		{name: "success", ctx: t.Context(), err: nil, want: ExitSuccess},
		{name: "usage error", ctx: t.Context(), err: usageErrorf("--location is required"), want: ExitUsageError},
		{name: "wrapped usage error", ctx: t.Context(), err: fmt.Errorf("reading options: %w", usageErrorf("bad")), want: ExitUsageError},
		{name: "runtime error", ctx: t.Context(), err: errors.New("boom"), want: ExitGeneralError},
		{name: "timed out", ctx: t.Context(), err: fmt.Errorf("listing SKUs: %w", context.DeadlineExceeded), want: ExitGeneralError},
		{name: "interrupted", ctx: cancelled, err: fmt.Errorf("listing SKUs: %w", context.Canceled), want: ExitInterrupted},
		{name: "interrupted wins over a usage error", ctx: cancelled, err: usageErrorf("bad"), want: ExitInterrupted},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := exitCode(tc.ctx, tc.err); got != tc.want {
				t.Errorf("exitCode(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

// TestRoot runs the command tree as Execute does, and checks the exit code
// each case would give. Every case passes --config, and isolateEnv clears the
// AZSKUFINDER_* variables, so that the developer's own config file and
// environment cannot change the result. No case may pass validation: that
// would sign in to Azure.
func TestRoot(t *testing.T) { //nolint:paralleltest,tparallel // isolateEnv uses t.Setenv
	isolateEnv(t)

	config := writeConfig(t, "")
	badDebug := writeConfig(t, "debug: yes\n")
	missing := writeConfig(t, "") + ".missing"

	tests := []struct {
		name       string
		args       []string
		want       int
		wantStdout string // text stdout must contain
	}{
		{name: "version", args: []string{"--config", config, "--version"}, want: ExitSuccess, wantStdout: "azskufinder version test"},
		{name: "no subcommand shows help", args: []string{"--config", config}, want: ExitSuccess, wantStdout: "Usage:"},
		{name: "vm help", args: []string{"--config", config, "vm", "--help"}, want: ExitSuccess, wantStdout: "azskufinder vm [flags]"},
		{name: "unknown subcommand", args: []string{"--config", config, "nope"}, want: ExitUsageError},
		{name: "unknown flag", args: []string{"--config", config, "--nope"}, want: ExitUsageError},
		{name: "bad flag value", args: []string{"--config", config, "vm", "--cpus", "abc"}, want: ExitUsageError},
		{name: "unexpected argument", args: []string{"--config", config, "vm", "extra"}, want: ExitUsageError},
		{
			name: "invalid vm options",
			args: []string{"--config", config, "vm", "--subscription-id=", "--location", "uksouth"},
			want: ExitUsageError,
		},
		{name: "missing config file", args: []string{"--config", missing, "vm"}, want: ExitUsageError},
		{name: "invalid setting in config file", args: []string{"--config", badDebug}, want: ExitUsageError},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root, stdout, stderr := newTestRoot(t)
			root.SetArgs(tc.args)

			err := root.ExecuteContext(t.Context())

			if got := exitCode(t.Context(), err); got != tc.want {
				t.Fatalf("azskufinder %s: exit code %d (%v), want %d", strings.Join(tc.args, " "), got, err, tc.want)
			}

			if !strings.Contains(stdout.String(), tc.wantStdout) {
				t.Errorf("azskufinder %s: stdout = %q, want it to contain %q", strings.Join(tc.args, " "), stdout, tc.wantStdout)
			}

			// Only Execute prints errors, so the commands themselves stay quiet.
			if err != nil && stdout.Len()+stderr.Len() > 0 {
				t.Errorf("azskufinder %s: wrote stdout %q, stderr %q, want nothing on error",
					strings.Join(tc.args, " "), stdout, stderr)
			}
		})
	}
}

// TestRoot_vmOptionsReachRunE checks that RunE reports invalid options with
// their hint. The messages themselves are covered by TestReadVMOptions_errors.
func TestRoot_vmOptionsReachRunE(t *testing.T) { //nolint:paralleltest // isolateEnv uses t.Setenv
	isolateEnv(t)

	root, _, _ := newTestRoot(t)
	root.SetArgs([]string{"--config", writeConfig(t, ""), "vm", "--subscription-id=", "--location", "uksouth"})

	err := root.ExecuteContext(t.Context())
	checkUsageError(t, err, "--subscription-id is required", "Use --subscription-id or set AZSKUFINDER_SUBSCRIPTION_ID.")
}

func TestLogFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want []string
	}{
		{
			name: "plain error",
			err:  errors.New("boom"),
			want: []string{`msg="command failed"`, "error=boom", "error_type=*errors.errorString"},
		},
		{
			name: "azure response error",
			err:  fmt.Errorf("listing SKUs: %w", responseError(t, 403, "AuthorizationFailed")),
			want: []string{"error_type=*fmt.wrapError", "status_code=403", "error_code=AuthorizationFailed"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

			logFailure(log, tc.err)

			for _, want := range tc.want {
				if !strings.Contains(buf.String(), want) {
					t.Errorf("logFailure() logged %q, want it to contain %q", buf.String(), want)
				}
			}
		})
	}
}

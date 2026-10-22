package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/mike-guy/azskufinder/internal/azure"
	"github.com/mike-guy/azskufinder/internal/output"
)

func TestHintFor(t *testing.T) {
	t.Parallel()

	const (
		readerHint       = "The identity needs the Reader role on the subscription."
		locationHint     = "Check the location name; 'az account list-locations -o table' lists them."
		subscriptionHint = "Check the subscription ID; 'az account list -o table' lists the ones you can use."
	)

	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "usage error with a hint",
			err:  withHint(usageErrorf("--location is required"), "Use --location."),
			want: "Use --location.",
		},
		{
			name: "usage error without a hint falls through",
			err:  usageErrorf("waiting: %w", context.DeadlineExceeded),
			want: "Allow longer with --timeout or AZSKUFINDER_TIMEOUT.",
		},
		{
			name: "wrapped hint error",
			err:  fmt.Errorf("finding SKUs: %w", withHint(errors.New("boom"), "Try again.")),
			want: "Try again.",
		},
		{
			name: "timeout",
			err:  fmt.Errorf("listing SKUs: %w", context.DeadlineExceeded),
			want: "Allow longer with --timeout or AZSKUFINDER_TIMEOUT.",
		},
		{
			name: "sign in",
			err:  fmt.Errorf("listing SKUs: %w", azure.ErrSignIn),
			want: "Sign in with 'az login', or set AZURE_TENANT_ID, AZURE_CLIENT_ID and AZURE_CLIENT_SECRET.",
		},
		{
			name: "unauthorised",
			err:  fmt.Errorf("listing SKUs: %w", responseError(t, 401, "InvalidAuthenticationToken")),
			want: readerHint,
		},
		{
			name: "forbidden",
			err:  fmt.Errorf("listing SKUs: %w", responseError(t, 403, "AuthorizationFailed")),
			want: readerHint,
		},
		{
			name: "unknown location",
			err:  fmt.Errorf("listing compute usage: %w", responseError(t, 400, "NoRegisteredProviderFound")),
			want: locationHint,
		},
		{
			name: "location not available",
			err:  responseError(t, 400, "LocationNotAvailableForResourceType"),
			want: locationHint,
		},
		{
			name: "subscription not found",
			err:  fmt.Errorf("listing SKUs: %w", responseError(t, 404, "SubscriptionNotFound")),
			want: subscriptionHint,
		},
		{
			name: "invalid subscription",
			err:  responseError(t, 400, "InvalidSubscriptionId"),
			want: subscriptionHint,
		},
		{
			name: "other Azure error",
			err:  responseError(t, 404, "ResourceNotFound"),
			want: "",
		},
		{
			name: "plain error",
			err:  errors.New("boom"),
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := hintFor(tc.err); got != tc.want {
				t.Errorf("hintFor(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

func TestWithHint_usageError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		wrap bool
	}{
		{name: "bare"},
		{name: "wrapped", wrap: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := usageErrorf("--location is required")
			if tc.wrap {
				err = fmt.Errorf("reading options: %w", err)
			}

			got := withHint(err, "Use --location.")

			if got != err {
				t.Errorf("withHint() = %v, want the usage error returned as it was", got)
			}

			var usageErr *usageError
			if !errors.As(got, &usageErr) {
				t.Errorf("withHint() = %T, want it to stay a usage error", got)
			}

			if h := hintFor(got); h != "Use --location." {
				t.Errorf("hintFor(withHint()) = %q, want %q", h, "Use --location.")
			}
		})
	}
}

func TestWithHint_otherError(t *testing.T) {
	t.Parallel()

	base := errors.New("boom")

	got := withHint(base, "Try again.")

	if !errors.Is(got, base) {
		t.Errorf("withHint() = %v, want it to wrap the original error", got)
	}

	if got.Error() != "boom" {
		t.Errorf("withHint().Error() = %q, want %q", got.Error(), "boom")
	}

	var usageErr *usageError
	if errors.As(got, &usageErr) {
		t.Errorf("withHint() made a usage error from an ordinary one")
	}

	if h := hintFor(got); h != "Try again." {
		t.Errorf("hintFor(withHint()) = %q, want %q", h, "Try again.")
	}
}

func TestPrintError(t *testing.T) {
	t.Parallel()

	on := output.NewStyles(true)

	tests := []struct {
		name string
		st   output.Styles
		err  error
		want string
	}{
		{
			name: "no hint",
			err:  errors.New("boom"),
			want: "error: boom\n",
		},
		{
			name: "hint",
			err:  withHint(errors.New("boom"), "Try again."),
			want: "error: boom\nhint: Try again.\n",
		},
		{
			name: "Azure error on one line",
			err:  fmt.Errorf("listing SKUs: %w", responseError(t, 400, "InvalidSubscriptionId")),
			want: "error: listing SKUs: denied (InvalidSubscriptionId, HTTP 400)\n" +
				"hint: Check the subscription ID; 'az account list -o table' lists the ones you can use.\n",
		},
		{
			name: "control characters removed",
			err:  errors.New("bad\x1b]0;title\x07 \rtext\u009b"),
			want: "error: bad]0;title text\n",
		},
		{
			name: "colour",
			st:   on,
			err:  withHint(errors.New("boom"), "Try again."),
			want: on.Error("error: boom") + "\n" + on.Dim("hint: Try again.") + "\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			printError(&buf, tc.st, tc.err)

			if got := buf.String(); got != tc.want {
				t.Errorf("printError(%q) wrote %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

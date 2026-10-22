package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/spf13/cobra"

	"github.com/mike-guy/azskufinder/internal/azure"
	"github.com/mike-guy/azskufinder/internal/output"
)

// Errors are returned unchanged up to Execute, which is the only place that
// prints them: "error: <message>" in red on stderr, then an optional
// "hint: <what to do>" line. Messages say what went wrong; hints say what to
// do about it, so remediation never goes into the message text.

// usageError marks a mistake in how the user invoked the command, such as a
// missing required flag. Execute maps it to ExitUsageError.
type usageError struct {
	err  error
	hint string
}

// usageErrorf returns a usage error formatted as fmt.Errorf does, so %w keeps
// the cause matchable with errors.Is and errors.As.
func usageErrorf(format string, a ...any) error {
	return &usageError{err: fmt.Errorf(format, a...)}
}

func (e *usageError) Error() string { return e.err.Error() }

func (e *usageError) Unwrap() error { return e.err }

// hintError attaches a hint to an error that is not a usage error.
type hintError struct {
	err  error
	hint string
}

func (e *hintError) Error() string { return e.err.Error() }

func (e *hintError) Unwrap() error { return e.err }

// withHint attaches a hint, printed by Execute after the error, saying what to
// do about err.
func withHint(err error, hint string) error {
	var usageErr *usageError
	if errors.As(err, &usageErr) {
		usageErr.hint = hint

		return err
	}

	return &hintError{err: err, hint: hint}
}

// hintFor returns what to do about err, or "" when there is nothing useful to
// say. A hint attached with withHint wins; otherwise known failures are
// recognised here, in one place.
func hintFor(err error) string {
	var usageErr *usageError
	if errors.As(err, &usageErr) && usageErr.hint != "" {
		return usageErr.hint
	}

	var hintErr *hintError
	if errors.As(err, &hintErr) {
		return hintErr.hint
	}

	var respErr *azcore.ResponseError

	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "Allow longer with --timeout or " + envName("timeout") + "."
	case errors.Is(err, azure.ErrSignIn):
		return "Sign in with 'az login', or set AZURE_TENANT_ID, AZURE_CLIENT_ID and AZURE_CLIENT_SECRET."
	case errors.As(err, &respErr) &&
		(respErr.StatusCode == http.StatusUnauthorized || respErr.StatusCode == http.StatusForbidden):
		return "The identity needs the Reader role on the subscription."
	case errors.As(err, &respErr) &&
		(respErr.ErrorCode == "NoRegisteredProviderFound" || respErr.ErrorCode == "LocationNotAvailableForResourceType"):
		// What Azure answers for a location that does not exist, such as a typo.
		return "Check the location name; 'az account list-locations -o table' lists them."
	case errors.As(err, &respErr) &&
		(respErr.ErrorCode == "InvalidSubscriptionId" || respErr.ErrorCode == "SubscriptionNotFound"):
		return "Check the subscription ID; 'az account list -o table' lists the ones you can use."
	}

	return ""
}

// printError writes err, and its hint if it has one, to w, styled with st.
// Azure's error text is sanitised, as warnings are, so it cannot drive the
// terminal.
func printError(w io.Writer, st output.Styles, err error) {
	_, _ = fmt.Fprintln(w, st.Error("error: "+output.Sanitise(azure.ErrorText(err))))

	if hint := hintFor(err); hint != "" {
		_, _ = fmt.Fprintln(w, st.Dim("hint: "+hint))
	}
}

// usageArgs wraps a cobra positional-argument check, such as cobra.NoArgs, so
// that an unexpected argument is a usage error rather than a runtime failure.
func usageArgs(check cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := check(cmd, args); err != nil {
			return usageErrorf("%w", err)
		}

		return nil
	}
}

// flagUsageError makes a flag cobra cannot parse, such as an unknown flag or
// "--cpus abc", a usage error. Set on the root, it applies to every command.
func flagUsageError(_ *cobra.Command, err error) error {
	return usageErrorf("%w", err)
}

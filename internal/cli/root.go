// Package cli holds the cobra commands. It is the composition root: it reads
// and validates settings, builds the clients and services, and chooses how the
// results are shown.
package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/spf13/cobra"

	"github.com/mike-guy/azskufinder/internal/app"
	"github.com/mike-guy/azskufinder/internal/output"
)

// Exit codes returned by Execute.
const (
	ExitSuccess      = 0
	ExitGeneralError = 1
	ExitUsageError   = 2
	ExitInterrupted  = 130
)

// Execute runs azskufinder and returns the process exit code. It is the only
// place errors are printed: commands return them unchanged.
func Execute(ctx context.Context, ver string) int {
	application := app.New(ver)

	rootCmd := newRoot(application)

	// Decided from the environment now, so that an error raised before
	// PersistentPreRunE, such as a flag parse error, is styled for its stream.
	// PersistentPreRunE decides again once --no-colour can be read.
	setStyles(application, rootCmd, false, application.Logger)

	err := rootCmd.ExecuteContext(ctx)
	if err == nil {
		return ExitSuccess
	}

	logFailure(application.Logger, err)

	code := exitCode(ctx, err)
	if code == ExitInterrupted {
		application.Logger.Debug("interrupted")

		return code
	}

	if errors.Is(err, context.DeadlineExceeded) {
		application.Logger.Debug("timed out")
	}

	printError(rootCmd.ErrOrStderr(), application.Stderr, err)

	return code
}

// exitCode maps the outcome of a run to the process exit code. An interrupted
// run exits with ExitInterrupted whatever error it returned.
func exitCode(ctx context.Context, err error) int {
	var usageErr *usageError

	switch {
	case err == nil:
		return ExitSuccess
	case errors.Is(ctx.Err(), context.Canceled):
		return ExitInterrupted
	case errors.As(err, &usageErr):
		return ExitUsageError
	default:
		return ExitGeneralError
	}
}

// logFailure logs the error a command failed with. An Azure response error
// carries its status and error code, which its message buries.
func logFailure(log *slog.Logger, err error) {
	attrs := []any{"error", err, "error_type", fmt.Sprintf("%T", err)}

	var respErr *azcore.ResponseError
	if errors.As(err, &respErr) {
		attrs = append(attrs, "status_code", respErr.StatusCode, "error_code", respErr.ErrorCode)
	}

	log.Debug("command failed", attrs...)
}

func newRoot(application *app.App) *cobra.Command {
	root := &cobra.Command{
		Use:   "azskufinder",
		Short: "Find available Azure SKUs that match your requirements",
		Long: `azskufinder helps you find Azure SKUs that you can actually deploy.

Rather than simply listing every SKU in a region, it takes your requirements
(such as vCPUs, memory, location and category) and narrows the results down using
what is actually available to you, including:

- the SKUs offered in the chosen location
- the quota remaining on your subscription
- capacity constraints and restrictions imposed by Azure

Only SKUs that are available and meet your requirements are returned, with an
estimate of cost based on the retail pricing API.

azskufinder signs in the way the Azure SDKs do, trying in turn: a service
principal in AZURE_TENANT_ID, AZURE_CLIENT_ID and AZURE_CLIENT_SECRET, workload
identity, managed identity, then an Azure CLI ('az login'), Azure Developer
CLI or Azure PowerShell sign-in. It never takes a credential as a flag. The identity needs the
Reader role on the subscription.

Settings come from, in order of precedence:

- flags
- environment variables
- the YAML config file
- default values

Every flag has an environment variable named after it, prefixed with
AZSKUFINDER_, upper-cased and with hyphens replaced by underscores (for example
AZSKUFINDER_SUBSCRIPTION_ID). The config file is ~/.azskufinder/config.yaml
unless --config names one, and uses the flag names as keys:

  subscription-id: 00000000-0000-0000-0000-000000000000
  location: uksouth
  currency: GBP

Results are written to standard output. The progress spinner, warnings and debug
logs go to standard error, so the output can be piped, for example
'azskufinder vm -o json | jq'.

Colour is on in a terminal, and off when the output is piped or redirected,
when NO_COLOR is set, or with --no-colour.

Exit codes: 0 success, 1 runtime failure, 2 usage error, 130 interrupted.`,
		Version:       application.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		// The root has subcommands but does nothing itself. With Args set, an
		// unknown subcommand is reported by usageArgs as a usage error, and
		// RunE shows the help, as cobra would, when no subcommand is given.
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			// Debug and colour settings can come from the environment or the
			// config file, so they are read once viper is set up.
			configFile, err := setupViper(application.Viper, cmd)
			if err != nil {
				return err
			}

			debug, err := readBool(application.Viper, "debug")
			if err != nil {
				return err
			}

			if debug {
				application.LogLevel.Set(slog.LevelDebug)
			}

			log := application.Logger.With("component", "cli")
			log.Debug("starting", "version", application.Version, "go_version", runtime.Version(),
				"os", runtime.GOOS, "arch", runtime.GOARCH, "command", cmd.CommandPath())
			log.Debug("config file", "path", configFile, "found", configFile != "")

			noColour, err := readBool(application.Viper, "no-colour")
			if err != nil {
				return err
			}

			setStyles(application, cmd, noColour, log)

			return nil
		},
	}

	root.SetFlagErrorFunc(flagUsageError)

	f := root.PersistentFlags()

	f.Bool("debug", false, usage("debug", "enable debug logging"))
	f.Bool("no-colour", false, usage("no-colour", "do not colour or style the output (it is also off when NO_COLOR is set or the output is not a terminal)"))
	f.StringP("subscription-id", "s", "", usage("subscription-id", "Azure subscription ID (required)"))
	f.StringP("location", "l", "", usage("location", "Azure location, for example uksouth (required)"))
	f.StringP("output", "o", defaultOutput, usage("output", "output format: "+strings.Join(output.Formats(), ", ")))
	f.String("currency", defaultCurrency, usage("currency", "currency for prices: "+strings.Join(currencies, ", ")))
	f.IntP("limit", "n", defaultLimit, usage("limit", "maximum number of SKUs to show, 0 for all"))
	f.Duration("timeout", defaultTimeout, usage("timeout", "give up after this long, 0 for no limit"))
	f.String("config", "", usage("config", "path to a YAML config file (default: ~/.azskufinder/config.yaml)"))
	registerFixed(root, "output", output.Formats()...)
	registerFixed(root, "currency", currencies...)

	root.AddCommand(newVMCmd(application))

	return root
}

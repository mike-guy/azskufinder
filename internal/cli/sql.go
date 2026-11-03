package cli

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/mike-guy/azskufinder/internal/app"
	"github.com/mike-guy/azskufinder/internal/azure"
	"github.com/mike-guy/azskufinder/internal/retail"
	"github.com/mike-guy/azskufinder/internal/sql"
)

func newSQLCmd(application *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sql",
		Short: "Find deployable Azure SQL sizes",
		Long: `Find Azure SQL sizes that meet your requirements and can be deployed.

  * 'sql db vcore' lists vCore-based single-database SKUs
  * 'sql db dtu' lists DTU-based single-database SKUs

Results are narrowed using your subscription (--subscription-id) in the
location (--location): SKUs Azure does not offer to the subscription there
are left out, and so are SKUs the remaining vCore quota cannot fit.

When Azure restricts provisioning in the location for the subscription, every
SKU there is left out as restricted.`,
		Example: `  azskufinder sql db vcore --location uksouth --vcores 4 --tier general-purpose
  azskufinder sql db dtu --location uksouth --dtus 100`,
		// As on the root: an unknown subcommand is a usage error, and no
		// subcommand shows the help.
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newSQLDBCmd(application))

	return cmd
}

// addSQLSharedFlags adds the flags sql db vcore and sql db dtu have in common.
// exact names the numbers --exact-match applies to, for its help text.
func addSQLSharedFlags(cmd *cobra.Command, sortKeys []string, exact string) {
	f := cmd.Flags()
	f.Float64("storage", 0, usage("storage", "data size in GiB the database must be able to hold"))
	f.BoolP("exact-match", "e", false, usage("exact-match", "make "+exact+" exact instead of a minimum"))
	f.Bool("zone-redundant", false, usage("zone-redundant", "keep SKUs that can be zone redundant"))
	f.String("backup-redundancy", "any", usage("backup-redundancy", "backup storage redundancy: "+strings.Join(sql.BackupRedundancyNames(), ", ")))
	f.String("sort-by", string(sql.DefaultSortKey), usage("sort-by", "column to sort by, ascending unless followed by :desc, such as name:desc; one of "+strings.Join(sortKeys, ", ")))

	registerFixed(cmd, "backup-redundancy", sql.BackupRedundancyNames()...)
	registerFixed(cmd, "sort-by", sortCompletions(sortKeys)...)
}

// sqlShared are the settings sql db vcore and sql db dtu read the same way.
type sqlShared struct {
	StorageGiB       *float64
	ExactMatch       bool
	ZoneRedundant    bool
	BackupRedundancy sql.BackupRedundancy
}

func readSQLShared(v *viper.Viper) (sqlShared, error) {
	var (
		s   sqlShared
		err error
	)

	if s.StorageGiB, err = readOptionalFloat(v, "storage"); err != nil {
		return s, err
	}

	if s.ExactMatch, err = readBool(v, "exact-match"); err != nil {
		return s, err
	}

	if s.ZoneRedundant, err = readBool(v, "zone-redundant"); err != nil {
		return s, err
	}

	if s.BackupRedundancy, err = sql.ParseBackupRedundancy(v.GetString("backup-redundancy")); err != nil {
		return s, usageErrorf("--backup-redundancy: %w", err)
	}

	return s, nil
}

// sharedKeyHint is the hint for a value that is valid for another command: the
// environment and config file set it for every command.
func sharedKeyHint(flag string) string {
	return "Values differ between commands, and " + envName(flag) + " or " + flag + " in the config file applies to every command."
}

// newSQLService builds the credential, clients and service for a sql run, as
// runVM does for vm.
func newSQLService(application *app.App, opts commonOptions) (*sql.Service, error) {
	// Before the credential, which logs which sign-in methods it could set up.
	azure.EnableSDKLogging(application.Logger)

	// Clients depend on parsed configuration, so they are built here rather
	// than stored on App.
	cred, err := azure.NewCredential()
	if err != nil {
		return nil, err
	}

	clientOpts := azure.ClientOptions(application.Version)

	src, err := sql.NewARMSource(opts.SubscriptionID, cred, clientOpts, application.Logger)
	if err != nil {
		return nil, err
	}

	prices := retail.NewClient(opts.Currency, clientOpts, application.Logger)

	return sql.NewService(src, prices, application.Logger), nil
}

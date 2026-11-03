package cli

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/mike-guy/azskufinder/internal/app"
	"github.com/mike-guy/azskufinder/internal/sql"
)

func newSQLDBDTUCmd(application *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dtu",
		Short: "Find deployable DTU-based SQL Database SKUs",
		Long: `Find DTU-based Azure SQL Database SKUs, such as S3 or P2, that meet
your requirements and can be deployed.

SKUs are filtered by service tier (--tier), DTUs (--dtus), the data size
the database must hold in GiB (--storage), zone redundancy (--zone-redundant)
and backup storage redundancy (--backup-redundancy). Requirements you leave out
are not applied. By default --dtus is a minimum; with --exact-match it must
match exactly. --storage is always a minimum: it keeps SKUs whose largest
data size is at least that big.

--tier is one or more of basic, standard and premium, separated by commas.

The results are then narrowed using your subscription in the location:

  * SKUs Azure has disabled or does not offer to the subscription
  * the regional vCore quota for SQL Database, which a DTU-based SKU uses at
    one vCore for every 100 DTUs, rounded up

The logical server quota is shown but not used to filter, as a database can go
on an existing server.

Each SKU is priced pay-as-you-go. DTU prices include the SQL licence, and
Azure Hybrid Benefit does not apply to them. Storage beyond what the SKU
includes, and backup, are not included.

--sort-by orders the SKUs by one column of the table, named in lower
case:

  * name
  * tier
  * dtus
  * maxstorage
  * freequota
  * hourly
  * monthly

It is ascending unless you add :desc, as in --sort-by dtus:desc, and the
default is monthly. SKUs with no value for the column come last either
way. The order is applied before --limit.`,
		Example: `  # At least 100 DTUs, in UK South
  azskufinder sql db dtu --subscription-id 00000000-0000-0000-0000-000000000000 \
    --location uksouth --dtus 100

  # Premium that can be zone redundant, as JSON
  azskufinder sql db dtu -l uksouth --tier premium --zone-redundant -o json`,
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSQLDBDTU(cmd, application)
		},
	}

	f := cmd.Flags()
	f.StringP("tier", "t", "", usage("tier", "service tier, comma separated: "+strings.Join(sql.DTUTierNames(), ", ")))
	f.Int("dtus", 0, usage("dtus", "number of DTUs: a minimum, or exact with --exact-match"))
	addSQLSharedFlags(cmd, sql.DTUSortKeyNames(), "--dtus")

	registerFixed(cmd, "tier", sql.DTUTierNames()...)

	return cmd
}

// sqlDTUOptions are the validated settings of the sql db dtu command.
type sqlDTUOptions struct {
	commonOptions
	Filter sql.DTUFilter
}

// readSQLDTUOptions reads and validates the settings of the sql db dtu
// command. Everything a person can get wrong is reported here, as a usage
// error, before any request is made.
func readSQLDTUOptions(v *viper.Viper) (sqlDTUOptions, error) {
	var (
		o   sqlDTUOptions
		err error
	)

	if o.commonOptions, err = readCommon(v); err != nil {
		return o, err
	}

	f := &o.Filter
	f.Limit = o.Limit

	shared, err := readSQLShared(v)
	if err != nil {
		return o, err
	}

	f.StorageGiB, f.ExactMatch, f.ZoneRedundant, f.BackupRedundancy = shared.StorageGiB, shared.ExactMatch, shared.ZoneRedundant, shared.BackupRedundancy

	if f.Sort, err = sql.ParseDTUSort(v.GetString("sort-by")); err != nil {
		return o, withHint(usageErrorf("--sort-by: %w", err), sharedKeyHint("sort-by"))
	}

	if f.Tiers, err = sql.ParseDTUTiers(v.GetString("tier")); err != nil {
		return o, withHint(usageErrorf("--tier: %w", err), sharedKeyHint("tier"))
	}

	if f.DTUs, err = readOptionalInt(v, "dtus"); err != nil {
		return o, err
	}

	return o, nil
}

func runSQLDBDTU(cmd *cobra.Command, application *app.App) error {
	opts, err := readSQLDTUOptions(application.Viper)
	if err != nil {
		return err
	}

	application.Logger.Debug("searching for DTU-based SQL Database SKUs",
		"component", "cli",
		"subscription_id", opts.SubscriptionID,
		"location", opts.Location,
		"currency", opts.Currency,
		"output", opts.Output,
		"timeout", opts.Timeout,
		"filter", opts.Filter,
	)

	ctx, cancel := withTimeout(cmd.Context(), opts.Timeout)
	defer cancel()

	svc, err := newSQLService(application, opts.commonOptions)
	if err != nil {
		return err
	}

	spinner := startSpinner(cmd.ErrOrStderr(), application, "Searching for DTU-based SQL Database SKUs in "+opts.Location+"…")
	defer spinner.Stop() // also clears the line on a panic

	res, err := svc.FindDTU(ctx, opts.Location, opts.Filter)

	spinner.Stop() // before any output, including the error Execute prints
	if err != nil {
		return err
	}

	if err := writeSQLDTUResult(cmd.OutOrStdout(), application.Stdout, opts, res); err != nil {
		return err
	}

	for _, w := range res.Warnings {
		printWarning(cmd.ErrOrStderr(), application.Stderr, w.Message)
	}

	return nil
}

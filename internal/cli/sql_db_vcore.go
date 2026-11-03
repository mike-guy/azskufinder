package cli

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/mike-guy/azskufinder/internal/app"
	"github.com/mike-guy/azskufinder/internal/sql"
)

func newSQLDBVCoreCmd(application *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vcore",
		Short: "Find deployable vCore-based SQL Database SKUs",
		Long: `Find vCore-based Azure SQL Database SKUs, such as GP_Gen5_4 or GP_S_Gen5_2,
that meet your requirements and can be deployed.

SKUs are filtered by service tier (--tier), compute model (--compute),
hardware (--hardware), vCores (--vcores), memory in GiB (--memory), the data size
the database must hold in GiB (--storage), zone redundancy (--zone-redundant)
and backup storage redundancy (--backup-redundancy). Requirements you leave out
are not applied. By default --vcores and --memory are minimums; with
--exact-match they must match exactly. --storage is always a minimum: it keeps
SKUs whose largest data size is at least that big.

--tier is one or more of general-purpose, business-critical and hyperscale,
separated by commas. --hardware is one or more of standard-series,
premium-series, premium-series-memory-optimised and dc-series; premium-series
hardware is offered for Hyperscale only. For serverless, --vcores is the most
vCores the database can scale to.

Azure publishes no memory figure for SQL Database, so memory comes from the
documented amount per vCore for the hardware. Serverless memory scales with the
workload, so it is shown as auto, and --memory cannot be used with --compute
serverless.

The results are then narrowed using your subscription in the location:

  * SKUs Azure has disabled or does not offer to the subscription
  * the regional vCore quota for SQL Database, which serverless uses at its
    maximum vCores

The logical server quota is shown but not used to filter, as a database can go
on an existing server.

Each SKU is priced pay-as-you-go: compute, plus the SQL licence unless
--licence ahb says Azure Hybrid Benefit covers it, plus the zone redundancy
surcharge with --zone-redundant. Hyperscale has no licence fee, and the
serverless price already includes it. Serverless is billed per second of use, so
its price is for running at its maximum vCores all month. Storage and backup
are not included.

--sort-by orders the SKUs by one column of the table, named in lower
case:

  * name
  * tier
  * compute
  * hardware
  * vcores
  * memory
  * maxstorage
  * freequota
  * hourly
  * monthly

It is ascending unless you add :desc, as in --sort-by vcores:desc, and the
default is monthly. SKUs with no value for the column come last either
way. The order is applied before --limit.`,
		Example: `  # General Purpose, 4 vCores or more, in UK South
  azskufinder sql db vcore --subscription-id 00000000-0000-0000-0000-000000000000 \
    --location uksouth --vcores 4 --tier general-purpose

  # Serverless, up to 8 vCores, zone redundant, priced with Azure Hybrid Benefit
  azskufinder sql db vcore -l uksouth --compute serverless --vcores 8 --exact-match \
    --zone-redundant --licence ahb`,
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSQLDBVCore(cmd, application)
		},
	}

	f := cmd.Flags()
	f.StringP("tier", "t", "", usage("tier", "service tier, comma separated: "+strings.Join(sql.VCoreTierNames(), ", ")))
	f.String("compute", "any", usage("compute", "compute model: "+strings.Join(sql.ComputeNames(), ", ")))
	f.String("hardware", "", usage("hardware", "hardware, comma separated: "+strings.Join(sql.HardwareNames(), ", ")))
	f.IntP("vcores", "c", 0, usage("vcores", "number of vCores (serverless: maximum vCores): a minimum, or exact with --exact-match"))
	f.Float64P("memory", "m", 0, usage("memory", "memory in GiB: a minimum, or exact with --exact-match"))
	f.String("licence", defaultSQLLicence, usage("licence", "SQL licence to price: included, or ahb when Azure Hybrid Benefit covers it"))
	addSQLSharedFlags(cmd, sql.VCoreSortKeyNames(), "--vcores and --memory")

	registerFixed(cmd, "tier", sql.VCoreTierNames()...)
	registerFixed(cmd, "compute", sql.ComputeNames()...)
	registerFixed(cmd, "hardware", sql.HardwareNames()...)
	registerFixed(cmd, "licence", sql.LicenceNames()...)

	return cmd
}

// sqlVCoreOptions are the validated settings of the sql db vcore command.
type sqlVCoreOptions struct {
	commonOptions
	Filter sql.VCoreFilter
}

// readSQLVCoreOptions reads and validates the settings of the sql db vcore
// command. Everything a person can get wrong is reported here, as a usage
// error, before any request is made.
func readSQLVCoreOptions(v *viper.Viper) (sqlVCoreOptions, error) {
	var (
		o   sqlVCoreOptions
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

	if f.Sort, err = sql.ParseVCoreSort(v.GetString("sort-by")); err != nil {
		return o, withHint(usageErrorf("--sort-by: %w", err), sharedKeyHint("sort-by"))
	}

	if f.Tiers, err = sql.ParseVCoreTiers(v.GetString("tier")); err != nil {
		return o, withHint(usageErrorf("--tier: %w", err), sharedKeyHint("tier"))
	}

	if f.Compute, err = sql.ParseCompute(v.GetString("compute")); err != nil {
		return o, usageErrorf("--compute: %w", err)
	}

	if f.Hardware, err = sql.ParseHardware(v.GetString("hardware")); err != nil {
		return o, usageErrorf("--hardware: %w", err)
	}

	if f.VCores, err = readOptionalInt(v, "vcores"); err != nil {
		return o, err
	}

	if f.MemoryGiB, err = readOptionalFloat(v, "memory"); err != nil {
		return o, err
	}

	if f.Licence, err = sql.ParseLicence(v.GetString("licence")); err != nil {
		return o, usageErrorf("--licence: %w", err)
	}

	// Serverless memory is not published, so nothing would match.
	if f.Compute == sql.ComputeServerless && f.MemoryGiB != nil {
		return o, usageErrorf("--memory cannot be used with --compute serverless, whose memory scales with the workload")
	}

	return o, nil
}

func runSQLDBVCore(cmd *cobra.Command, application *app.App) error {
	opts, err := readSQLVCoreOptions(application.Viper)
	if err != nil {
		return err
	}

	application.Logger.Debug("searching for vCore-based SQL Database SKUs",
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

	spinner := startSpinner(cmd.ErrOrStderr(), application, "Searching for vCore-based SQL Database SKUs in "+opts.Location+"…")
	defer spinner.Stop() // also clears the line on a panic

	res, err := svc.FindVCore(ctx, opts.Location, opts.Filter)

	spinner.Stop() // before any output, including the error Execute prints
	if err != nil {
		return err
	}

	if err := writeSQLVCoreResult(cmd.OutOrStdout(), application.Stdout, opts, res); err != nil {
		return err
	}

	for _, w := range res.Warnings {
		printWarning(cmd.ErrOrStderr(), application.Stderr, w.Message)
	}

	return nil
}

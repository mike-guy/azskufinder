package cli

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/mike-guy/azskufinder/internal/app"
	"github.com/mike-guy/azskufinder/internal/azure"
	"github.com/mike-guy/azskufinder/internal/output"
	"github.com/mike-guy/azskufinder/internal/retail"
	"github.com/mike-guy/azskufinder/internal/vm"
)

func newVMCmd(application *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vm",
		Short: "Find deployable virtual machine SKUs",
		Long: `Find Azure virtual machine SKUs that meet your requirements and can be deployed.

Candidate SKUs are filtered by the number of vCPUs (--cpus), the amount of
memory in GiB (--memory), the number of GPUs (--gpus), the number of network
interfaces the SKU can take (--nics), the CPU architecture (--arch), the category
of VM (--category) and the disk controller (--disk-controller). Requirements you
leave out are not applied. By default the numbers are minimums; with
--exact-match they must match exactly. For SKUs with constrained cores, --cpus is
the number of vCPUs the guest sees; quota is still counted against the larger
number Azure bills.

The results are then narrowed further using information from your subscription
(--subscription-id), so that SKUs you cannot use are not suggested:

  * SKUs that are restricted or unavailable in the location, and the zones
    that are restricted
  * vCPU quota remaining for the SKU's family and for the region overall

A subscription ID and a location are required, as quota and restrictions are
specific to them.

--category is one of general-purpose, burstable, compute, memory, storage, gpu and
hpc, or several separated by commas. Azure does not publish a category for each
SKU, so the category is worked out from the SKU's name (the D in Standard_D4s_v5 is
general purpose).

--disk-controller is any, scsi or nvme, and keeps the SKUs that support that disk
controller. It matters because of the image: many newer sizes, such as most v6
SKUs, support only NVMe, so an image without NVMe drivers will not boot on them,
and an image that needs NVMe cannot be used on a size that supports only SCSI.
SKUs that report no disk controller support SCSI only, as Azure documents.

Each SKU is shown with an estimate of its pay-as-you-go price per hour and per
month (--currency and --os choose which). With --include-spot, the Spot price
and the eviction rate (the chance per hour over the last 28 days that Azure takes
the VM back) are added. They need only the Reader role. Spot VMs use one regional
Spot vCPU quota instead of the family and regional quotas, so --include-spot also
keeps Spot-capable SKUs that lack on-demand quota but fit the Spot quota, marked
"Spot only".

--sort-by orders the SKUs by one column of the table, named in lower case:

  * name
  * vcpus
  * memory
  * nics
  * gpus
  * arch
  * category
  * freequota
  * hourly
  * monthly
  * spot
  * eviction

It is ascending unless you add :desc, as in --sort-by memory:desc, and the default
is monthly. SKUs with no value for the column come last either way. The order is
applied before --limit, so --limit 5 --sort-by memory:desc keeps the five with the
most memory. Sorting by spot or eviction needs --include-spot.

Everything is read live from Azure, so quota is always current.`,
		Example: `  # Four vCPUs and 16 GiB of memory or more, in UK South
  azskufinder vm --subscription-id 00000000-0000-0000-0000-000000000000 \
    --location uksouth --cpus 4 --memory 16

  # Exactly 8 vCPUs, memory optimised, Arm only, as JSON
  azskufinder vm --subscription-id 00000000-0000-0000-0000-000000000000 \
    --location swedencentral --cpus 8 --exact-match --category memory --arch arm64 -o json

  # Settings from the environment, with Spot prices and eviction rates
  export AZSKUFINDER_SUBSCRIPTION_ID=00000000-0000-0000-0000-000000000000
  AZSKUFINDER_LOCATION=swedencentral azskufinder vm --gpus 1 --include-spot`,
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runVM(cmd, application)
		},
	}

	f := cmd.Flags()
	f.IntP("cpus", "c", 0, usage("cpus", "number of vCPUs: a minimum, or exact with --exact-match"))
	f.Float64P("memory", "m", 0, usage("memory", "memory in GiB: a minimum, or exact with --exact-match"))
	f.IntP("gpus", "g", 0, usage("gpus", "number of GPUs: a minimum, or exact with --exact-match (--gpus 0 --exact-match means none)"))
	f.Int("nics", 0, usage("nics", "number of network interfaces the VM must take: a minimum, or exact with --exact-match"))
	f.BoolP("exact-match", "e", false, usage("exact-match", "make --cpus, --memory, --gpus and --nics exact values instead of minimums"))
	f.StringP("arch", "a", "any", usage("arch", "CPU architecture: "+strings.Join(vm.ArchNames(), ", ")))
	f.StringP("category", "C", "", usage("category", "kind of VM, comma separated: "+strings.Join(vm.CategoryNames(), ", ")))
	f.String("disk-controller", "any", usage("disk-controller", "disk controller the VM must support, as the image may need: "+strings.Join(vm.DiskControllerNames(), ", ")))
	f.String("os", defaultOS, usage("os", "operating system to price: linux or windows"))
	f.BoolP("include-spot", "S", false, usage("include-spot", "also show Spot prices and eviction rates, and keep SKUs that only Spot quota allows"))
	f.String("sort-by", string(vm.DefaultSortKey), usage("sort-by", "column to sort by, ascending unless followed by :desc, such as memory:desc; one of "+strings.Join(vm.SortKeyNames(), ", ")))

	registerFixed(cmd, "os", string(vm.OSLinux), string(vm.OSWindows))
	registerFixed(cmd, "sort-by", sortCompletions(vm.SortKeyNames())...)
	registerFixed(cmd, "arch", vm.ArchNames()...)
	registerFixed(cmd, "category", vm.CategoryNames()...)
	registerFixed(cmd, "disk-controller", vm.DiskControllerNames()...)

	return cmd
}

// vmOptions are the validated settings of the vm command.
type vmOptions struct {
	commonOptions
	Filter vm.Filter
}

// readVMOptions reads and validates the settings of the vm command. Everything
// a person can get wrong is reported here, as a usage error, before any
// request is made.
func readVMOptions(v *viper.Viper) (vmOptions, error) {
	var (
		o   vmOptions
		err error
	)

	if o.commonOptions, err = readCommon(v); err != nil {
		return o, err
	}

	o.Filter.Limit = o.Limit

	if o.Filter.OS, err = vm.ParseOS(v.GetString("os")); err != nil {
		return o, usageErrorf("--os: %w", err)
	}

	if o.Filter.IncludeSpot, err = readBool(v, "include-spot"); err != nil {
		return o, err
	}

	if o.Filter.Sort, err = vm.ParseSort(v.GetString("sort-by")); err != nil {
		return o, usageErrorf("--sort-by: %w", err)
	}

	if o.Filter.Sort.NeedsSpot() && !o.Filter.IncludeSpot {
		return o, withHint(usageErrorf("--sort-by %s needs Spot data", o.Filter.Sort.Key), "Add --include-spot.")
	}

	if o.Filter.CPUs, err = readOptionalInt(v, "cpus"); err != nil {
		return o, err
	}

	if o.Filter.MemoryGiB, err = readOptionalFloat(v, "memory"); err != nil {
		return o, err
	}

	if o.Filter.GPUs, err = readOptionalInt(v, "gpus"); err != nil {
		return o, err
	}

	if o.Filter.NICs, err = readOptionalInt(v, "nics"); err != nil {
		return o, err
	}

	if o.Filter.ExactMatch, err = readBool(v, "exact-match"); err != nil {
		return o, err
	}

	if o.Filter.Arch, err = vm.ParseArch(v.GetString("arch")); err != nil {
		return o, usageErrorf("--arch: %w", err)
	}

	if o.Filter.Categories, err = vm.ParseCategories(v.GetString("category")); err != nil {
		return o, usageErrorf("--category: %w", err)
	}

	if o.Filter.DiskController, err = vm.ParseDiskController(v.GetString("disk-controller")); err != nil {
		return o, usageErrorf("--disk-controller: %w", err)
	}

	return o, nil
}

func runVM(cmd *cobra.Command, application *app.App) error {
	opts, err := readVMOptions(application.Viper)
	if err != nil {
		return err
	}

	application.Logger.Debug("searching for VM SKUs",
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

	// Before the credential, which logs which sign-in methods it could set up.
	azure.EnableSDKLogging(application.Logger)

	// Clients depend on parsed configuration, so they are built here rather
	// than stored on App.
	cred, err := azure.NewCredential()
	if err != nil {
		return err
	}

	clientOpts := azure.ClientOptions(application.Version)

	src, err := vm.NewARMSource(opts.SubscriptionID, cred, clientOpts, application.Logger)
	if err != nil {
		return err
	}

	prices := retail.NewClient(opts.Currency, clientOpts, application.Logger)
	svc := vm.NewService(src, prices, application.Logger)

	stderr := cmd.ErrOrStderr()
	debug := application.LogLevel.Level() <= slog.LevelDebug

	// spinner stays nil when it should not be shown; Stop on nil does nothing.
	var spinner *output.Spinner
	if spinnerOn(isTerminal(stderr), debug, os.Getenv) {
		spinner = output.StartSpinner(stderr, application.Stderr, "Searching for VM SKUs in "+opts.Location+"…")
	}
	defer spinner.Stop() // also clears the line on a panic

	res, err := svc.Find(ctx, opts.Location, opts.Filter)

	spinner.Stop() // before any output, including the error Execute prints
	if err != nil {
		return err
	}

	if err := writeVMResult(cmd.OutOrStdout(), application.Stdout, opts, res); err != nil {
		return err
	}

	printWarnings(stderr, application.Stderr, res.Warnings)

	return nil
}

// printWarnings writes warnings to w, which is stderr, so that the results on
// stdout stay clean to pipe. st is w's Styles.
func printWarnings(w io.Writer, st output.Styles, warnings []vm.Warning) {
	for _, warning := range warnings {
		_, _ = fmt.Fprintln(w, st.Warn("warning: "+output.Sanitise(warning.Message)))
	}
}

package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/mike-guy/azskufinder/internal/output"
	"github.com/mike-guy/azskufinder/internal/vm"
)

// tableOpts are the options for the default table output, with filter.
func tableOpts(f vm.Filter) vmOptions {
	f.OS = vm.OSLinux

	return vmOptions{
		commonOptions: commonOptions{
			SubscriptionID: testSubscriptionID,
			Location:       "uksouth",
			Output:         output.FormatTable,
			Currency:       "GBP",
			Limit:          f.Limit,
		},
		Filter: f,
	}
}

const linuxPricingNote = "Prices: pay-as-you-go estimates in GBP for Linux, at 730 hours a month; " +
	"reservations, savings plans and discounts are not applied."

func TestDescribeFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		f    vm.Filter
		want []criterion
	}{
		{name: "nothing set", f: vm.Filter{}, want: []criterion{}},
		{name: "minimum", f: vm.Filter{CPUs: new(4)}, want: []criterion{{"vCPUs", ">= 4"}}},
		{name: "minimum of zero is left out", f: vm.Filter{CPUs: new(0), GPUs: new(0), MemoryGiB: new(0.0)}, want: []criterion{}},
		{name: "exact zero", f: vm.Filter{CPUs: new(0), ExactMatch: true}, want: []criterion{{"vCPUs", "= 0"}}},
		{name: "exact no GPUs", f: vm.Filter{GPUs: new(0), ExactMatch: true}, want: []criterion{{"GPUs", "= 0"}}},
		{name: "whole memory", f: vm.Filter{MemoryGiB: new(16.0)}, want: []criterion{{"memory", ">= 16 GiB"}}},
		{name: "fractional memory", f: vm.Filter{MemoryGiB: new(1.5)}, want: []criterion{{"memory", ">= 1.5 GiB"}}},
		{name: "arch", f: vm.Filter{Arch: vm.ArchARM64}, want: []criterion{{"architecture", "arm64"}}},
		{
			name: "categories",
			f:    vm.Filter{Categories: []vm.Category{vm.CategoryMemory, vm.CategoryGPU}},
			want: []criterion{{"category", "memory or gpu"}},
		},
		{name: "disk controller", f: vm.Filter{DiskController: vm.DiskControllerNVMe}, want: []criterion{{"disk controller", "nvme"}}},
		{
			name: "everything, in order",
			f: vm.Filter{
				DiskController: vm.DiskControllerSCSI, Categories: []vm.Category{vm.CategoryCompute}, Arch: vm.ArchX64,
				GPUs: new(1), MemoryGiB: new(8.0), CPUs: new(2), ExactMatch: true,
			},
			want: []criterion{
				{"vCPUs", "= 2"},
				{"memory", "= 8 GiB"},
				{"GPUs", "= 1"},
				{"architecture", "x64"},
				{"category", "compute"},
				{"disk controller", "scsi"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := describeFilter(tc.f)
			if got == nil {
				t.Fatalf("describeFilter() = nil, want a non-nil slice so JSON shows []")
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("describeFilter() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRequirementLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		f    vm.Filter
		want []string
	}{
		{name: "nothing set", f: vm.Filter{}, want: nil},
		{
			name: "every kind",
			f: vm.Filter{
				CPUs: new(2), MemoryGiB: new(4.0), GPUs: new(1), Arch: vm.ArchX64,
				Categories: []vm.Category{vm.CategoryGeneral}, DiskController: vm.DiskControllerNVMe,
			},
			want: []string{
				" - vCPUs >= 2",
				" - Memory >= 4 GiB",
				" - GPUs >= 1",
				" - Architecture x64",
				" - Category general-purpose",
				" - Disk Controller nvme",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tc.want, requirementLines(tc.f)); diff != "" {
				t.Errorf("requirementLines() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestEmptyExplanation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		summary vm.Summary
		want    string
	}{
		{
			name: "nothing returned",
			want: "No VM SKUs were returned for uksouth. Check the location name.",
		},
		{
			name:    "nothing matched",
			summary: vm.Summary{Considered: 900},
			want:    "No VM SKUs in uksouth meet your requirements (900 checked).",
		},
		{
			name:    "all left out",
			summary: vm.Summary{Considered: 900, Matched: 5, Restricted: 2, NoQuota: 3},
			want:    "None of the matching VM SKUs in uksouth can be deployed: 2 restricted for this subscription, 3 without enough quota.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := emptyExplanation(vm.Result{Location: "uksouth", Summary: tc.summary}); got != tc.want {
				t.Errorf("emptyExplanation(%+v) = %q, want %q", tc.summary, got, tc.want)
			}
		})
	}
}

func TestLeftOut(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		summary vm.Summary
		want    []string
	}{
		{name: "none", summary: vm.Summary{Matched: 4, Eligible: 4}, want: nil},
		{name: "restricted", summary: vm.Summary{Restricted: 1}, want: []string{"1 restricted for this subscription"}},
		{
			name:    "both",
			summary: vm.Summary{Restricted: 2, NoQuota: 3},
			want:    []string{"2 restricted for this subscription", "3 without enough quota"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tc.want, leftOut(tc.summary)); diff != "" {
				t.Errorf("leftOut(%+v) mismatch (-want +got):\n%s", tc.summary, diff)
			}
		})
	}
}

func TestVMHeadline(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		f       vm.Filter
		summary vm.Summary
		want    []string
	}{
		{
			name:    "one of each",
			summary: vm.Summary{Matched: 1, Eligible: 1, Shown: 1},
			want:    []string{"1 matching SKU in uksouth", "1 deployable SKU"},
		},
		{
			name:    "all shown, with requirements",
			f:       vm.Filter{CPUs: new(2), MemoryGiB: new(4.0), ExactMatch: true},
			summary: vm.Summary{Matched: 19, Eligible: 12, Shown: 12, Restricted: 7},
			want: []string{
				"19 matching SKUs in uksouth",
				"12 deployable SKUs with filters:",
				" - vCPUs = 2",
				" - Memory = 4 GiB",
				"7 restricted for this subscription",
			},
		},
		{
			name:    "limited, with Spot only and everything left out",
			summary: vm.Summary{Matched: 12, Restricted: 2, NoQuota: 3, Eligible: 7, SpotOnly: 1, Shown: 5},
			want: []string{
				"12 matching SKUs in uksouth",
				"Showing 5 of 7 deployable SKUs",
				"1 of them only as Spot (not enough on-demand quota)",
				"2 restricted for this subscription",
				"3 without enough quota",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := vmHeadline(output.Styles{}, tableOpts(tc.f), vm.Result{Location: "uksouth", Summary: tc.summary})
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("vmHeadline() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestVMFooter(t *testing.T) {
	t.Parallel()

	const evictionNote = "Spot VMs can be evicted. EVICTION is the hourly chance of that over the last 28 days."

	tests := []struct {
		name      string
		windows   bool
		spot      bool
		summary   vm.Summary
		spotQuota *vm.QuotaHeadroom
		want      []string
	}{
		{
			name:    "all shown",
			summary: vm.Summary{Eligible: 3, Shown: 3},
			want:    []string{linuxPricingNote},
		},
		{
			name:    "limited",
			summary: vm.Summary{Eligible: 30, Shown: 25},
			want:    []string{"Use --limit 0 to show all 30.", linuxPricingNote},
		},
		{
			name:    "windows",
			windows: true,
			want: []string{"Prices: pay-as-you-go estimates in GBP for Windows, at 730 hours a month; " +
				"reservations, savings plans and discounts are not applied."},
		},
		{
			name:      "spot with quota",
			spot:      true,
			spotQuota: &vm.QuotaHeadroom{Limit: 100, Used: 4, Free: 96},
			want:      []string{linuxPricingNote, "Spot quota: 96 of 100 vCPUs free in uksouth.", evictionNote},
		},
		{
			name: "spot without quota",
			spot: true,
			want: []string{linuxPricingNote, evictionNote},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			opts := tableOpts(vm.Filter{IncludeSpot: tc.spot})
			if tc.windows {
				opts.Filter.OS = vm.OSWindows
			}

			got := vmFooter(output.Styles{}, opts, vm.Result{Location: "uksouth", Summary: tc.summary, SpotQuota: tc.spotQuota})
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("vmFooter() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestMissingPriceNote(t *testing.T) {
	t.Parallel()

	priced := vm.VM{Name: "priced", Arch: vm.ArchX64, Pricing: vm.Pricing{OnDemandHourly: new(0.1)}}
	x64 := vm.VM{Name: "x64", Arch: vm.ArchX64}
	arm := vm.VM{Name: "arm", Arch: vm.ArchARM64}
	unavailable := []vm.Warning{{Code: vm.WarningPricingUnavailable}}

	tests := []struct {
		name     string
		windows  bool
		vms      []vm.VM
		warnings []vm.Warning
		want     string
	}{
		{name: "all priced", vms: []vm.VM{priced}, want: ""},
		{name: "one missing", vms: []vm.VM{priced, x64}, want: "1 SKU has no Linux retail price from Azure, so its price is shown as -."},
		{
			name:    "several missing, all Arm64, Windows",
			windows: true,
			vms:     []vm.VM{arm, priced, arm},
			want:    "2 SKUs (all Arm64) have no Windows retail price from Azure, so their prices are shown as -.",
		},
		{name: "one Arm64", vms: []vm.VM{arm}, want: "1 SKU (Arm64) has no Linux retail price from Azure, so its price is shown as -."},
		{name: "mixed architectures", vms: []vm.VM{arm, x64}, want: "2 SKUs have no Linux retail price from Azure, so their prices are shown as -."},
		{name: "prices unavailable, the warning says why", vms: []vm.VM{x64}, warnings: unavailable, want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			opts := tableOpts(vm.Filter{})
			if tc.windows {
				opts.Filter.OS = vm.OSWindows
			}

			if got := missingPriceNote(opts, vm.Result{VMs: tc.vms, Warnings: tc.warnings}); got != tc.want {
				t.Errorf("missingPriceNote() = %q, want %q", got, tc.want)
			}
		})
	}
}

// cellCase is one VM and the text a column shows for it.
type cellCase struct {
	name string
	v    vm.VM
	want string
}

func runCellCases(t *testing.T, fnName string, fn func(vm.VM) string, cases []cellCase) {
	t.Helper()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := fn(tc.v); got != tc.want {
				t.Errorf("%s(%s) = %q, want %q", fnName, tc.v.Name, got, tc.want)
			}
		})
	}
}

func TestVCPUText(t *testing.T) {
	t.Parallel()
	runCellCases(t, "vcpuText", vcpuText, []cellCase{
		{name: "same", v: vm.VM{VCPUs: 4, VCPUsAvailable: 4}, want: "4"},
		{name: "constrained", v: vm.VM{VCPUs: 8, VCPUsAvailable: 4}, want: "4 (8 billed)"},
	})
}

func TestGPUText(t *testing.T) {
	t.Parallel()
	runCellCases(t, "gpuText", gpuText, []cellCase{
		{name: "none", v: vm.VM{}, want: output.NoValue},
		{name: "two", v: vm.VM{GPUs: 2}, want: "2"},
	})
}

func TestDiskControllerText(t *testing.T) {
	t.Parallel()
	runCellCases(t, "diskControllerText", diskControllerText, []cellCase{
		{name: "none known", v: vm.VM{DiskControllers: []vm.DiskController{}}, want: output.NoValue},
		{name: "scsi", v: vm.VM{DiskControllers: []vm.DiskController{vm.DiskControllerSCSI}}, want: "SCSI"},
		{name: "nvme", v: vm.VM{DiskControllers: []vm.DiskController{vm.DiskControllerNVMe}}, want: "NVMe"},
		{
			name: "both",
			v:    vm.VM{DiskControllers: []vm.DiskController{vm.DiskControllerSCSI, vm.DiskControllerNVMe}},
			want: "SCSI, NVMe",
		},
	})
}

func TestQuotaText(t *testing.T) {
	t.Parallel()
	runCellCases(t, "quotaText", quotaText, []cellCase{
		{name: "spot only", v: vm.VM{SpotOnly: true, Quota: []vm.QuotaHeadroom{{Free: 4}}}, want: "Spot only"},
		{name: "tightest quota", v: vm.VM{Quota: []vm.QuotaHeadroom{{Free: 80}, {Free: 40}}}, want: "40"},
		{name: "no quota", v: vm.VM{}, want: output.NoValue},
	})
}

func TestEvictionText(t *testing.T) {
	t.Parallel()
	runCellCases(t, "evictionText", evictionText, []cellCase{
		{name: "not spot capable", v: vm.VM{}, want: "n/a"},
		{name: "no spot data", v: vm.VM{SpotCapable: true}, want: output.NoValue},
		{name: "no rate", v: vm.VM{SpotCapable: true, Spot: &vm.Spot{}}, want: output.NoValue},
		{name: "band", v: vm.VM{SpotCapable: true, Spot: &vm.Spot{EvictionRate: new("0-5")}}, want: "0-5%"},
	})
}

func headers(cols []output.Column[vm.VM]) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = c.Header
	}

	return out
}

func TestVMColumns(t *testing.T) {
	t.Parallel()

	base := []string{"NAME", "VCPUS", "MEMORY", "DISK", "GPUS", "ARCH", "CATEGORY", "ZONES", "FREEQUOTA", "HOURLY", "MONTHLY"}

	if diff := cmp.Diff(base, headers(vmColumns(output.Styles{}, "GBP", false))); diff != "" {
		t.Errorf("vmColumns(without Spot) headers mismatch (-want +got):\n%s", diff)
	}

	withSpot := headers(vmColumns(output.Styles{}, "GBP", true))
	if diff := cmp.Diff(append(base, "SPOT", "EVICTION"), withSpot); diff != "" {
		t.Errorf("vmColumns(with Spot) headers mismatch (-want +got):\n%s", diff)
	}
}

// TestVMColumns_sortKeys checks that each --sort-by key is the lower-cased
// header of a column, so what a person sees is what they type.
func TestVMColumns_sortKeys(t *testing.T) {
	t.Parallel()

	shown := map[string]bool{}
	for _, h := range headers(vmColumns(output.Styles{}, "GBP", true)) {
		shown[strings.ToLower(h)] = true
	}

	for _, key := range vm.SortKeyNames() {
		if !shown[key] {
			t.Errorf("sort key %q has no column headed %q", key, strings.ToUpper(key))
		}
	}
}

func TestWriteVMResult_golden(t *testing.T) {
	t.Parallel()

	requirements := vm.Filter{CPUs: new(2), MemoryGiB: new(4.0), Limit: 4}

	spotOpts := tableOpts(requirements)
	spotOpts.Filter.IncludeSpot = true
	spotOpts.Currency = "USD"

	jsonOpts := tableOpts(vm.Filter{CPUs: new(2), Categories: []vm.Category{vm.CategoryGeneral, vm.CategoryMemory}, Limit: 4})
	jsonOpts.Output = output.FormatJSON
	jsonOpts.Filter.IncludeSpot = true

	emptyJSON := tableOpts(vm.Filter{})
	emptyJSON.Output = output.FormatJSON

	withWarning := sampleResult()
	withWarning.Warnings = []vm.Warning{{Code: vm.WarningSpotUnavailable, Message: "eviction rates could not be fetched"}}

	noSpot := sampleResult()
	noSpot.SpotQuota = nil

	tests := []struct {
		name string
		st   output.Styles
		opts vmOptions
		res  vm.Result
	}{
		{name: "vm_table", opts: tableOpts(requirements), res: noSpot},
		{name: "vm_table_spot", opts: spotOpts, res: sampleResult()},
		{name: "vm_table_colour", st: output.NewStyles(true), opts: spotOpts, res: sampleResult()},
		{name: "vm_json", opts: jsonOpts, res: withWarning},
		{
			name: "vm_json_empty",
			opts: emptyJSON,
			res:  vm.Result{Location: "uksouth", Summary: vm.Summary{Considered: 900}, VMs: []vm.VM{}, Warnings: []vm.Warning{}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			if err := writeVMResult(&buf, tc.st, tc.opts, tc.res); err != nil {
				t.Fatalf("writeVMResult() error = %v", err)
			}

			golden(t, tc.name, buf.Bytes())
		})
	}
}

func TestWriteVMResult_empty(t *testing.T) {
	t.Parallel()

	res := vm.Result{Location: "uksouth", Summary: vm.Summary{Considered: 900}}

	var buf bytes.Buffer
	if err := writeVMResult(&buf, output.Styles{}, tableOpts(vm.Filter{CPUs: new(512)}), res); err != nil {
		t.Fatalf("writeVMResult() error = %v", err)
	}

	want := "\nNo VM SKUs in uksouth meet your requirements (900 checked).\nFilters:\n - vCPUs >= 512\n"
	if got := buf.String(); got != want {
		t.Errorf("writeVMResult() wrote %q, want %q", got, want)
	}
}

func TestWriteVMResult_writeError(t *testing.T) {
	t.Parallel()

	for _, format := range output.Formats() {
		t.Run(format, func(t *testing.T) {
			t.Parallel()

			opts := tableOpts(vm.Filter{})
			opts.Output = format

			if err := writeVMResult(errWriter{}, output.Styles{}, opts, sampleResult()); !errors.Is(err, errWrite) {
				t.Errorf("writeVMResult() error = %v, want one wrapping %v", err, errWrite)
			}
		})
	}
}

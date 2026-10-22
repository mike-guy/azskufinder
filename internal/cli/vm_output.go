package cli

import (
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/mike-guy/azskufinder/internal/output"
	"github.com/mike-guy/azskufinder/internal/vm"
)

// jsonAPIVersion identifies the shape of the JSON output, so that scripts can
// detect a change. Only add fields within a version; change it to remove or
// rename any.
const jsonAPIVersion = "azskufinder/v1"

// vmDocument is the JSON output. Embedding vm.Result flattens its fields
// (location, summary, spotQuota, results, warnings) into the document.
type vmDocument struct {
	APIVersion string      `json:"apiVersion"`
	Kind       string      `json:"kind"`
	Criteria   []criterion `json:"criteria"`
	Pricing    pricingInfo `json:"pricing"`
	vm.Result
}

// criterion is one requirement that was set, for the headline and JSON.
type criterion struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// pricingInfo describes the prices in the results.
type pricingInfo struct {
	Currency      string `json:"currency"`
	OS            string `json:"os"`
	HoursPerMonth int    `json:"hoursPerMonth"`
}

// writeVMResult writes res to w in the chosen format, styling the table with st.
func writeVMResult(w io.Writer, st output.Styles, opts vmOptions, res vm.Result) error {
	if opts.Output == output.FormatJSON {
		return output.JSON(w, vmDocument{
			APIVersion: jsonAPIVersion,
			Kind:       "vm",
			Criteria:   describeFilter(opts.Filter),
			Pricing:    pricingInfo{Currency: opts.Currency, OS: string(opts.Filter.OS), HoursPerMonth: vm.HoursPerMonth},
			Result:     res,
		})
	}

	return writeVMTable(w, st, opts, res)
}

// describeFilter lists the requirements that are set: {"vCPUs", ">= 4"},
// {"memory", "= 16 GiB"}, {"category", "memory or gpu"}. It is never nil, so
// the JSON shows an empty list rather than null.
func describeFilter(f vm.Filter) []criterion {
	compare := ">="
	if f.ExactMatch {
		compare = "="
	}

	// A minimum of 0 rules nothing out, so it is not listed.
	constrains := func(n float64) bool { return f.ExactMatch || n > 0 }

	out := []criterion{}

	if f.CPUs != nil && constrains(float64(*f.CPUs)) {
		out = append(out, criterion{Name: "vCPUs", Value: compare + " " + strconv.Itoa(*f.CPUs)})
	}

	if f.MemoryGiB != nil && constrains(*f.MemoryGiB) {
		out = append(out, criterion{Name: "memory", Value: compare + " " + gib(*f.MemoryGiB)})
	}

	if f.GPUs != nil && constrains(float64(*f.GPUs)) {
		out = append(out, criterion{Name: "GPUs", Value: compare + " " + strconv.Itoa(*f.GPUs)})
	}

	if f.NICs != nil && constrains(float64(*f.NICs)) {
		out = append(out, criterion{Name: "NICs", Value: compare + " " + strconv.Itoa(*f.NICs)})
	}

	if f.Arch != "" {
		out = append(out, criterion{Name: "architecture", Value: string(f.Arch)})
	}

	if len(f.Categories) > 0 {
		names := make([]string, len(f.Categories))
		for i, c := range f.Categories {
			names[i] = string(c)
		}

		out = append(out, criterion{Name: "category", Value: strings.Join(names, " or ")})
	}

	if f.DiskController != "" {
		out = append(out, criterion{Name: "disk controller", Value: string(f.DiskController)})
	}

	return out
}

// writeVMTable writes what was found, then the table, then what to bear in
// mind when reading it. The answer comes first so that it is not lost above a
// long table.
func writeVMTable(w io.Writer, st output.Styles, opts vmOptions, res vm.Result) error {
	var b strings.Builder

	// A blank line sets the results apart from the command that was typed.
	b.WriteString("\n")

	if len(res.VMs) == 0 {
		writeLines(&b, noResults(st, opts, res))
	} else {
		writeLines(&b, vmHeadline(st, opts, res))
		b.WriteString("\n")

		if err := output.Table(&b, st, vmColumns(st, opts.Currency, opts.Filter.IncludeSpot), res.VMs); err != nil {
			return err
		}

		b.WriteString("\n")
		writeLines(&b, vmFooter(st, opts, res))
	}

	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("writing results: %w", err)
	}

	return nil
}

func writeLines(b *strings.Builder, lines []string) {
	for _, line := range lines {
		b.WriteString(line + "\n")
	}
}

// noResults says why nothing can be deployed, and what was asked for.
func noResults(st output.Styles, opts vmOptions, res vm.Result) []string {
	lines := []string{st.Bold(emptyExplanation(res))}

	if requirements := requirementLines(opts.Filter); len(requirements) > 0 {
		lines = append(lines, "Filters:")
		lines = append(lines, requirements...)
	}

	return lines
}

// vmHeadline says what was found, one fact to a line, with the filters listed
// under the deployable count so that the line stays short however many are set:
//
//	19 matching SKUs in northeurope
//	12 deployable SKUs with filters:
//	 - vCPUs = 2
//	 - Memory = 4 GiB
//	7 restricted for this subscription
//
// The deployable line and its filters are green, and the lines about what was
// left out are yellow. A line whose count is 0 is left out.
func vmHeadline(st output.Styles, opts vmOptions, res vm.Result) []string {
	s := res.Summary

	deployable := fmt.Sprintf("%d deployable %s", s.Eligible, plural(s.Eligible, "SKU", "SKUs"))
	if s.Shown < s.Eligible {
		deployable = fmt.Sprintf("Showing %d of %d deployable SKUs", s.Shown, s.Eligible)
	}

	requirements := requirementLines(opts.Filter)
	if len(requirements) > 0 {
		deployable += " with filters:"
	}

	lines := []string{
		st.Bold(fmt.Sprintf("%d matching %s in %s", s.Matched, plural(s.Matched, "SKU", "SKUs"), res.Location)),
		st.Good(deployable),
	}

	// The filters belong to the deployable line, so they share its colour.
	for _, r := range requirements {
		lines = append(lines, st.Good(r))
	}

	// These can be deployed, so the line is plain rather than a warning.
	if s.SpotOnly > 0 {
		lines = append(lines, fmt.Sprintf("%d of them only as Spot (not enough on-demand quota)", s.SpotOnly))
	}

	for _, l := range leftOut(s) {
		lines = append(lines, st.Warn(l))
	}

	return lines
}

// vmFooter explains what the table does and does not show.
func vmFooter(st output.Styles, opts vmOptions, res vm.Result) []string {
	var lines []string

	if res.Summary.Shown < res.Summary.Eligible {
		lines = append(lines, st.Dim("Use --limit 0 to show all "+strconv.Itoa(res.Summary.Eligible)+"."))
	}

	lines = append(lines, st.Dim(pricingNote(opts)))

	if note := missingPriceNote(opts, res); note != "" {
		lines = append(lines, st.Dim(note))
	}

	if opts.Filter.IncludeSpot {
		if q := res.SpotQuota; q != nil {
			lines = append(lines, st.Dim(fmt.Sprintf("Spot quota: %d of %d vCPUs free in %s.", q.Free, q.Limit, res.Location)))
		}

		lines = append(lines, st.Dim("Spot VMs can be evicted. EVICTION is the hourly chance of that over the last 28 days."))
	}

	return lines
}

// requirementLines lists the requirements one to a line, as " - vCPUs >= 4"
// and " - Memory >= 16 GiB". It is nil when none is set.
func requirementLines(f vm.Filter) []string {
	var lines []string

	for _, c := range describeFilter(f) {
		lines = append(lines, " - "+criterionLabel(c.Name)+" "+c.Value)
	}

	return lines
}

// criterionLabel capitalises each word of a criterion name for the table, as
// "Disk Controller". A word already capitalised inside, such as vCPUs or GPUs,
// is left alone. The JSON keeps the names as they are, as scripts may match on
// them.
func criterionLabel(name string) string {
	words := strings.Fields(name)
	for i, w := range words {
		if w != strings.ToLower(w) {
			continue
		}

		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}

	return strings.Join(words, " ")
}

// emptyExplanation says why nothing is shown, in terms of the SKUs counted.
// The counts are kept out of the subject, so no number agreement is needed.
func emptyExplanation(res vm.Result) string {
	s := res.Summary

	switch {
	case s.Considered == 0:
		return fmt.Sprintf("No VM SKUs were returned for %s. Check the location name.", res.Location)
	case s.Matched == 0:
		return fmt.Sprintf("No VM SKUs in %s meet your requirements (%d checked).", res.Location, s.Considered)
	default:
		return fmt.Sprintf("None of the matching VM SKUs in %s can be deployed: %s.", res.Location, strings.Join(leftOut(s), ", "))
	}
}

// leftOut says how many matching SKUs were left out, and why, omitting any
// reason whose count is 0.
func leftOut(s vm.Summary) []string {
	var parts []string

	if s.Restricted > 0 {
		parts = append(parts, fmt.Sprintf("%d restricted for this subscription", s.Restricted))
	}

	if s.NoQuota > 0 {
		parts = append(parts, fmt.Sprintf("%d without enough quota", s.NoQuota))
	}

	return parts
}

func pricingNote(opts vmOptions) string {
	return fmt.Sprintf("Prices: pay-as-you-go estimates in %s for %s, at %d hours a month; "+
		"reservations, savings plans and discounts are not applied.", opts.Currency, osName(opts.Filter.OS), vm.HoursPerMonth)
}

// missingPriceNote says how many of the SKUs shown have no on-demand price, so
// that a price shown as NoValue is not taken for a fault. Azure has no Windows
// price for most Arm64 SKUs, so it says when they all are. It is "" when every
// SKU has a price, or when none could be fetched, which a warning explains.
func missingPriceNote(opts vmOptions, res vm.Result) string {
	if slices.ContainsFunc(res.Warnings, func(w vm.Warning) bool { return w.Code == vm.WarningPricingUnavailable }) {
		return ""
	}

	missing, arm := 0, 0

	for _, v := range res.VMs {
		if v.Pricing.OnDemandHourly == nil {
			missing++

			if v.Arch == vm.ArchARM64 {
				arm++
			}
		}
	}

	if missing == 0 {
		return ""
	}

	which := plural(missing, "1 SKU", strconv.Itoa(missing)+" SKUs")
	if arm == missing {
		which += plural(missing, " (Arm64)", " (all Arm64)")
	}

	return fmt.Sprintf("%s %s no %s retail price from Azure, so %s shown as %s.",
		which, plural(missing, "has", "have"), osName(opts.Filter.OS),
		plural(missing, "its price is", "their prices are"), output.NoValue)
}

// osName is the operating system as it is written in prose.
func osName(os vm.OS) string {
	if os == vm.OSWindows {
		return "Windows"
	}

	return "Linux"
}

func gib(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64) + " GiB"
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}

	return many
}

// vmColumns are the table columns. Styles, currency and Spot are CLI-only
// inputs, which is why the columns live here and not in vm. Every sortable
// column's header matches its vm.SortKeyNames() entry in lower case, so what a
// person sees is what they type for --sort-by.
func vmColumns(st output.Styles, currency string, withSpot bool) []output.Column[vm.VM] {
	cols := []output.Column[vm.VM]{
		{Header: "NAME", Value: func(v vm.VM) string { return v.Name }, Style: st.Bold},
		{Header: "VCPUS", Align: output.AlignRight, Value: vcpuText},
		{Header: "MEMORY", Align: output.AlignRight, Value: func(v vm.VM) string { return gib(v.MemoryGiB) }},
		{Header: "DISK", Value: diskControllerText},
		{Header: "NICS", Align: output.AlignRight, Value: nicText},
		{Header: "GPUS", Align: output.AlignRight, Value: gpuText},
		{Header: "ARCH", Value: func(v vm.VM) string { return string(v.Arch) }},
		{Header: "CATEGORY", Value: func(v vm.VM) string { return string(v.Category) }},
		{Header: "ZONES", Value: func(v vm.VM) string { return output.Zones(v.Zones) }},
		{Header: "FREEQUOTA", Align: output.AlignRight, Value: quotaText},
		{Header: "HOURLY", Align: output.AlignRight, Value: func(v vm.VM) string {
			return output.Money(currency, v.Pricing.OnDemandHourly, 4)
		}},
		{Header: "MONTHLY", Align: output.AlignRight, Value: func(v vm.VM) string {
			return output.Money(currency, v.Pricing.OnDemandMonthly, 2)
		}},
	}

	if withSpot {
		cols = append(cols,
			output.Column[vm.VM]{Header: "SPOT", Align: output.AlignRight, Value: func(v vm.VM) string {
				if v.Spot == nil {
					return output.NoValue
				}

				return output.Money(currency, v.Spot.HourlyPrice, 4)
			}},
			output.Column[vm.VM]{Header: "EVICTION", Value: evictionText},
		)
	}

	return cols
}

// vcpuText shows the vCPUs the guest sees, and the billed count when it
// differs: "4" or "4 (8 billed)".
func vcpuText(v vm.VM) string {
	if v.VCPUs == v.VCPUsAvailable {
		return strconv.Itoa(v.VCPUs)
	}

	return fmt.Sprintf("%d (%d billed)", v.VCPUsAvailable, v.VCPUs)
}

// gpuText shows NoValue for no GPUs.
func gpuText(v vm.VM) string {
	if v.GPUs == 0 {
		return output.NoValue
	}

	return strconv.Itoa(v.GPUs)
}

// nicText shows the most network interfaces the SKU takes, or NoValue when
// Azure did not say.
func nicText(v vm.VM) string {
	if v.MaxNICs == nil {
		return output.NoValue
	}

	return strconv.Itoa(*v.MaxNICs)
}

// diskControllerText shows the disk controllers as Azure writes them, such as
// "SCSI, NVMe", or NoValue when none is known.
func diskControllerText(v vm.VM) string {
	if len(v.DiskControllers) == 0 {
		return output.NoValue
	}

	names := make([]string, len(v.DiskControllers))
	for i, c := range v.DiskControllers {
		switch c {
		case vm.DiskControllerSCSI:
			names[i] = "SCSI"
		case vm.DiskControllerNVMe:
			names[i] = "NVMe"
		}
	}

	return strings.Join(names, ", ")
}

// quotaText shows "Spot only", the tightest free quota, or NoValue.
func quotaText(v vm.VM) string {
	if v.SpotOnly {
		return "Spot only"
	}

	free, ok := v.MinQuotaFree()
	if !ok {
		return output.NoValue
	}

	return strconv.FormatInt(free, 10)
}

// evictionText shows the band, such as "0-5%", "n/a" when the SKU is not
// Spot-capable, or NoValue when Azure had no rate.
func evictionText(v vm.VM) string {
	switch {
	case !v.SpotCapable:
		return "n/a"
	case v.Spot == nil || v.Spot.EvictionRate == nil:
		return output.NoValue
	default:
		return *v.Spot.EvictionRate + "%"
	}
}

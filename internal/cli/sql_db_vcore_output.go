package cli

import (
	"fmt"
	"io"
	"slices"
	"strconv"

	"github.com/mike-guy/azskufinder/internal/output"
	"github.com/mike-guy/azskufinder/internal/sql"
)

// sqlVCoreDocument is the JSON output of sql db vcore. Embedding the result
// flattens its fields (location, summary, serverQuota, results, warnings) into
// the document.
type sqlVCoreDocument struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Criteria   []criterion     `json:"criteria"`
	Pricing    sqlVCorePricing `json:"pricing"`
	sql.DBResult[sql.VCoreDatabase]
}

// sqlVCorePricing describes the prices in the results.
type sqlVCorePricing struct {
	Currency      string `json:"currency"`
	Licence       string `json:"licence"`
	ZoneRedundant bool   `json:"zoneRedundant"`
	HoursPerMonth int    `json:"hoursPerMonth"`
}

// memoryAuto is shown for serverless memory, which scales with the workload.
const memoryAuto = "auto"

// writeSQLVCoreResult writes res to w in the chosen format, styling the table
// with st.
func writeSQLVCoreResult(w io.Writer, st output.Styles, opts sqlVCoreOptions, res sql.DBResult[sql.VCoreDatabase]) error {
	f := opts.Filter
	criteria := describeVCoreFilter(f)

	if opts.Output == output.FormatJSON {
		return output.JSON(w, sqlVCoreDocument{
			APIVersion: jsonAPIVersion,
			Kind:       "sql-db-vcore",
			Criteria:   criteria,
			Pricing:    sqlVCorePricing{Currency: opts.Currency, Licence: string(f.Licence), ZoneRedundant: f.ZoneRedundant, HoursPerMonth: sql.HoursPerMonth},
			DBResult:   res,
		})
	}

	report := sqlReport{
		location: res.Location,
		summary:  res.Summary,
		criteria: criteria,
		nouns:    dbNouns,
		what:     "vCore-based SQL Database SKUs",
		footer:   vcoreFooter(st, opts, res),
	}

	return writeSQLTable(w, st, report, func(b io.Writer) error {
		return output.Table(b, st, vcoreColumns(st, opts.Currency), res.Databases)
	})
}

// describeVCoreFilter lists the requirements that are set. It is never nil.
func describeVCoreFilter(f sql.VCoreFilter) []criterion {
	out := appendList([]criterion{}, "tier", f.Tiers)

	if f.Compute != "" {
		out = append(out, criterion{Name: "compute", Value: string(f.Compute)})
	}

	out = appendList(out, "hardware", f.Hardware)
	out = appendCount(out, "vCores", f.ExactMatch, f.VCores)
	out = appendMemory(out, f.ExactMatch, f.MemoryGiB)

	return appendCommon(out, f.StorageGiB, f.ZoneRedundant, f.BackupRedundancy)
}

// vcoreFooter explains what the sql db vcore table does and does not show.
func vcoreFooter(st output.Styles, opts sqlVCoreOptions, res sql.DBResult[sql.VCoreDatabase]) []string {
	var lines []string

	if res.Summary.Shown < res.Summary.Eligible {
		lines = append(lines, st.Dim("Use --limit 0 to show all "+strconv.Itoa(res.Summary.Eligible)+"."))
	}

	what := "compute and SQL licence"
	if opts.Filter.Licence == sql.LicenceAHB {
		what = "compute, with Azure Hybrid Benefit covering the SQL licence"
	}

	lines = append(lines, st.Dim(sqlPricingNote(opts.Currency, what)))

	if opts.Filter.Licence == sql.LicenceAHB {
		lines = append(lines, st.Dim("Azure Hybrid Benefit does not apply to Hyperscale or serverless, which have no separate licence fee."))
	}

	if slices.ContainsFunc(res.Databases, func(d sql.VCoreDatabase) bool { return d.Compute == sql.ComputeServerless }) {
		lines = append(lines, st.Dim(fmt.Sprintf(
			"Serverless is billed per second of use; its price is for its maximum vCores for %d hours, and its memory scales with the workload (%s).",
			sql.HoursPerMonth, memoryAuto)))
	}

	if opts.Filter.ZoneRedundant {
		lines = append(lines, st.Dim("Zone redundancy surcharges are included where Azure charges one."))
	}

	unpriced := countUnpriced(res.Databases, func(d sql.VCoreDatabase) bool { return d.Pricing.Hourly == nil })
	if note := sqlMissingPriceNote(res.Warnings, unpriced); note != "" {
		lines = append(lines, st.Dim(note))
	}

	if note := serverQuotaNote(res.ServerQuota, res.Location); note != "" {
		lines = append(lines, st.Dim(note))
	}

	return lines
}

// vcoreColumns are the sql db vcore table columns. Every sortable column's
// header matches its sql.VCoreSortKeyNames() entry in lower case.
func vcoreColumns(st output.Styles, currency string) []output.Column[sql.VCoreDatabase] {
	return []output.Column[sql.VCoreDatabase]{
		{Header: "NAME", Value: func(d sql.VCoreDatabase) string { return d.Name }, Style: st.Bold},
		{Header: "TIER", Value: func(d sql.VCoreDatabase) string { return string(d.Tier) }},
		{Header: "COMPUTE", Value: func(d sql.VCoreDatabase) string { return string(d.Compute) }},
		{Header: "HARDWARE", Value: func(d sql.VCoreDatabase) string { return string(d.Hardware) }},
		{Header: "VCORES", Align: output.AlignRight, Value: func(d sql.VCoreDatabase) string { return strconv.Itoa(d.VCores) }},
		{Header: "MEMORY", Align: output.AlignRight, Value: memoryText},
		{Header: "MAXSTORAGE", Align: output.AlignRight, Value: func(d sql.VCoreDatabase) string { return storageText(d.MaxStorageGiB) }},
		{Header: "ZR", Value: func(d sql.VCoreDatabase) string { return yesNo(d.ZoneRedundant) }},
		{Header: "FREEQUOTA", Align: output.AlignRight, Value: func(d sql.VCoreDatabase) string { return freeText(d.MinQuotaFree()) }},
		{Header: "HOURLY", Align: output.AlignRight, Value: func(d sql.VCoreDatabase) string {
			return output.Money(currency, d.Pricing.Hourly, 4)
		}},
		{Header: "MONTHLY", Align: output.AlignRight, Value: func(d sql.VCoreDatabase) string {
			return output.Money(currency, d.Pricing.Monthly, 2)
		}},
	}
}

// memoryText shows the memory, memoryAuto for serverless, or NoValue when it is
// not known.
func memoryText(d sql.VCoreDatabase) string {
	switch {
	case d.Compute == sql.ComputeServerless:
		return memoryAuto
	case d.MemoryGiB == nil:
		return output.NoValue
	default:
		return gib(*d.MemoryGiB)
	}
}

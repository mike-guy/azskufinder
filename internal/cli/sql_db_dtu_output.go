package cli

import (
	"io"
	"strconv"

	"github.com/mike-guy/azskufinder/internal/output"
	"github.com/mike-guy/azskufinder/internal/sql"
)

// sqlDTUDocument is the JSON output of sql db dtu. Embedding the result
// flattens its fields (location, summary, serverQuota, results, warnings) into
// the document.
type sqlDTUDocument struct {
	APIVersion string        `json:"apiVersion"`
	Kind       string        `json:"kind"`
	Criteria   []criterion   `json:"criteria"`
	Pricing    sqlDTUPricing `json:"pricing"`
	sql.DBResult[sql.DTUDatabase]
}

// sqlDTUPricing describes the prices in the results. DTU prices always include
// the SQL licence, so there is no licence choice to report.
type sqlDTUPricing struct {
	Currency      string `json:"currency"`
	HoursPerMonth int    `json:"hoursPerMonth"`
}

// writeSQLDTUResult writes res to w in the chosen format, styling the table
// with st.
func writeSQLDTUResult(w io.Writer, st output.Styles, opts sqlDTUOptions, res sql.DBResult[sql.DTUDatabase]) error {
	criteria := describeDTUFilter(opts.Filter)

	if opts.Output == output.FormatJSON {
		return output.JSON(w, sqlDTUDocument{
			APIVersion: jsonAPIVersion,
			Kind:       "sql-db-dtu",
			Criteria:   criteria,
			Pricing:    sqlDTUPricing{Currency: opts.Currency, HoursPerMonth: sql.HoursPerMonth},
			DBResult:   res,
		})
	}

	report := sqlReport{
		location: res.Location,
		summary:  res.Summary,
		criteria: criteria,
		nouns:    dbNouns,
		what:     "DTU-based SQL Database SKUs",
		footer:   dtuFooter(st, opts, res),
	}

	return writeSQLTable(w, st, report, func(b io.Writer) error {
		return output.Table(b, st, dtuColumns(st, opts.Currency), res.Databases)
	})
}

// describeDTUFilter lists the requirements that are set. It is never nil.
func describeDTUFilter(f sql.DTUFilter) []criterion {
	out := appendList([]criterion{}, "tier", f.Tiers)
	out = appendCount(out, "DTUs", f.ExactMatch, f.DTUs)

	return appendCommon(out, f.StorageGiB, f.ZoneRedundant, f.BackupRedundancy)
}

// dtuFooter explains what the sql db dtu table does and does not show.
func dtuFooter(st output.Styles, opts sqlDTUOptions, res sql.DBResult[sql.DTUDatabase]) []string {
	var lines []string

	if res.Summary.Shown < res.Summary.Eligible {
		lines = append(lines, st.Dim("Use --limit 0 to show all "+strconv.Itoa(res.Summary.Eligible)+"."))
	}

	lines = append(lines, st.Dim(sqlPricingNote(opts.Currency, "the SKU, including the SQL licence")))

	unpriced := countUnpriced(res.Databases, func(d sql.DTUDatabase) bool { return d.Pricing.Hourly == nil })
	if note := sqlMissingPriceNote(res.Warnings, unpriced); note != "" {
		lines = append(lines, st.Dim(note))
	}

	lines = append(lines, st.Dim("FREEQUOTA is the regional vCore quota: each 100 DTUs, rounded up, uses 1 vCore."))

	if note := serverQuotaNote(res.ServerQuota, res.Location); note != "" {
		lines = append(lines, st.Dim(note))
	}

	return lines
}

// dtuColumns are the sql db dtu table columns. Every sortable column's header
// matches its sql.DTUSortKeyNames() entry in lower case.
func dtuColumns(st output.Styles, currency string) []output.Column[sql.DTUDatabase] {
	return []output.Column[sql.DTUDatabase]{
		{Header: "NAME", Value: func(d sql.DTUDatabase) string { return d.Name }, Style: st.Bold},
		{Header: "TIER", Value: func(d sql.DTUDatabase) string { return string(d.Tier) }},
		{Header: "DTUS", Align: output.AlignRight, Value: func(d sql.DTUDatabase) string { return strconv.Itoa(d.DTUs) }},
		{Header: "MAXSTORAGE", Align: output.AlignRight, Value: func(d sql.DTUDatabase) string { return storageText(d.MaxStorageGiB) }},
		{Header: "ZR", Value: func(d sql.DTUDatabase) string { return yesNo(d.ZoneRedundant) }},
		{Header: "FREEQUOTA", Align: output.AlignRight, Value: func(d sql.DTUDatabase) string { return freeText(d.MinQuotaFree()) }},
		{Header: "HOURLY", Align: output.AlignRight, Value: func(d sql.DTUDatabase) string {
			return output.Money(currency, d.Pricing.Hourly, 4)
		}},
		{Header: "MONTHLY", Align: output.AlignRight, Value: func(d sql.DTUDatabase) string {
			return output.Money(currency, d.Pricing.Monthly, 2)
		}},
	}
}

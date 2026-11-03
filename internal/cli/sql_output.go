package cli

import (
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/mike-guy/azskufinder/internal/output"
	"github.com/mike-guy/azskufinder/internal/sql"
)

// sqlNouns name what a sql command lists, for the headline and footer.
type sqlNouns struct {
	one, many string
}

var dbNouns = sqlNouns{one: "SKU", many: "SKUs"}

// sqlReport is what the table output says around the table.
type sqlReport struct {
	location string
	summary  sql.Summary
	criteria []criterion
	nouns    sqlNouns
	what     string // "SQL Database SKUs", for the empty result
	footer   []string
}

// writeSQLTable writes what was found, then the table, then what to bear in
// mind when reading it, as writeVMTable does.
func writeSQLTable(w io.Writer, st output.Styles, r sqlReport, table func(io.Writer) error) error {
	var b strings.Builder

	b.WriteString("\n")

	if r.summary.Shown == 0 {
		writeLines(&b, sqlNoResults(st, r))
	} else {
		writeLines(&b, sqlHeadline(st, r))
		b.WriteString("\n")

		if err := table(&b); err != nil {
			return err
		}

		b.WriteString("\n")
		writeLines(&b, r.footer)
	}

	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("writing results: %w", err)
	}

	return nil
}

// sqlHeadline says what was found, as vmHeadline does.
func sqlHeadline(st output.Styles, r sqlReport) []string {
	s := r.summary

	deployable := fmt.Sprintf("%d deployable %s", s.Eligible, plural(s.Eligible, r.nouns.one, r.nouns.many))
	if s.Shown < s.Eligible {
		deployable = fmt.Sprintf("Showing %d of %d deployable %s", s.Shown, s.Eligible, r.nouns.many)
	}

	requirements := criteriaLines(r.criteria)
	if len(requirements) > 0 {
		deployable += " with filters:"
	}

	lines := []string{
		st.Bold(fmt.Sprintf("%d matching %s in %s", s.Matched, plural(s.Matched, r.nouns.one, r.nouns.many), r.location)),
		st.Good(deployable),
	}

	for _, line := range requirements {
		lines = append(lines, st.Good(line))
	}

	for _, line := range sqlLeftOut(s) {
		lines = append(lines, st.Warn(line))
	}

	return lines
}

// sqlNoResults says why nothing can be deployed, and what was asked for.
func sqlNoResults(st output.Styles, r sqlReport) []string {
	s := r.summary

	var why string

	switch {
	case s.Considered == 0:
		why = fmt.Sprintf("No %s were returned for %s. Check the location name.", r.what, r.location)
	case s.Matched == 0:
		why = fmt.Sprintf("No %s in %s meet your requirements (%d checked).", r.what, r.location, s.Considered)
	default:
		why = fmt.Sprintf("None of the matching %s in %s can be deployed: %s.", r.what, r.location, strings.Join(sqlLeftOut(s), ", "))
	}

	lines := []string{st.Bold(why)}

	if requirements := criteriaLines(r.criteria); len(requirements) > 0 {
		lines = append(lines, "Filters:")
		lines = append(lines, requirements...)
	}

	return lines
}

// sqlLeftOut says how many matching options were left out, and why, omitting
// any reason whose count is 0.
func sqlLeftOut(s sql.Summary) []string {
	var parts []string

	if s.Restricted > 0 {
		parts = append(parts, fmt.Sprintf("%d restricted for this subscription", s.Restricted))
	}

	if s.NoQuota > 0 {
		parts = append(parts, fmt.Sprintf("%d without enough quota", s.NoQuota))
	}

	return parts
}

// criteriaLines lists criteria one to a line, as requirementLines does.
func criteriaLines(criteria []criterion) []string {
	var lines []string

	for _, c := range criteria {
		lines = append(lines, " - "+criterionLabel(c.Name)+" "+c.Value)
	}

	return lines
}

func appendList[T ~string](out []criterion, name string, values []T) []criterion {
	if len(values) == 0 {
		return out
	}

	texts := make([]string, len(values))
	for i, v := range values {
		texts[i] = string(v)
	}

	return append(out, criterion{Name: name, Value: strings.Join(texts, " or ")})
}

// appendCount lists a whole-number requirement, such as {"vCores", ">= 4"}. A
// minimum of 0 rules nothing out, so it is not listed.
func appendCount(out []criterion, name string, exact bool, n *int) []criterion {
	switch {
	case n == nil, !exact && *n == 0:
		return out
	case exact:
		return append(out, criterion{Name: name, Value: "= " + strconv.Itoa(*n)})
	default:
		return append(out, criterion{Name: name, Value: ">= " + strconv.Itoa(*n)})
	}
}

// appendMemory lists the memory requirement, as appendCount does.
func appendMemory(out []criterion, exact bool, memory *float64) []criterion {
	switch {
	case memory == nil, !exact && *memory == 0:
		return out
	case exact:
		return append(out, criterion{Name: "memory", Value: "= " + gib(*memory)})
	default:
		return append(out, criterion{Name: "memory", Value: ">= " + gib(*memory)})
	}
}

func appendCommon(out []criterion, storage *float64, zoneRedundant bool, backup sql.BackupRedundancy) []criterion {
	if storage != nil && *storage > 0 {
		out = append(out, criterion{Name: "storage", Value: ">= " + gib(*storage)})
	}

	if zoneRedundant {
		out = append(out, criterion{Name: "zone redundant", Value: "yes"})
	}

	if backup != "" {
		out = append(out, criterion{Name: "backup redundancy", Value: string(backup)})
	}

	return out
}

// sqlPricingNote says what the prices are. what is what they cover, such as
// "compute and SQL licence".
func sqlPricingNote(currency, what string) string {
	return fmt.Sprintf("Prices: pay-as-you-go estimates in %s for %s, at %d hours a month; "+
		"storage, backup, reservations and discounts are not included.", currency, what, sql.HoursPerMonth)
}

// serverQuotaNote says how many logical servers are left, or "" when Azure did
// not say.
func serverQuotaNote(q *sql.QuotaHeadroom, location string) string {
	if q == nil {
		return ""
	}

	return fmt.Sprintf("Logical servers: %d of %d free in %s.", q.Free, q.Limit, location)
}

func countUnpriced[T any](rows []T, unpriced func(T) bool) int {
	n := 0

	for _, r := range rows {
		if unpriced(r) {
			n++
		}
	}

	return n
}

// sqlMissingPriceNote says how many of the rows shown have no price, so that
// a price shown as NoValue is not taken for a fault. It is "" when every row
// has a price, or when none could be fetched, which a warning explains.
func sqlMissingPriceNote(warnings []sql.Warning, missing int) string {
	if missing == 0 || slices.ContainsFunc(warnings, func(w sql.Warning) bool { return w.Code == sql.WarningPricingUnavailable }) {
		return ""
	}

	return fmt.Sprintf("%s no retail price from Azure, so %s shown as %s.",
		plural(missing, "1 has", strconv.Itoa(missing)+" have"),
		plural(missing, "its price is", "their prices are"), output.NoValue)
}

// storageText shows a size in TiB when it is a whole number of them, such as
// "4 TiB", and otherwise in GiB.
func storageText(p *float64) string {
	switch {
	case p == nil:
		return output.NoValue
	case *p >= 1024 && *p == float64(int64(*p/1024))*1024:
		return strconv.FormatFloat(*p/1024, 'f', -1, 64) + " TiB"
	default:
		return gib(*p)
	}
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}

	return "no"
}

func freeText(free int64, ok bool) string {
	if !ok {
		return output.NoValue
	}

	return strconv.FormatInt(free, 10)
}

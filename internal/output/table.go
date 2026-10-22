package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

const ruleChar = "─"

// Align is how a column's cells are aligned.
type Align int

// Alignments a column can have.
const (
	AlignLeft Align = iota
	AlignRight
)

// Column is one table column over rows of T.
type Column[T any] struct {
	Header string
	Value  func(T) string
	Align  Align

	// Style, if set, is applied to each cell after it is sanitised, such as
	// Styles.Bold for the name column.
	Style func(string) string
}

// Table writes rows as a minimal table: a bold header, one dim rule under it,
// columns two spaces apart, no borders and no trailing spaces. Every cell is
// sanitised, so callers cannot forget to. go-pretty does the alignment, and
// measures widths without counting the colour codes. st styles the header and
// the rule.
func Table[T any](w io.Writer, st Styles, cols []Column[T], rows []T) error {
	t := table.NewWriter()
	t.SetStyle(minimalStyle())

	header := make(table.Row, len(cols))
	configs := make([]table.ColumnConfig, 0, len(cols))

	for i, c := range cols {
		header[i] = st.Bold(c.Header)

		if c.Align == AlignRight {
			configs = append(configs, table.ColumnConfig{Number: i + 1, Align: text.AlignRight, AlignHeader: text.AlignRight})
		}
	}

	t.AppendHeader(header)
	t.SetColumnConfigs(configs)

	for _, r := range rows {
		row := make(table.Row, len(cols))

		for i, c := range cols {
			cell := Sanitise(c.Value(r))
			if c.Style != nil {
				cell = c.Style(cell)
			}

			row[i] = cell
		}

		t.AppendRow(row)
	}

	if _, err := io.WriteString(w, tidy(st, t.Render())); err != nil {
		return fmt.Errorf("writing table: %w", err)
	}

	return nil
}

// minimalStyle is a table with no borders and no vertical lines: columns are
// separated by two spaces, and the header by a single rule.
func minimalStyle() table.Style {
	return table.Style{
		Name: "minimal",
		Box: table.BoxStyle{
			MiddleHorizontal: ruleChar,
			PaddingRight:     "  ",
		},
		Format: table.FormatOptions{Footer: text.FormatDefault, Header: text.FormatDefault, Row: text.FormatDefault},
		Options: table.Options{
			DrawBorder:      false,
			SeparateColumns: false,
			SeparateFooter:  false,
			SeparateHeader:  true,
			SeparateRows:    false,
		},
	}
}

// tidy removes the trailing padding go-pretty leaves on every line, and redraws
// the rule under the header so that it is exactly as wide as the table.
func tidy(st Styles, rendered string) string {
	lines := strings.Split(strings.TrimRight(rendered, "\n"), "\n")
	width := 0

	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")

		if i != 1 {
			width = max(width, text.StringWidthWithoutEscSequences(lines[i]))
		}
	}

	if len(lines) > 1 {
		lines[1] = st.Dim(strings.Repeat(ruleChar, width))
	}

	return strings.Join(lines, "\n") + "\n"
}

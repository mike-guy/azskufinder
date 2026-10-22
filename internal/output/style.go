package output

import (
	"strings"
	"unicode"

	"github.com/jedib0t/go-pretty/v6/text"
)

// Styles colours and styles text for one output stream. Whether colour is on
// is decided per stream by the command (stdout and stderr can be redirected
// separately), so it is a value rather than a process-wide switch. JSON is
// never coloured.
//
// The scheme is deliberately quiet: bold marks structure and dim marks context.
// Colour is kept for the outcome (green for what can be deployed, yellow for
// what was left out), warnings (yellow) and errors (red).
//
// The zero Styles has colour off.
type Styles struct {
	colour bool
}

// NewStyles returns Styles with colour and text styling on or off.
func NewStyles(colour bool) Styles {
	return Styles{colour: colour}
}

// Colour reports whether colour and text styling are on.
func (s Styles) Colour() bool { return s.colour }

// Bold emphasises text: headlines, the table header and names.
func (s Styles) Bold(str string) string { return s.paint(text.Colors{text.Bold}, str) }

// Dim de-emphasises text that is context rather than the answer.
func (s Styles) Dim(str string) string { return s.paint(text.Colors{text.Faint}, str) }

// Good marks the answer the person came for.
func (s Styles) Good(str string) string { return s.paint(text.Colors{text.FgGreen}, str) }

// Warn marks a warning, or something left out. Pass the whole message, not
// just a prefix.
func (s Styles) Warn(str string) string { return s.paint(text.Colors{text.FgYellow}, str) }

// Error marks an error. Pass the whole message, not just a prefix.
func (s Styles) Error(str string) string { return s.paint(text.Colors{text.FgRed}, str) }

// paint uses text.Escape rather than Colors.Sprint, which would also consult
// go-pretty's process-wide colour switch.
func (s Styles) paint(c text.Colors, str string) string {
	if !s.colour || str == "" {
		return str
	}

	return text.Escape(str, c.EscapeSeq())
}

// Sanitise removes control characters, such as the ESC that starts an escape
// sequence, a carriage return and the C1 controls, from text that came from
// Azure. Printed as is, such text could move the cursor, rewrite the terminal
// title or overwrite what came before it. Newlines and tabs are kept, because
// Azure's error messages span several lines. JSON does not need this: the
// encoder escapes control characters.
func Sanitise(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}

		return r
	}, s)
}

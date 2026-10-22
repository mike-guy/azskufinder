// Package output renders results for people (a table) or for scripts (JSON).
// It knows nothing about any resource type: commands supply the columns.
package output

import (
	"fmt"
	"slices"
	"strings"
)

// Format names accepted by ParseFormat.
const (
	FormatTable = "table"
	FormatJSON  = "json"
)

// Formats lists the supported format names.
func Formats() []string { return []string{FormatTable, FormatJSON} }

// ParseFormat normalises a format name and checks it is supported.
func ParseFormat(s string) (string, error) {
	f := strings.ToLower(strings.TrimSpace(s))
	if !slices.Contains(Formats(), f) {
		return "", fmt.Errorf("unknown output format %q: use one of %s", s, strings.Join(Formats(), ", "))
	}

	return f, nil
}

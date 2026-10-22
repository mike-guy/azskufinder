package output

import (
	"encoding/json"
	"fmt"
	"io"
)

// JSON writes v as indented JSON. HTML escaping is off so that ">=" stays
// readable.
func JSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)

	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("writing JSON: %w", err)
	}

	return nil
}

package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestJSON(t *testing.T) {
	t.Parallel()

	type price struct {
		Currency string   `json:"currency"`
		Hourly   *float64 `json:"hourly"`
	}

	type result struct {
		Name  string   `json:"name"`
		Zones []string `json:"zones"`
		Price price    `json:"price"`
	}

	tests := []struct {
		name string
		v    any
	}{
		{name: "nested", v: result{Name: "Standard_D2s_v5", Zones: []string{"1", "2"}, Price: price{Currency: "GBP"}}},
		{name: "html not escaped", v: map[string]string{"filter": "vcpus >= 4", "markup": "<a&b>"}},
		{name: "control characters", v: map[string]string{"message": "\x1b[31mred\r\n"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			if err := JSON(&buf, tc.v); err != nil {
				t.Fatalf("JSON() error = %v", err)
			}

			golden(t, "json_"+strings.ReplaceAll(tc.name, " ", "_"), buf.String())
		})
	}
}

func TestJSON_errors(t *testing.T) {
	t.Parallel()

	t.Run("unencodable value", func(t *testing.T) {
		t.Parallel()

		err := JSON(&bytes.Buffer{}, make(chan int))

		var typeErr *json.UnsupportedTypeError
		if !errors.As(err, &typeErr) {
			t.Errorf("JSON(chan) error = %v, want a *json.UnsupportedTypeError", err)
		}

		if err != nil && !strings.HasPrefix(err.Error(), "writing JSON") {
			t.Errorf("JSON(chan) error = %q, want it to start with %q", err, "writing JSON")
		}
	})

	t.Run("write fails", func(t *testing.T) {
		t.Parallel()

		err := JSON(errWriter{}, "x")
		if !errors.Is(err, errWrite) {
			t.Errorf("JSON(errWriter) error = %v, want %v", err, errWrite)
		}

		if err != nil && !strings.HasPrefix(err.Error(), "writing JSON") {
			t.Errorf("JSON(errWriter) error = %q, want it to start with %q", err, "writing JSON")
		}
	})
}

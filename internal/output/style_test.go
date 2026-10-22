package output

import (
	"testing"
	"unicode"
	"unicode/utf8"
)

func TestStyles(t *testing.T) {
	t.Parallel()

	methods := []struct {
		name string
		fn   func(Styles, string) string
		code string
	}{
		{name: "bold", fn: Styles.Bold, code: "\x1b[1m"},
		{name: "dim", fn: Styles.Dim, code: "\x1b[2m"},
		{name: "good", fn: Styles.Good, code: "\x1b[32m"},
		{name: "warn", fn: Styles.Warn, code: "\x1b[33m"},
		{name: "error", fn: Styles.Error, code: "\x1b[31m"},
	}

	type testCase struct {
		name   string
		styles Styles
		fn     func(Styles, string) string
		in     string
		want   string
	}

	var tests []testCase

	for _, m := range methods {
		tests = append(tests,
			testCase{name: m.name + "/zero value", styles: Styles{}, fn: m.fn, in: "text", want: "text"},
			testCase{name: m.name + "/colour off", styles: NewStyles(false), fn: m.fn, in: "text", want: "text"},
			testCase{name: m.name + "/colour on", styles: NewStyles(true), fn: m.fn, in: "text", want: m.code + "text\x1b[0m"},
			testCase{name: m.name + "/empty", styles: NewStyles(true), fn: m.fn, in: "", want: ""},
		)
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.fn(tc.styles, tc.in); got != tc.want {
				t.Errorf("%s(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
			}
		})
	}
}

var sanitiseTests = []struct {
	name string
	in   string
	want string
}{
	{name: "escape sequence", in: "\x1b[31mred", want: "[31mred"},
	{name: "carriage return", in: "safe\roverwritten", want: "safeoverwritten"},
	{name: "bell", in: "ding\a", want: "ding"},
	{name: "delete", in: "a\x7fb", want: "ab"},
	{name: "c1 control", in: "a\u009b31mb", want: "a31mb"},
	{name: "newline and tab kept", in: "line one\n\tline two", want: "line one\n\tline two"},
	{name: "non-ascii kept", in: "£ ⠋ 日本", want: "£ ⠋ 日本"},
	{name: "empty", in: "", want: ""},
}

func TestSanitise(t *testing.T) {
	t.Parallel()

	for _, tc := range sanitiseTests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := Sanitise(tc.in); got != tc.want {
				t.Errorf("Sanitise(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func FuzzSanitise(f *testing.F) {
	for _, tc := range sanitiseTests {
		f.Add(tc.in)
	}

	f.Fuzz(func(t *testing.T, s string) {
		got := Sanitise(s)

		for _, r := range got {
			if unicode.IsControl(r) && r != '\n' && r != '\t' {
				t.Fatalf("Sanitise(%q) = %q, which contains control character %U", s, got, r)
			}
		}

		if again := Sanitise(got); again != got {
			t.Errorf("Sanitise(Sanitise(%q)) = %q, want %q", s, again, got)
		}

		if utf8.ValidString(s) && len(got) > len(s) {
			t.Errorf("Sanitise(%q) = %q, longer than its input", s, got)
		}
	})
}

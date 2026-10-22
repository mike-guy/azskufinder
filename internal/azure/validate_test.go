package azure

import (
	"net/url"
	"strings"
	"testing"
)

type validCase struct {
	name string
	in   string
	want bool
}

var (
	subscriptionIDCases = []validCase{
		{name: "lower case", in: "0a1b2c3d-4e5f-6a7b-8c9d-0e1f2a3b4c5d", want: true},
		{name: "upper case", in: "0A1B2C3D-4E5F-6A7B-8C9D-0E1F2A3B4C5D", want: true},
		{name: "no hyphens", in: "0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d", want: false},
		{name: "braces", in: "{0a1b2c3d-4e5f-6a7b-8c9d-0e1f2a3b4c5d}", want: false},
		{name: "short group", in: "0a1b2c3-4e5f-6a7b-8c9d-0e1f2a3b4c5d", want: false},
		{name: "not hex", in: "0a1b2c3g-4e5f-6a7b-8c9d-0e1f2a3b4c5d", want: false},
		{name: "trailing newline", in: "0a1b2c3d-4e5f-6a7b-8c9d-0e1f2a3b4c5d\n", want: false},
		{name: "empty", in: "", want: false},
	}

	locationCases = []validCase{
		{name: "letters", in: "uksouth", want: true},
		{name: "digits", in: "eastus2", want: true},
		{name: "upper case", in: "UKSouth", want: false},
		{name: "space", in: "uk south", want: false},
		{name: "hyphen", in: "uk-south", want: false},
		{name: "quote", in: "uksouth'", want: false},
		{name: "trailing newline", in: "uksouth\n", want: false},
		{name: "empty", in: "", want: false},
	}

	skuNameCases = []validCase{
		{name: "plain", in: "Standard_D2s_v5", want: true},
		{name: "constrained core", in: "Standard_E8-4s_v5", want: true},
		{name: "double hyphen", in: "Standard_E8--4s_v5", want: false},
		{name: "leading hyphen", in: "-Standard_D2s_v5", want: false},
		{name: "trailing hyphen", in: "Standard_D2s_v5-", want: false},
		{name: "quote", in: "Standard_D2s_v5'", want: false},
		{name: "backslash", in: `Standard_D2s_v5\`, want: false},
		{name: "space", in: "Standard D2s_v5", want: false},
		{name: "trailing newline", in: "Standard_D2s_v5\n", want: false},
		{name: "empty", in: "", want: false},
	}
)

func runValidCases(t *testing.T, fnName string, fn func(string) bool, cases []validCase) {
	t.Helper()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := fn(tc.in); got != tc.want {
				t.Errorf("%s(%q) = %t, want %t", fnName, tc.in, got, tc.want)
			}
		})
	}
}

func TestValidSubscriptionID(t *testing.T) {
	t.Parallel()
	runValidCases(t, "ValidSubscriptionID", ValidSubscriptionID, subscriptionIDCases)
}

func TestValidLocation(t *testing.T) {
	t.Parallel()
	runValidCases(t, "ValidLocation", ValidLocation, locationCases)
}

func TestValidSKUName(t *testing.T) {
	t.Parallel()
	runValidCases(t, "ValidSKUName", ValidSKUName, skuNameCases)
}

// checkSafe fails the test unless s, which a Valid* check accepted, can go
// into a URL path, an OData string literal or a Kusto query unescaped.
func checkSafe(t *testing.T, fnName, s string) {
	t.Helper()

	if s == "" {
		t.Fatalf("%s(%q) = true for an empty string", fnName, s)
	}

	for _, r := range s {
		if r < '!' || r > '~' {
			t.Fatalf("%s(%q) = true with non-printable or non-ASCII %q", fnName, s, r)
		}
	}

	if esc := url.PathEscape(s); esc != s {
		t.Fatalf("%s(%q) = true but url.PathEscape gives %q", fnName, s, esc)
	}

	if strings.ContainsAny(s, "'\"\\%/?#&") {
		t.Fatalf("%s(%q) = true with a quote, escape or URL delimiter", fnName, s)
	}
}

func fuzzValid(f *testing.F, fnName string, fn func(string) bool, cases []validCase, extra func(*testing.T, string)) {
	f.Helper()

	for _, tc := range cases {
		f.Add(tc.in)
	}

	f.Fuzz(func(t *testing.T, s string) {
		if !fn(s) {
			return
		}

		checkSafe(t, fnName, s)

		if extra != nil {
			extra(t, s)
		}
	})
}

func FuzzValidSubscriptionID(f *testing.F) {
	fuzzValid(f, "ValidSubscriptionID", ValidSubscriptionID, subscriptionIDCases, nil)
}

func FuzzValidLocation(f *testing.F) {
	fuzzValid(f, "ValidLocation", ValidLocation, locationCases, nil)
}

func FuzzValidSKUName(f *testing.F) {
	fuzzValid(f, "ValidSKUName", ValidSKUName, skuNameCases, nil)
}

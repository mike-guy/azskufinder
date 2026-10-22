package output

import "testing"

func TestMoney(t *testing.T) {
	t.Parallel()

	price := func(f float64) *float64 { return &f }

	tests := []struct {
		name     string
		currency string
		price    *float64
		decimals int
		want     string
	}{
		{name: "unknown price", currency: "GBP", price: nil, decimals: 4, want: NoValue},
		{name: "pounds", currency: "GBP", price: price(0.1223), decimals: 4, want: "£0.1223"},
		{name: "dollars", currency: "USD", price: price(1.5), decimals: 2, want: "$1.50"},
		{name: "euros", currency: "EUR", price: price(2), decimals: 2, want: "€2.00"},
		{name: "other currency uses its code", currency: "SEK", price: price(1.2), decimals: 2, want: "SEK 1.20"},
		{name: "no currency", currency: "", price: price(1.2), decimals: 2, want: "1.20"},
		{name: "no decimals", currency: "GBP", price: price(89.6), decimals: 0, want: "£90"},
		{name: "rounds", currency: "GBP", price: price(0.12346), decimals: 4, want: "£0.1235"},
		{name: "negative", currency: "GBP", price: price(-0.5), decimals: 2, want: "£-0.50"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := Money(tc.currency, tc.price, tc.decimals); got != tc.want {
				t.Errorf("Money(%q, %v, %d) = %q, want %q", tc.currency, tc.price, tc.decimals, got, tc.want)
			}
		})
	}
}

func TestZones(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		zones []string
		want  string
	}{
		{name: "nil", zones: nil, want: NoValue},
		{name: "empty", zones: []string{}, want: NoValue},
		{name: "one", zones: []string{"2"}, want: "2"},
		{name: "several", zones: []string{"1", "2", "3"}, want: "1,2,3"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := Zones(tc.zones); got != tc.want {
				t.Errorf("Zones(%q) = %q, want %q", tc.zones, got, tc.want)
			}
		})
	}
}

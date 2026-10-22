package output

import (
	"strconv"
	"strings"
)

// NoValue is shown for anything unknown or absent.
const NoValue = "-"

// currencySymbols are the currencies shown with a symbol. Any other currency is
// shown with its code, which is never ambiguous: "$" alone could be one of
// several dollars.
var currencySymbols = map[string]string{"GBP": "£", "USD": "$", "EUR": "€"}

// Money formats a price with its currency symbol ("£0.1223"), or code and a
// space for other currencies ("SEK 1.20"), or NoValue when it is unknown.
func Money(currency string, p *float64, decimals int) string {
	if p == nil {
		return NoValue
	}

	prefix, ok := currencySymbols[currency]
	if !ok && currency != "" {
		prefix = currency + " "
	}

	return prefix + strconv.FormatFloat(*p, 'f', decimals, 64)
}

// Zones joins zones as "1,2,3", or NoValue when there are none.
func Zones(z []string) string {
	if len(z) == 0 {
		return NoValue
	}

	return strings.Join(z, ",")
}

package azure

import "regexp"

// These check the shape of values that end up in URLs, OData filters and
// Kusto queries.
var (
	subscriptionIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	locationPattern       = regexp.MustCompile(`^[a-z0-9]+$`)

	// SKU names may contain single hyphens, as constrained-core sizes do
	// (Standard_E8-4s_v5), but no quotes, spaces or other punctuation.
	skuNamePattern = regexp.MustCompile(`^[A-Za-z0-9_]+(-[A-Za-z0-9_]+)*$`)
)

// ValidSubscriptionID reports whether s is shaped like a subscription ID (a UUID).
func ValidSubscriptionID(s string) bool { return subscriptionIDPattern.MatchString(s) }

// ValidLocation reports whether s is shaped like an ARM location name, such as
// "uksouth". It does not check that the location exists.
func ValidLocation(s string) bool { return locationPattern.MatchString(s) }

// ValidSKUName reports whether s is shaped like an ARM SKU name, such as
// "Standard_E8-4s_v5", which is the same for every resource type. A name it
// accepts can be placed in a Kusto string literal unescaped.
func ValidSKUName(s string) bool { return skuNamePattern.MatchString(s) }

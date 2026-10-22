package cli

import (
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/mike-guy/azskufinder/internal/azure"
	"github.com/mike-guy/azskufinder/internal/output"
)

// maxFloatFlag is a sanity bound for decimal flags such as --memory.
const maxFloatFlag = 1e9

// commonOptions are the validated settings every resource command uses.
type commonOptions struct {
	SubscriptionID string
	Location       string
	Output         string
	Currency       string
	Limit          int

	// Timeout bounds the whole run; zero or less means no limit.
	Timeout time.Duration
}

// readCommon reads and validates the settings shared by the commands.
// Everything a person can get wrong is reported here, as a usage error, before
// any request is made.
func readCommon(v *viper.Viper) (commonOptions, error) {
	var (
		o   commonOptions
		err error
	)

	o.SubscriptionID = strings.TrimSpace(v.GetString("subscription-id"))
	if o.SubscriptionID == "" {
		return o, withHint(usageErrorf("--subscription-id is required"), "Use --subscription-id or set "+envName("subscription-id")+".")
	}

	if !azure.ValidSubscriptionID(o.SubscriptionID) {
		return o, usageErrorf("--subscription-id must look like 00000000-0000-0000-0000-000000000000, got %q", o.SubscriptionID)
	}

	o.Location = strings.ToLower(strings.TrimSpace(v.GetString("location")))
	if o.Location == "" {
		return o, withHint(usageErrorf("--location is required"), "Use --location or set "+envName("location")+".")
	}

	if !azure.ValidLocation(o.Location) {
		return o, usageErrorf("--location must be an Azure location name such as uksouth, got %q", v.GetString("location"))
	}

	if o.Output, err = output.ParseFormat(v.GetString("output")); err != nil {
		return o, usageErrorf("--output: %w", err)
	}

	o.Currency = strings.ToUpper(strings.TrimSpace(v.GetString("currency")))
	if !slices.Contains(currencies, o.Currency) {
		return o, usageErrorf("--currency must be one of %s, got %q", strings.Join(currencies, ", "), v.GetString("currency"))
	}

	limit, err := readOptionalInt(v, "limit")
	if err != nil {
		return o, err
	}

	o.Limit = defaultLimit
	if limit != nil {
		o.Limit = *limit
	}

	if o.Timeout, err = readDuration(v, "timeout"); err != nil {
		return o, err
	}

	return o, nil
}

// readDuration reads a duration such as "90s" or "2m". Unlike viper's
// GetDuration, a bare number is an error rather than nanoseconds, and a value
// it cannot read is an error rather than silently zero.
func readDuration(v *viper.Viper, key string) (time.Duration, error) {
	s := strings.TrimSpace(v.GetString(key))

	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, usageErrorf("--%s must be a duration such as 90s or 2m, got %q", key, s)
	}

	return d, nil
}

// readOptionalInt reads a whole number that may be unset. It returns nil when
// the value was not given as a flag, environment variable or config key, which
// is what tells "--gpus 0" apart from no --gpus.
func readOptionalInt(v *viper.Viper, key string) (*int, error) {
	if !v.IsSet(key) {
		return nil, nil //nolint:nilnil // nil means "not given"
	}

	s := strings.TrimSpace(v.GetString(key))

	n, err := strconv.Atoi(s)
	if err != nil {
		return nil, usageErrorf("--%s must be a whole number, got %q", key, s)
	}

	if n < 0 {
		return nil, usageErrorf("--%s must not be negative, got %d", key, n)
	}

	return &n, nil
}

// readOptionalFloat reads a decimal number that may be unset.
func readOptionalFloat(v *viper.Viper, key string) (*float64, error) {
	if !v.IsSet(key) {
		return nil, nil //nolint:nilnil // nil means "not given"
	}

	s := strings.TrimSpace(v.GetString(key))

	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || f < 0 || f > maxFloatFlag {
		return nil, usageErrorf("--%s must be a number of at least 0, got %q", key, s)
	}

	return &f, nil
}

// readBool reads a flag, environment variable or config value as a bool. Unset
// means false. Unlike viper's GetBool, a value such as "yes" is an error rather
// than silently false.
func readBool(v *viper.Viper, key string) (bool, error) {
	s := strings.TrimSpace(v.GetString(key))
	if s == "" {
		return false, nil
	}

	b, err := strconv.ParseBool(s)
	if err != nil {
		return false, usageErrorf("--%s must be true or false, got %q", key, s)
	}

	return b, nil
}

package vm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v9"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph"

	"github.com/mike-guy/azskufinder/internal/azure"
)

const (
	// resourceTypeVM is the Resource SKUs API resourceType of virtual machines.
	resourceTypeVM = "virtualMachines"

	// spotEvictionType is the SpotResources type holding eviction rates.
	spotEvictionType = "microsoft.compute/skuspotevictionrate/location"

	// spotPageSize is the most rows Resource Graph returns in one page.
	spotPageSize int32 = 1000

	// spotBatchSize is how many SKU names go into one query. Unlike the SKU and
	// usage calls, this query carries the names in its text, which Resource
	// Graph limits in length.
	spotBatchSize = 100

	// spotMaxPages caps the hand-written skip-token loop. The SKU and usage
	// calls use SDK pagers, which stop on an empty nextLink, so they need no
	// cap; --timeout bounds every call regardless.
	spotMaxPages = 100
)

// ARMSource reads virtual machine SKUs, compute quota and Spot eviction rates
// for one subscription. It is the only code that sees ARM SDK types: it
// converts them into the package's own small structs straight away.
type ARMSource struct {
	skus   *armcompute.ResourceSKUsClient
	usages *armcompute.UsageClient
	graph  *armresourcegraph.Client
	log    *slog.Logger
}

// NewARMSource checks the subscription ID and creates the SDK clients. opts
// are the shared Azure client options; any ARM-only option is set here. A nil
// logger discards debug output.
func NewARMSource(subscriptionID string, cred azcore.TokenCredential, opts policy.ClientOptions, log *slog.Logger) (*ARMSource, error) {
	if !azure.ValidSubscriptionID(subscriptionID) {
		return nil, fmt.Errorf("%q is not a valid subscription ID", subscriptionID)
	}

	armOpts := &arm.ClientOptions{ClientOptions: opts}

	skus, err := armcompute.NewResourceSKUsClient(subscriptionID, cred, armOpts)
	if err != nil {
		return nil, fmt.Errorf("creating resource SKU client: %w", err)
	}

	usages, err := armcompute.NewUsageClient(subscriptionID, cred, armOpts)
	if err != nil {
		return nil, fmt.Errorf("creating usage client: %w", err)
	}

	graph, err := armresourcegraph.NewClient(cred, armOpts)
	if err != nil {
		return nil, fmt.Errorf("creating Resource Graph client: %w", err)
	}

	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	return &ARMSource{skus: skus, usages: usages, graph: graph, log: log.With("component", "vm.arm")}, nil
}

// listSKUs returns the virtual machine SKUs offered in location, with the
// restrictions that apply to the subscription there. The location goes into
// the filter text, so the caller must have checked it: Service.Find does.
func (a *ARMSource) listSKUs(ctx context.Context, location string) ([]sku, error) {
	filter := "location eq '" + location + "'"
	pager := a.skus.NewListPager(&armcompute.ResourceSKUsClientListOptions{Filter: &filter})

	skus := []sku{}
	pages, otherTypes := 0, 0

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing resource SKUs: %w", err)
		}

		pages++

		for _, s := range page.Value {
			if s != nil && deref(s.ResourceType) == resourceTypeVM {
				skus = append(skus, convertSKU(s))
			} else {
				otherTypes++
			}
		}
	}

	a.log.Debug("listed resource SKUs", "location", location, "pages", pages, "vm_skus", len(skus), "other_types", otherTypes)

	return skus, nil
}

// listQuota returns the compute quota of location, from the Usages API, keyed
// by quota name, such as "standardDSv3Family", "cores" and "lowPriorityCores".
func (a *ARMSource) listQuota(ctx context.Context, location string) (map[string]quota, error) {
	pager := a.usages.NewListPager(location, nil)

	quotas := map[string]quota{}
	pages, incomplete := 0, 0

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing compute usage: %w", err)
		}

		pages++

		for _, u := range page.Value {
			if u == nil || u.Name == nil || u.Name.Value == nil || u.CurrentValue == nil || u.Limit == nil {
				incomplete++

				continue
			}

			quotas[*u.Name.Value] = quota{Used: int64(*u.CurrentValue), Limit: *u.Limit}
		}
	}

	a.log.Debug("listed compute usage", "location", location, "pages", pages, "entries", len(quotas), "incomplete", incomplete)

	return quotas, nil
}

// convertSKU turns an SDK SKU into ours. The location filter on the request
// scopes the SKU's location info and restrictions to that location, so all of
// them apply.
func convertSKU(s *armcompute.ResourceSKU) sku {
	out := sku{
		Name:            deref(s.Name),
		Family:          deref(s.Family),
		Capabilities:    make(map[string]string, len(s.Capabilities)),
		Zones:           []string{},
		RestrictedZones: []string{},
	}

	for _, c := range s.Capabilities {
		if c != nil && c.Name != nil {
			out.Capabilities[*c.Name] = deref(c.Value)
		}
	}

	for _, info := range s.LocationInfo {
		if info != nil {
			out.Zones = append(out.Zones, derefAll(info.Zones)...)
		}
	}

	for _, r := range s.Restrictions {
		if r == nil {
			continue
		}

		switch deref(r.Type) {
		case armcompute.ResourceSKURestrictionsTypeLocation:
			out.LocationRestricted = true
		case armcompute.ResourceSKURestrictionsTypeZone:
			if r.RestrictionInfo != nil {
				out.RestrictedZones = append(out.RestrictedZones, derefAll(r.RestrictionInfo.Zones)...)
			}
		}
	}

	return out
}

// listEvictionRates returns the Spot eviction rate band of each SKU in location,
// such as "0-5" (the hourly chance, in percent, over the last 28 days). The map
// is keyed by the lower-case SKU name. SKUs Azure has no rate for are left out,
// and so are names that could not be placed safely in the query: one odd name
// must not cost every other SKU its rate.
func (a *ARMSource) listEvictionRates(ctx context.Context, location string, skus []string) (map[string]string, error) {
	rates := map[string]string{}

	valid := slices.DeleteFunc(slices.Clone(skus), func(s string) bool {
		if azure.ValidSKUName(s) {
			return false
		}

		a.log.Debug("skipping SKU", "sku", s, "reason", "unsafe-name-for-query")

		return true
	})

	batch := 0

	for names := range slices.Chunk(valid, spotBatchSize) {
		batch++

		if err := a.listEvictionRatesBatch(ctx, location, batch, names, rates); err != nil {
			return nil, fmt.Errorf("querying Spot eviction rates: %w", err)
		}
	}

	a.log.Debug("queried Spot eviction rates", "location", location, "skus", len(valid), "batches", batch, "rates", len(rates))

	return rates, nil
}

// listEvictionRatesBatch adds the rates of one batch of SKUs to rates.
func (a *ARMSource) listEvictionRatesBatch(ctx context.Context, location string, batch int, skus []string, rates map[string]string) error {
	query := evictionQuery(location, skus)

	var skipToken *string

	seen := map[string]bool{}

	for page := 1; page <= spotMaxPages; page++ {
		// No Subscriptions on purpose: SpotResources holds tenant-wide data
		// that belongs to no subscription, so a query scoped to one returns no
		// rows at all.
		resp, err := a.graph.Resources(ctx, armresourcegraph.QueryRequest{
			Query: &query,
			Options: &armresourcegraph.QueryRequestOptions{
				ResultFormat: new(armresourcegraph.ResultFormatObjectArray),
				Top:          new(spotPageSize),
				SkipToken:    skipToken,
			},
		}, nil)
		if err != nil {
			return err
		}

		rows, err := decodeEvictionRows(resp.Data)
		if err != nil {
			return err
		}

		a.log.Debug("Spot eviction rates page", "batch", batch, "page", page, "skus", len(skus), "rows", len(rows))

		for _, row := range rows {
			if row.EvictionRate != "" {
				rates[strings.ToLower(row.SKU)] = row.EvictionRate
			}
		}

		if resp.SkipToken == nil || *resp.SkipToken == "" {
			return nil
		}

		if seen[*resp.SkipToken] {
			return errors.New("page repeated by Resource Graph")
		}

		seen[*resp.SkipToken] = true
		skipToken = resp.SkipToken
	}

	return fmt.Errorf("more than %d pages", spotMaxPages)
}

type evictionRow struct {
	SKU          string `json:"sku"`
	EvictionRate string `json:"evictionRate"`
}

// decodeEvictionRows reads the rows from the response, which the SDK returns as
// untyped JSON.
func decodeEvictionRows(data any) ([]evictionRow, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("reading rows: %w", err)
	}

	var rows []evictionRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("decoding rows: %w", err)
	}

	return rows, nil
}

// evictionQuery builds the Kusto query. The location and SKU names go into the
// query text, so callers must have checked them: Service.Find checks the
// location, and listEvictionRates the names.
func evictionQuery(location string, skus []string) string {
	names := make([]string, len(skus))
	for i, s := range skus {
		names[i] = "'" + s + "'"
	}

	return fmt.Sprintf(`SpotResources
| where type =~ '%s'
| where location =~ '%s'
| where sku.name in~ (%s)
| project type, sku = tostring(sku.name), evictionRate = tostring(properties.evictionRate)`,
		spotEvictionType, location, strings.Join(names, ", "))
}

// deref returns the value p points to, or "" for nil. T is string or one of the
// string-based enum types the SDK uses, which keeps its type.
func deref[T ~string](p *T) T {
	if p == nil {
		return ""
	}

	return *p
}

// derefAll returns the non-nil strings from a slice of string pointers.
func derefAll(ps []*string) []string {
	out := make([]string, 0, len(ps))

	for _, p := range ps {
		if p != nil {
			out = append(out, *p)
		}
	}

	return out
}

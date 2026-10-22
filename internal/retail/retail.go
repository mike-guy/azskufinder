// Package retail is a client for the Azure Retail Prices API. There is no
// official SDK, so it sends requests through an azcore pipeline, which gives it
// the same retries, user agent and debug logging as the ARM clients. It knows
// nothing about any resource type.
package retail

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
)

const (
	endpoint = "https://prices.azure.com/api/retail/prices"

	// apiVersion is the newest version as of October 2026, and the only one
	// that returns savings plan prices.
	apiVersion = "2023-01-01-preview"

	// moduleName identifies this client in the azcore telemetry user agent.
	moduleName = "retail"

	// maxRetryDelay caps how long a retry may wait. The API answers throttling
	// with Retry-After: 60, far too long for a CLI, and azcore gives up on a
	// Retry-After above the cap. Quick failures, such as a 503, still retry.
	maxRetryDelay = 5 * time.Second
)

// ErrThrottled reports that the Retail Prices API is rate limiting requests.
var ErrThrottled = errors.New("the Azure Retail Prices API is rate limiting requests, try again in a minute")

// Price is one meter from the Retail Prices API.
type Price struct {
	CurrencyCode         string    `json:"currencyCode"`
	RetailPrice          float64   `json:"retailPrice"`
	UnitPrice            float64   `json:"unitPrice"`
	TierMinimumUnits     float64   `json:"tierMinimumUnits"`
	UnitOfMeasure        string    `json:"unitOfMeasure"`
	ARMRegionName        string    `json:"armRegionName"`
	ARMSKUName           string    `json:"armSkuName"`
	SKUName              string    `json:"skuName"`
	MeterName            string    `json:"meterName"`
	ProductName          string    `json:"productName"`
	ServiceName          string    `json:"serviceName"`
	ServiceFamily        string    `json:"serviceFamily"`
	Type                 string    `json:"type"`
	ReservationTerm      string    `json:"reservationTerm"`
	IsPrimaryMeterRegion bool      `json:"isPrimaryMeterRegion"`
	EffectiveStartDate   time.Time `json:"effectiveStartDate"`
}

// page is one page of a Retail Prices API response.
type page struct {
	Items        []Price `json:"Items"`
	NextPageLink string  `json:"NextPageLink"`
}

// Client queries the Retail Prices API in one currency.
type Client struct {
	pl       runtime.Pipeline
	currency string
	endpoint string
	log      *slog.Logger
}

// NewClient returns a client that asks for prices in currency, an ISO 4217
// code such as GBP. opts are the shared Azure client options. Unless opts sets
// its own Retry.MaxRetryDelay, a throttled request fails with ErrThrottled
// instead of waiting. A nil logger discards debug output.
func NewClient(currency string, opts policy.ClientOptions, log *slog.Logger) *Client {
	if opts.Retry.MaxRetryDelay == 0 {
		opts.Retry.MaxRetryDelay = maxRetryDelay
	}

	pl := runtime.NewPipeline(moduleName, apiVersion, runtime.PipelineOptions{
		AllowedQueryParameters: []string{"currencyCode", "$skip"},
	}, &opts)

	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	return &Client{pl: pl, currency: currency, endpoint: endpoint, log: log.With("component", "retail")}
}

// Query returns every price matching filter, an OData expression such as
// "serviceName eq 'Virtual Machines'", following NextPageLink until the last
// page. A NextPageLink seen before is an error rather than a loop. Filter
// values are case-sensitive, so they must match the API's casing exactly.
// Build string literals with Quote.
func (c *Client) Query(ctx context.Context, filter string) ([]Price, error) {
	var prices []Price

	c.log.Debug("querying retail prices", "currency", c.currency, "filter", filter)

	n := 0
	seen := map[string]bool{}

	for u := c.firstURL(filter); u != ""; {
		if seen[u] {
			return nil, errors.New("querying retail prices: page repeated by the Retail Prices API")
		}

		seen[u] = true
		n++

		p, err := c.fetch(ctx, u)
		if err != nil {
			return nil, fmt.Errorf("querying retail prices: %w", err)
		}

		c.log.Debug("retail prices page", "page", n, "items", len(p.Items), "more", p.NextPageLink != "")

		prices = append(prices, p.Items...)
		u = p.NextPageLink
	}

	c.log.Debug("queried retail prices", "pages", n, "total", len(prices))

	return prices, nil
}

// firstURL returns the URL of the first page. Later pages come from
// NextPageLink, which carries the same parameters.
func (c *Client) firstURL(filter string) string {
	q := url.Values{}
	q.Set("api-version", apiVersion)
	q.Set("currencyCode", Quote(c.currency))
	q.Set("$filter", filter)

	return c.endpoint + "?" + q.Encode()
}

// fetch gets and decodes one page.
func (c *Client) fetch(ctx context.Context, u string) (page, error) {
	req, err := runtime.NewRequest(ctx, http.MethodGet, u)
	if err != nil {
		return page{}, err
	}

	resp, err := c.pl.Do(req)
	if err != nil {
		return page{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if runtime.HasStatusCode(resp, http.StatusTooManyRequests) {
		c.log.Debug("retail prices throttled", "retry_after", resp.Header.Get("Retry-After"))

		return page{}, fmt.Errorf("%w (%w)", ErrThrottled, runtime.NewResponseError(resp))
	}

	if !runtime.HasStatusCode(resp, http.StatusOK) {
		return page{}, runtime.NewResponseError(resp)
	}

	var p page
	if err := runtime.UnmarshalAsJSON(resp, &p); err != nil {
		return page{}, err
	}

	return p, nil
}

// Quote returns s as an OData string literal: wrapped in single quotes, with
// any single quote inside doubled.
func Quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

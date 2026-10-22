package retail

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/google/go-cmp/cmp"
)

const testFilter = "serviceName eq 'Virtual Machines'"

// fastRetry retries quickly, so tests that retry do not wait.
var fastRetry = policy.ClientOptions{Retry: policy.RetryOptions{RetryDelay: time.Millisecond}}

// newTestClient serves h and returns a GBP client pointed at it, with a count
// of the requests the server has received.
func newTestClient(t *testing.T, opts policy.ClientOptions, h http.HandlerFunc) (*Client, *atomic.Int32) {
	t.Helper()

	var n atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		h(w, r)
	}))
	t.Cleanup(srv.Close)

	c := NewClient("GBP", opts, nil)
	c.endpoint = srv.URL

	return c, &n
}

// writePage writes p as a JSON response.
func writePage(t *testing.T, w http.ResponseWriter, p page) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(p); err != nil {
		t.Errorf("encoding page: %v", err)
	}
}

func TestQuote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain", in: "GBP", want: "'GBP'"},
		{name: "empty", in: "", want: "''"},
		{name: "embedded quote", in: "O'Brien", want: "'O''Brien'"},
		{name: "two quotes", in: "''", want: "''''''"},
		{name: "only a quote", in: "'", want: "''''"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := Quote(tc.in); got != tc.want {
				t.Errorf("Quote(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestClient_firstURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		currency     string
		filter       string
		wantCurrency string
	}{
		{
			name:         "simple filter",
			currency:     "GBP",
			filter:       testFilter,
			wantCurrency: "'GBP'",
		},
		{
			name:         "compound filter",
			currency:     "USD",
			filter:       "serviceName eq 'Virtual Machines' and armRegionName eq 'uksouth'",
			wantCurrency: "'USD'",
		},
		{
			name:         "filter with ampersand and equals",
			currency:     "GBP",
			filter:       "productName eq 'A&B=C'",
			wantCurrency: "'GBP'",
		},
		{
			name:         "currency with quote",
			currency:     "G'B",
			filter:       testFilter,
			wantCurrency: "'G''B'",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c := NewClient(tc.currency, policy.ClientOptions{}, nil)

			raw := c.firstURL(tc.filter)

			u, err := url.Parse(raw)
			if err != nil {
				t.Fatalf("firstURL(%q) = %q, not a URL: %v", tc.filter, raw, err)
			}

			if got := u.Scheme + "://" + u.Host + u.Path; got != endpoint {
				t.Errorf("firstURL(%q) endpoint = %q, want %q", tc.filter, got, endpoint)
			}

			want := url.Values{
				"api-version":  {apiVersion},
				"currencyCode": {tc.wantCurrency},
				"$filter":      {tc.filter},
			}
			if diff := cmp.Diff(want, u.Query()); diff != "" {
				t.Errorf("firstURL(%q) query mismatch (-want +got):\n%s", tc.filter, diff)
			}
		})
	}
}

func TestClient_Query(t *testing.T) {
	t.Parallel()

	a := Price{ARMSKUName: "Standard_D2s_v5", RetailPrice: 0.1}
	b := Price{ARMSKUName: "Standard_D4s_v5", RetailPrice: 0.2}
	c := Price{ARMSKUName: "Standard_D8s_v5", RetailPrice: 0.4}

	tests := []struct {
		name    string
		handler func(t *testing.T) http.HandlerFunc
		want    []Price
	}{
		{
			name: "single page",
			handler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					writePage(t, w, page{Items: []Price{a, b}})
				}
			},
			want: []Price{a, b},
		},
		{
			name: "three pages",
			handler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					base := "http://" + r.Host

					switch r.URL.Path {
					case "/":
						writePage(t, w, page{Items: []Price{a}, NextPageLink: base + "/p2?$skip=100"})
					case "/p2":
						writePage(t, w, page{Items: []Price{b}, NextPageLink: base + "/p3?$skip=200"})
					case "/p3":
						writePage(t, w, page{Items: []Price{c}})
					default:
						t.Errorf("unexpected request for %s", r.URL)
						http.NotFound(w, r)
					}
				}
			},
			want: []Price{a, b, c},
		},
		{
			name: "empty result",
			handler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					writePage(t, w, page{Items: []Price{}})
				}
			},
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client, _ := newTestClient(t, policy.ClientOptions{}, tc.handler(t))

			got, err := client.Query(t.Context(), testFilter)
			if err != nil {
				t.Fatalf("Query(%q) error = %v, want nil", testFilter, err)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Query(%q) mismatch (-want +got):\n%s", testFilter, diff)
			}
		})
	}
}

func TestClient_Query_decodesAllFields(t *testing.T) {
	t.Parallel()

	const body = `{
		"BillingCurrency": "GBP",
		"Items": [{
			"currencyCode": "GBP",
			"tierMinimumUnits": 1,
			"reservationTerm": "1 Year",
			"retailPrice": 512.5,
			"unitPrice": 498.25,
			"armRegionName": "uksouth",
			"location": "UK South",
			"effectiveStartDate": "2024-05-01T00:00:00Z",
			"meterId": "00000000-0000-0000-0000-000000000000",
			"meterName": "D2s v5",
			"productId": "DZH318Z0BQ4L",
			"skuId": "DZH318Z0BQ4L/00SQ",
			"productName": "Virtual Machines Dsv5 Series",
			"skuName": "D2s v5",
			"serviceName": "Virtual Machines",
			"serviceId": "DZH313Z7MMC8",
			"serviceFamily": "Compute",
			"unitOfMeasure": "1 Hour",
			"type": "Reservation",
			"isPrimaryMeterRegion": true,
			"armSkuName": "Standard_D2s_v5"
		}],
		"NextPageLink": null,
		"Count": 1
	}`

	client, _ := newTestClient(t, policy.ClientOptions{}, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})

	got, err := client.Query(t.Context(), testFilter)
	if err != nil {
		t.Fatalf("Query(%q) error = %v, want nil", testFilter, err)
	}

	want := []Price{{
		CurrencyCode:         "GBP",
		RetailPrice:          512.5,
		UnitPrice:            498.25,
		TierMinimumUnits:     1,
		UnitOfMeasure:        "1 Hour",
		ARMRegionName:        "uksouth",
		ARMSKUName:           "Standard_D2s_v5",
		SKUName:              "D2s v5",
		MeterName:            "D2s v5",
		ProductName:          "Virtual Machines Dsv5 Series",
		ServiceName:          "Virtual Machines",
		ServiceFamily:        "Compute",
		Type:                 "Reservation",
		ReservationTerm:      "1 Year",
		IsPrimaryMeterRegion: true,
		EffectiveStartDate:   time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC),
	}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Query(%q) mismatch (-want +got):\n%s", testFilter, diff)
	}
}

func TestClient_Query_followsNextPageLinkVerbatim(t *testing.T) {
	t.Parallel()

	// The shape of a real NextPageLink: the API's own parameters, partly encoded.
	const next = "/api/retail/prices?currencyCode='GBP'&$filter=serviceName%20eq%20%27Virtual%20Machines%27&$skip=100"

	var got atomic.Value

	client, n := newTestClient(t, policy.ClientOptions{}, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			writePage(t, w, page{NextPageLink: "http://" + r.Host + next})
			return
		}

		got.Store(r.RequestURI)
		writePage(t, w, page{})
	})

	if _, err := client.Query(t.Context(), testFilter); err != nil {
		t.Fatalf("Query(%q) error = %v, want nil", testFilter, err)
	}

	if n.Load() != 2 {
		t.Fatalf("Query(%q) sent %d requests, want 2", testFilter, n.Load())
	}

	if got.Load() != next {
		t.Errorf("second request URI = %v, want %q", got.Load(), next)
	}
}

func TestClient_Query_errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		handler      func(t *testing.T) http.HandlerFunc
		wantRequests int32
		// wantStatus is the status of the *azcore.ResponseError in the chain,
		// or 0 when the error is not a response error.
		wantStatus int
	}{
		{
			name: "bad request",
			handler: func(*testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					http.Error(w, `{"Error":{"Code":"InvalidFilter"}}`, http.StatusBadRequest)
				}
			},
			wantRequests: 1,
			wantStatus:   http.StatusBadRequest,
		},
		{
			name: "malformed json",
			handler: func(*testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte("not json"))
				}
			},
			wantRequests: 1,
		},
		{
			name: "later page fails",
			handler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/" {
						writePage(t, w, page{Items: []Price{{ARMSKUName: "Standard_D2s_v5"}}, NextPageLink: "http://" + r.Host + "/p2"})
						return
					}

					http.Error(w, "bad", http.StatusBadRequest)
				}
			},
			wantRequests: 2,
			wantStatus:   http.StatusBadRequest,
		},
		{
			name: "next page link repeats",
			handler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					writePage(t, w, page{Items: []Price{{ARMSKUName: "Standard_D2s_v5"}}, NextPageLink: "http://" + r.Host + "/p2"})
				}
			},
			wantRequests: 2,
		},
		{
			name: "unparseable next page link",
			handler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					writePage(t, w, page{Items: []Price{{ARMSKUName: "Standard_D2s_v5"}}, NextPageLink: "://bad"})
				}
			},
			wantRequests: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client, n := newTestClient(t, fastRetry, tc.handler(t))

			got, err := client.Query(t.Context(), testFilter)
			if err == nil {
				t.Fatalf("Query(%q) error = nil, want an error", testFilter)
			}

			if got != nil {
				t.Errorf("Query(%q) = %v, want nil", testFilter, got)
			}

			if errors.Is(err, ErrThrottled) {
				t.Errorf("Query(%q) error = %v, want not ErrThrottled", testFilter, err)
			}

			if got := n.Load(); got != tc.wantRequests {
				t.Errorf("Query(%q) sent %d requests, want %d", testFilter, got, tc.wantRequests)
			}

			if tc.wantStatus == 0 {
				return
			}

			var respErr *azcore.ResponseError
			if !errors.As(err, &respErr) {
				t.Fatalf("Query(%q) error = %v, want an *azcore.ResponseError", testFilter, err)
			}

			if respErr.StatusCode != tc.wantStatus {
				t.Errorf("Query(%q) status = %d, want %d", testFilter, respErr.StatusCode, tc.wantStatus)
			}
		})
	}
}

func TestClient_Query_throttled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		opts         policy.ClientOptions
		retryAfter   string
		wantRequests int32
	}{
		{
			// azcore's own cap is 60s, so without ours this would wait a minute.
			name:         "default cap gives up on long retry after",
			retryAfter:   "60",
			wantRequests: 1,
		},
		{
			// Our 5s default would retry after 1s; the caller's 500ms cap does not.
			name:         "caller's cap is kept",
			opts:         policy.ClientOptions{Retry: policy.RetryOptions{MaxRetryDelay: 500 * time.Millisecond}},
			retryAfter:   "1",
			wantRequests: 1,
		},
		{
			name:         "no retry after retries then fails",
			opts:         fastRetry,
			wantRequests: 4,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client, n := newTestClient(t, tc.opts, func(w http.ResponseWriter, _ *http.Request) {
				if tc.retryAfter != "" {
					w.Header().Set("Retry-After", tc.retryAfter)
				}

				w.WriteHeader(http.StatusTooManyRequests)
			})

			// A regression that waits out Retry-After fails here instead of hanging.
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			t.Cleanup(cancel)

			got, err := client.Query(ctx, testFilter)
			if !errors.Is(err, ErrThrottled) {
				t.Fatalf("Query(%q) error = %v, want ErrThrottled", testFilter, err)
			}

			if got != nil {
				t.Errorf("Query(%q) = %v, want nil", testFilter, got)
			}

			var respErr *azcore.ResponseError
			if !errors.As(err, &respErr) || respErr.StatusCode != http.StatusTooManyRequests {
				t.Errorf("Query(%q) error = %v, want an *azcore.ResponseError with status 429", testFilter, err)
			}

			if got := n.Load(); got != tc.wantRequests {
				t.Errorf("Query(%q) sent %d requests, want %d", testFilter, got, tc.wantRequests)
			}
		})
	}
}

func TestClient_Query_retriesTransientFailure(t *testing.T) {
	t.Parallel()

	want := []Price{{ARMSKUName: "Standard_D2s_v5"}}

	var tries atomic.Int32

	client, n := newTestClient(t, fastRetry, func(w http.ResponseWriter, _ *http.Request) {
		if tries.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		writePage(t, w, page{Items: want})
	})

	got, err := client.Query(t.Context(), testFilter)
	if err != nil {
		t.Fatalf("Query(%q) error = %v, want nil", testFilter, err)
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Query(%q) mismatch (-want +got):\n%s", testFilter, diff)
	}

	if got := n.Load(); got != 2 {
		t.Errorf("Query(%q) sent %d requests, want 2", testFilter, got)
	}
}

func TestClient_Query_cancelledContext(t *testing.T) {
	t.Parallel()

	client, n := newTestClient(t, policy.ClientOptions{}, func(w http.ResponseWriter, _ *http.Request) {
		writePage(t, w, page{})
	})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	got, err := client.Query(ctx, testFilter)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Query(%q) error = %v, want context.Canceled", testFilter, err)
	}

	if got != nil {
		t.Errorf("Query(%q) = %v, want nil", testFilter, got)
	}

	if got := n.Load(); got != 0 {
		t.Errorf("Query(%q) sent %d requests, want 0", testFilter, got)
	}
}

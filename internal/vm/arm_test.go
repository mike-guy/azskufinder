package vm

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	azfake "github.com/Azure/azure-sdk-for-go/sdk/azcore/fake"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v9"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v9/fake"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph"
	rgfake "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph/fake"
	"github.com/google/go-cmp/cmp"
)

func TestNewARMSource(t *testing.T) {
	t.Parallel()

	t.Run("valid", func(t *testing.T) {
		t.Parallel()

		a, err := NewARMSource(testSubscriptionID, &azfake.TokenCredential{}, policy.ClientOptions{}, nil)
		if err != nil || a == nil {
			t.Errorf("NewARMSource() = %v, %v, want a source and no error", a, err)
		}
	})

	t.Run("invalid subscription", func(t *testing.T) {
		t.Parallel()

		_, err := NewARMSource("not-a-guid", &azfake.TokenCredential{}, policy.ClientOptions{}, nil)
		if err == nil || !strings.Contains(err.Error(), "not a valid subscription ID") {
			t.Errorf("NewARMSource(%q) error = %v, want an invalid subscription error", "not-a-guid", err)
		}
	})
}

// vmSKU is an SDK virtual machine SKU with the given name.
func vmSKU(name string) *armcompute.ResourceSKU {
	return &armcompute.ResourceSKU{
		Name:         new(name),
		Family:       new("standardDSv5Family"),
		ResourceType: new(resourceTypeVM),
		Capabilities: []*armcompute.ResourceSKUCapabilities{{Name: new("vCPUs"), Value: new("2")}},
	}
}

func TestARMSource_listSKUs(t *testing.T) {
	t.Parallel()

	var filter string

	srv := &fake.ResourceSKUsServer{
		NewListPager: func(opts *armcompute.ResourceSKUsClientListOptions) (resp azfake.PagerResponder[armcompute.ResourceSKUsClientListResponse]) {
			if opts != nil && opts.Filter != nil {
				filter = *opts.Filter
			}

			resp.AddPage(http.StatusOK, armcompute.ResourceSKUsClientListResponse{ResourceSKUsResult: armcompute.ResourceSKUsResult{
				Value: []*armcompute.ResourceSKU{
					vmSKU("Standard_D2s_v5"),
					{Name: new("Premium_LRS"), ResourceType: new("disks")},
					nil,
				},
			}}, nil)
			resp.AddPage(http.StatusOK, armcompute.ResourceSKUsClientListResponse{ResourceSKUsResult: armcompute.ResourceSKUsResult{
				Value: []*armcompute.ResourceSKU{vmSKU("Standard_D4s_v5")},
			}}, nil)

			return resp
		},
	}

	got, err := newTestARMSource(t, srv, nil, nil).listSKUs(t.Context(), "uksouth")
	if err != nil {
		t.Fatalf("listSKUs() error = %v", err)
	}

	var gotNames []string
	for _, s := range got {
		gotNames = append(gotNames, s.Name)
	}

	if diff := cmp.Diff([]string{"Standard_D2s_v5", "Standard_D4s_v5"}, gotNames); diff != "" {
		t.Errorf("listSKUs() names mismatch (-want +got):\n%s", diff)
	}

	if want := "location eq 'uksouth'"; filter != want {
		t.Errorf("listSKUs() filter = %q, want %q", filter, want)
	}
}

func TestARMSource_listSKUs_error(t *testing.T) {
	t.Parallel()

	srv := &fake.ResourceSKUsServer{
		NewListPager: func(*armcompute.ResourceSKUsClientListOptions) (resp azfake.PagerResponder[armcompute.ResourceSKUsClientListResponse]) {
			resp.AddResponseError(http.StatusForbidden, "AuthorizationFailed")

			return resp
		},
	}

	_, err := newTestARMSource(t, srv, nil, nil).listSKUs(t.Context(), "uksouth")
	if err == nil || !strings.Contains(err.Error(), "listing resource SKUs") || !strings.Contains(err.Error(), "AuthorizationFailed") {
		t.Errorf("listSKUs() error = %v, want a wrapped AuthorizationFailed", err)
	}
}

func TestConvertSKU(t *testing.T) {
	t.Parallel()

	zone := func(zones ...string) []*string {
		out := make([]*string, len(zones))
		for i, z := range zones {
			out[i] = new(z)
		}

		return out
	}

	tests := []struct {
		name string
		in   *armcompute.ResourceSKU
		want sku
	}{
		{
			name: "empty",
			in:   &armcompute.ResourceSKU{},
			want: sku{Capabilities: map[string]string{}, Zones: []string{}, RestrictedZones: []string{}},
		},
		{
			name: "capabilities",
			in: &armcompute.ResourceSKU{
				Name:   new("Standard_D2s_v5"),
				Family: new("standardDSv5Family"),
				Capabilities: []*armcompute.ResourceSKUCapabilities{
					{Name: new("vCPUs"), Value: new("2")},
					{Name: new("NoValue")},
					{Value: new("no name")},
					nil,
				},
			},
			want: sku{
				Name: "Standard_D2s_v5", Family: "standardDSv5Family",
				Capabilities: map[string]string{"vCPUs": "2", "NoValue": ""},
				Zones:        []string{}, RestrictedZones: []string{},
			},
		},
		{
			name: "zones from every location info",
			in: &armcompute.ResourceSKU{
				LocationInfo: []*armcompute.ResourceSKULocationInfo{
					{Zones: zone("1", "2")},
					nil,
					{Zones: []*string{new("3"), nil}},
				},
			},
			want: sku{Capabilities: map[string]string{}, Zones: []string{"1", "2", "3"}, RestrictedZones: []string{}},
		},
		{
			name: "restrictions",
			in: &armcompute.ResourceSKU{
				Restrictions: []*armcompute.ResourceSKURestrictions{
					nil,
					{Type: new(armcompute.ResourceSKURestrictionsTypeZone), RestrictionInfo: &armcompute.ResourceSKURestrictionInfo{Zones: zone("2")}},
					{Type: new(armcompute.ResourceSKURestrictionsTypeZone)},
					{Type: new(armcompute.ResourceSKURestrictionsTypeLocation)},
				},
			},
			want: sku{Capabilities: map[string]string{}, Zones: []string{}, RestrictedZones: []string{"2"}, LocationRestricted: true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tc.want, convertSKU(tc.in)); diff != "" {
				t.Errorf("convertSKU() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestARMSource_listQuota(t *testing.T) {
	t.Parallel()

	usage := func(name string, used int32, limit int64) *armcompute.Usage {
		return &armcompute.Usage{Name: &armcompute.UsageName{Value: new(name)}, CurrentValue: new(used), Limit: new(limit)}
	}

	var location string

	srv := &fake.UsageServer{
		NewListPager: func(loc string, _ *armcompute.UsageClientListOptions) (resp azfake.PagerResponder[armcompute.UsageClientListResponse]) {
			location = loc

			resp.AddPage(http.StatusOK, armcompute.UsageClientListResponse{ListUsagesResult: armcompute.ListUsagesResult{
				Value: []*armcompute.Usage{
					usage(regionalCores, 10, 100),
					nil,
					{Limit: new(int64(5))},
					{Name: &armcompute.UsageName{}, CurrentValue: new(int32(1)), Limit: new(int64(5))},
					{Name: &armcompute.UsageName{Value: new("noUsed")}, Limit: new(int64(5))},
					{Name: &armcompute.UsageName{Value: new("noLimit")}, CurrentValue: new(int32(1))},
				},
			}}, nil)
			resp.AddPage(http.StatusOK, armcompute.UsageClientListResponse{ListUsagesResult: armcompute.ListUsagesResult{
				Value: []*armcompute.Usage{usage(spotCores, 0, 50)},
			}}, nil)

			return resp
		},
	}

	got, err := newTestARMSource(t, nil, srv, nil).listQuota(t.Context(), "uksouth")
	if err != nil {
		t.Fatalf("listQuota() error = %v", err)
	}

	want := map[string]quota{
		regionalCores: {Used: 10, Limit: 100},
		spotCores:     {Used: 0, Limit: 50},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("listQuota() mismatch (-want +got):\n%s", diff)
	}

	if location != "uksouth" {
		t.Errorf("listQuota() location = %q, want %q", location, "uksouth")
	}
}

func TestARMSource_listQuota_error(t *testing.T) {
	t.Parallel()

	srv := &fake.UsageServer{
		NewListPager: func(string, *armcompute.UsageClientListOptions) (resp azfake.PagerResponder[armcompute.UsageClientListResponse]) {
			resp.AddResponseError(http.StatusForbidden, "AuthorizationFailed")

			return resp
		},
	}

	_, err := newTestARMSource(t, nil, srv, nil).listQuota(t.Context(), "uksouth")
	if err == nil || !strings.Contains(err.Error(), "listing compute usage") {
		t.Errorf("listQuota() error = %v, want a wrapped error", err)
	}
}

// graphPage is one page of Resource Graph rows, followed by skipToken when it
// is not empty.
type graphPage struct {
	rows      any
	skipToken string
}

// graphServer is a Resource Graph fake that records each query and serves the
// page that page returns for it. Calls are made one at a time, from the
// test's goroutine, so the records need no lock.
type graphServer struct {
	queries    []string
	skipTokens []string
	scoped     int // queries limited to subscriptions or management groups
	page       func(call int) graphPage
	err        bool
}

func (g *graphServer) server() *rgfake.Server {
	return &rgfake.Server{
		Resources: func(_ context.Context, q armresourcegraph.QueryRequest, _ *armresourcegraph.ClientResourcesOptions) (resp azfake.Responder[armresourcegraph.ClientResourcesResponse], errResp azfake.ErrorResponder) {
			g.queries = append(g.queries, deref(q.Query))

			if len(q.Subscriptions) > 0 || len(q.ManagementGroups) > 0 {
				g.scoped++
			}

			if q.Options != nil {
				g.skipTokens = append(g.skipTokens, deref(q.Options.SkipToken))
			}

			if g.err {
				errResp.SetResponseError(http.StatusBadRequest, "BadRequest")

				return resp, errResp
			}

			p := g.page(len(g.queries))

			var token *string
			if p.skipToken != "" {
				token = new(p.skipToken)
			}

			resp.SetResponse(http.StatusOK, armresourcegraph.ClientResourcesResponse{QueryResponse: armresourcegraph.QueryResponse{
				Data:      p.rows,
				SkipToken: token,
			}}, nil)

			return resp, errResp
		},
	}
}

func rate(sku, rate string) any {
	return map[string]any{"sku": sku, "evictionRate": rate}
}

func TestARMSource_listEvictionRates(t *testing.T) {
	t.Parallel()

	g := &graphServer{page: func(int) graphPage {
		return graphPage{rows: []any{rate("Standard_D2s_v5", "0-5"), rate("Standard_E8-4s_v5", ""), rate("standard_D4s_v5", "5-10")}}
	}}

	skus := []string{"Standard_D2s_v5", "Standard_E8-4s_v5", "bad'name", "a b", "-x", "Standard_D4s_v5"}

	got, err := newTestARMSource(t, nil, nil, g.server()).listEvictionRates(t.Context(), "uksouth", skus)
	if err != nil {
		t.Fatalf("listEvictionRates() error = %v", err)
	}

	// Keyed by lower-case name, and a SKU without a rate is left out.
	want := map[string]string{"standard_d2s_v5": "0-5", "standard_d4s_v5": "5-10"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("listEvictionRates() mismatch (-want +got):\n%s", diff)
	}

	if len(g.queries) != 1 {
		t.Fatalf("listEvictionRates() made %d queries, want 1", len(g.queries))
	}

	// The unsafe names are left out and the others still asked for.
	if want := evictionQuery("uksouth", []string{"Standard_D2s_v5", "Standard_E8-4s_v5", "Standard_D4s_v5"}); g.queries[0] != want {
		t.Errorf("listEvictionRates() query = %q, want %q", g.queries[0], want)
	}

	// SpotResources is tenant-wide data: a query scoped to subscriptions
	// returns no rows at all.
	if g.scoped != 0 {
		t.Errorf("listEvictionRates() scoped %d queries to subscriptions or management groups, want none", g.scoped)
	}
}

func TestARMSource_listEvictionRates_batches(t *testing.T) {
	t.Parallel()

	g := &graphServer{page: func(int) graphPage { return graphPage{} }}

	skus := make([]string, 250)
	for i := range skus {
		skus[i] = fmt.Sprintf("Standard_D%d", i)
	}

	if _, err := newTestARMSource(t, nil, nil, g.server()).listEvictionRates(t.Context(), "uksouth", skus); err != nil {
		t.Fatalf("listEvictionRates() error = %v", err)
	}

	var sizes []int
	for _, q := range g.queries {
		sizes = append(sizes, strings.Count(q, "'Standard_D"))
	}

	if diff := cmp.Diff([]int{100, 100, 50}, sizes); diff != "" {
		t.Errorf("listEvictionRates() batch sizes mismatch (-want +got):\n%s", diff)
	}
}

func TestARMSource_listEvictionRates_pages(t *testing.T) {
	t.Parallel()

	g := &graphServer{page: func(call int) graphPage {
		if call == 1 {
			return graphPage{rows: []any{rate("Standard_D2s_v5", "0-5")}, skipToken: "next"}
		}

		return graphPage{rows: []any{rate("Standard_D4s_v5", "5-10")}}
	}}

	got, err := newTestARMSource(t, nil, nil, g.server()).listEvictionRates(t.Context(), "uksouth", []string{"Standard_D2s_v5", "Standard_D4s_v5"})
	if err != nil {
		t.Fatalf("listEvictionRates() error = %v", err)
	}

	if diff := cmp.Diff(map[string]string{"standard_d2s_v5": "0-5", "standard_d4s_v5": "5-10"}, got); diff != "" {
		t.Errorf("listEvictionRates() mismatch (-want +got):\n%s", diff)
	}

	if diff := cmp.Diff([]string{"", "next"}, g.skipTokens); diff != "" {
		t.Errorf("listEvictionRates() skip tokens mismatch (-want +got):\n%s", diff)
	}
}

func TestARMSource_listEvictionRates_errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		g       *graphServer
		wantErr string
	}{
		{
			name:    "query fails",
			g:       &graphServer{err: true},
			wantErr: "querying Spot eviction rates",
		},
		{
			name:    "page repeated",
			g:       &graphServer{page: func(int) graphPage { return graphPage{skipToken: "same"} }},
			wantErr: "page repeated by Resource Graph",
		},
		{
			name:    "too many pages",
			g:       &graphServer{page: func(call int) graphPage { return graphPage{skipToken: fmt.Sprint(call)} }},
			wantErr: fmt.Sprintf("more than %d pages", spotMaxPages),
		},
		{
			name:    "rows not an array",
			g:       &graphServer{page: func(int) graphPage { return graphPage{rows: map[string]any{"sku": "x"}} }},
			wantErr: "decoding rows",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := newTestARMSource(t, nil, nil, tc.g.server()).listEvictionRates(t.Context(), "uksouth", []string{"Standard_D2s_v5"})

			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("listEvictionRates() error = %v, want one containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestARMSource_listEvictionRates_noSafeNames(t *testing.T) {
	t.Parallel()

	g := &graphServer{page: func(int) graphPage { return graphPage{} }}

	got, err := newTestARMSource(t, nil, nil, g.server()).listEvictionRates(t.Context(), "uksouth", []string{"bad'name"})
	if err != nil || len(got) != 0 {
		t.Errorf("listEvictionRates() = %v, %v, want an empty map and no error", got, err)
	}

	if len(g.queries) != 0 {
		t.Errorf("listEvictionRates() made %d queries, want none", len(g.queries))
	}
}

func TestEvictionQuery(t *testing.T) {
	t.Parallel()

	want := `SpotResources
| where type =~ 'microsoft.compute/skuspotevictionrate/location'
| where location =~ 'uksouth'
| where sku.name in~ ('Standard_D2s_v5', 'Standard_E8-4s_v5')
| project type, sku = tostring(sku.name), evictionRate = tostring(properties.evictionRate)`

	if got := evictionQuery("uksouth", []string{"Standard_D2s_v5", "Standard_E8-4s_v5"}); got != want {
		t.Errorf("evictionQuery() = %q, want %q", got, want)
	}
}

func TestDecodeEvictionRows(t *testing.T) {
	t.Parallel()

	t.Run("rows", func(t *testing.T) {
		t.Parallel()

		got, err := decodeEvictionRows([]any{rate("Standard_D2s_v5", "0-5"), map[string]any{"sku": "x", "other": 1}})
		if err != nil {
			t.Fatalf("decodeEvictionRows() error = %v", err)
		}

		want := []evictionRow{{SKU: "Standard_D2s_v5", EvictionRate: "0-5"}, {SKU: "x"}}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("decodeEvictionRows() mismatch (-want +got):\n%s", diff)
		}
	})

	errTests := []struct {
		name    string
		data    any
		wantErr string
	}{
		{name: "not an array", data: map[string]any{"sku": "x"}, wantErr: "decoding rows"},
		{name: "not JSON", data: func() {}, wantErr: "reading rows"},
	}

	for _, tc := range errTests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := decodeEvictionRows(tc.data)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("decodeEvictionRows() error = %v, want one containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestDeref(t *testing.T) {
	t.Parallel()

	if got := deref[string](nil); got != "" {
		t.Errorf("deref(nil) = %q, want %q", got, "")
	}

	if got := deref(new("x")); got != "x" {
		t.Errorf("deref(&%q) = %q, want %q", "x", got, "x")
	}

	// Enum types keep their type, so they can be switched on.
	if got := deref(new(armcompute.ResourceSKURestrictionsTypeZone)); got != armcompute.ResourceSKURestrictionsTypeZone {
		t.Errorf("deref(&Zone) = %q, want %q", got, armcompute.ResourceSKURestrictionsTypeZone)
	}
}

func TestDerefAll(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   []*string
		want []string
	}{
		{name: "nil", in: nil, want: []string{}},
		{name: "nil entries skipped", in: []*string{new("1"), nil, new("2")}, want: []string{"1", "2"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tc.want, derefAll(tc.in)); diff != "" {
				t.Errorf("derefAll() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

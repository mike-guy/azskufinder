package vm

import (
	"context"
	"log/slog"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	azfake "github.com/Azure/azure-sdk-for-go/sdk/azcore/fake"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v7"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v7/fake"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph"
	rgfake "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph/fake"

	"github.com/mike-guy/azskufinder/internal/retail"
)

const testSubscriptionID = "00000000-0000-0000-0000-000000000000"

// discard is the logger for the internal functions that take one directly.
var discard = slog.New(slog.DiscardHandler)

// skuOf builds a readable virtual machine SKU, x64 unless an option says
// otherwise.
func skuOf(name, family string, vcpus int, memGiB float64, opts ...func(*sku)) sku {
	s := sku{
		Name:   name,
		Family: family,
		Capabilities: map[string]string{
			"vCPUs":               strconv.Itoa(vcpus),
			"MemoryGB":            strconv.FormatFloat(memGiB, 'f', -1, 64),
			"CpuArchitectureType": "x64",
		},
		Zones:           []string{},
		RestrictedZones: []string{},
	}

	for _, o := range opts {
		o(&s)
	}

	return s
}

func withCap(name, value string) func(*sku) {
	return func(s *sku) { s.Capabilities[name] = value }
}

func withGPUs(n int) func(*sku) { return withCap("GPUs", strconv.Itoa(n)) }

func withAvailable(n int) func(*sku) { return withCap("vCPUsAvailable", strconv.Itoa(n)) }

func withSpot() func(*sku) { return withCap("LowPriorityCapable", "True") }

func withZones(zones ...string) func(*sku) {
	return func(s *sku) { s.Zones = zones }
}

func restrictedZones(zones ...string) func(*sku) {
	return func(s *sku) { s.RestrictedZones = zones }
}

func locationRestricted() func(*sku) {
	return func(s *sku) { s.LocationRestricted = true }
}

// vmOf parses a SKU built by skuOf, failing the test if it cannot be read.
func vmOf(t *testing.T, s sku) VM {
	t.Helper()

	v, ok := parseVM(s)
	if !ok {
		t.Fatalf("parseVM(%q) failed", s.Name)
	}

	return v
}

// priceRow builds an hourly consumption price row.
func priceRow(skuName, meter, product string, price float64) retail.Price {
	return retail.Price{
		ARMSKUName:    skuName,
		MeterName:     meter,
		ProductName:   product,
		RetailPrice:   price,
		Type:          "Consumption",
		UnitOfMeasure: "1 Hour",
	}
}

// names returns the names of the VMs, in order.
func names(vms []VM) []string {
	out := make([]string, len(vms))
	for i, v := range vms {
		out[i] = v.Name
	}

	return out
}

// fakeARM is an armSource that returns canned data. With waitSKUs, waitQuota
// or waitRates, that call blocks until its context is done.
type fakeARM struct {
	skus   []sku
	quotas map[string]quota
	rates  map[string]string

	skusErr, quotaErr, ratesErr    error
	waitSKUs, waitQuota, waitRates bool

	mu            sync.Mutex
	calls         int
	evictionCalls [][]string
}

func (f *fakeARM) listSKUs(ctx context.Context, _ string) ([]sku, error) {
	f.count()

	if f.waitSKUs {
		<-ctx.Done()

		return nil, ctx.Err()
	}

	return f.skus, f.skusErr
}

func (f *fakeARM) listQuota(ctx context.Context, _ string) (map[string]quota, error) {
	f.count()

	if f.waitQuota {
		<-ctx.Done()

		return nil, ctx.Err()
	}

	return f.quotas, f.quotaErr
}

func (f *fakeARM) listEvictionRates(ctx context.Context, _ string, skus []string) (map[string]string, error) {
	f.mu.Lock()
	f.calls++
	f.evictionCalls = append(f.evictionCalls, slices.Clone(skus))
	f.mu.Unlock()

	if f.waitRates {
		<-ctx.Done()

		return nil, ctx.Err()
	}

	return f.rates, f.ratesErr
}

func (f *fakeARM) count() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++
}

// fakePrices is a priceSource that returns canned rows. With wait, it blocks
// until its context is done.
type fakePrices struct {
	rows []retail.Price
	err  error
	wait bool

	mu     sync.Mutex
	calls  int
	filter string
}

func (f *fakePrices) Query(ctx context.Context, filter string) ([]retail.Price, error) {
	f.mu.Lock()
	f.calls++
	f.filter = filter
	f.mu.Unlock()

	if f.wait {
		<-ctx.Done()

		return nil, ctx.Err()
	}

	return f.rows, f.err
}

// testClientOptions sends requests to a fake server transport, with retry
// delays short enough not to slow the tests.
func testClientOptions(transport policy.Transporter) *arm.ClientOptions {
	return &arm.ClientOptions{ClientOptions: policy.ClientOptions{
		Transport: transport,
		Retry:     policy.RetryOptions{RetryDelay: time.Millisecond, MaxRetryDelay: time.Millisecond},
	}}
}

// newTestARMSource returns an ARMSource whose clients talk to the given SDK
// fake servers. A nil server leaves that client unset.
func newTestARMSource(t *testing.T, skus *fake.ResourceSKUsServer, usages *fake.UsageServer, graph *rgfake.Server) *ARMSource {
	t.Helper()

	cred := &azfake.TokenCredential{}
	a := &ARMSource{log: discard}

	var err error

	if skus != nil {
		a.skus, err = armcompute.NewResourceSKUsClient(testSubscriptionID, cred, testClientOptions(fake.NewResourceSKUsServerTransport(skus)))
		if err != nil {
			t.Fatalf("NewResourceSKUsClient() = %v", err)
		}
	}

	if usages != nil {
		a.usages, err = armcompute.NewUsageClient(testSubscriptionID, cred, testClientOptions(fake.NewUsageServerTransport(usages)))
		if err != nil {
			t.Fatalf("NewUsageClient() = %v", err)
		}
	}

	if graph != nil {
		a.graph, err = armresourcegraph.NewClient(cred, testClientOptions(rgfake.NewServerTransport(graph)))
		if err != nil {
			t.Fatalf("armresourcegraph.NewClient() = %v", err)
		}
	}

	return a
}

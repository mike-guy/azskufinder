package sql

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	azfake "github.com/Azure/azure-sdk-for-go/sdk/azcore/fake"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/sql/armsql/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/sql/armsql/v2/fake"

	"github.com/mike-guy/azskufinder/internal/retail"
)

const testSubscriptionID = "00000000-0000-0000-0000-000000000000"

// discard is the logger for the internal functions that take one directly.
var discard = slog.New(slog.DiscardHandler)

var deployable = availability{deployable: true}

// objective builds a deployable vCore service objective, General Purpose on
// standard-series unless an option says otherwise.
func objective(name string, vcores float64, opts ...func(*serviceObjective)) serviceObjective {
	o := serviceObjective{
		name:          name,
		skuName:       "GP_Gen5",
		edition:       "GeneralPurpose",
		family:        "Gen5",
		computeModel:  "Provisioned",
		unit:          "VCores",
		performance:   vcores,
		maxStorageGiB: new(1024.0),
		zoneRedundant: true,
		backup:        []string{"GRS", "LRS", "ZRS", "GZRS"},
		availability:  deployable,
	}

	for _, opt := range opts {
		opt(&o)
	}

	return o
}

// dtuObjective builds a deployable DTU service objective.
func dtuObjective(name, edition string, dtus float64) serviceObjective {
	return objective(name, dtus, func(o *serviceObjective) {
		o.skuName, o.edition, o.family, o.unit, o.zoneRedundant = edition, edition, "", "DTU", edition == "Premium"
	})
}

// loadPrices reads a fixture of real Retail Prices rows, trimmed from the
// uksouth price list in USD.
func loadPrices(t *testing.T, name string) []retail.Price {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}

	var rows []retail.Price
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatalf("decoding %s: %v", name, err)
	}

	return rows
}

// priceRow is a hand-made Retail Prices row.
func priceRow(region, product, sku, meter, unit string, price float64, start string) retail.Price {
	var when time.Time
	if start != "" {
		when, _ = time.Parse(time.DateOnly, start)
	}

	return retail.Price{
		ARMRegionName: region, ProductName: product, SKUName: sku, MeterName: meter,
		UnitOfMeasure: unit, RetailPrice: price, Type: "Consumption", EffectiveStartDate: when,
	}
}

// fakeARM is an armSource that returns canned data or errors.
type fakeARM struct {
	db       dbCaps
	quotas   map[string]quota
	capsErr  error
	quotaErr error
}

func (f *fakeARM) listDBCapabilities(context.Context, string) (dbCaps, error) {
	return f.db, f.capsErr
}

func (f *fakeARM) listQuota(context.Context, string) (map[string]quota, error) {
	return f.quotas, f.quotaErr
}

// fakePrices is a priceSource that returns canned rows or an error, and
// records the filter it was asked for.
type fakePrices struct {
	rows   []retail.Price
	err    error
	filter string
}

func (f *fakePrices) Query(_ context.Context, filter string) ([]retail.Price, error) {
	f.filter = filter

	return f.rows, f.err
}

func testClientOptions(transport policy.Transporter) *arm.ClientOptions {
	return &arm.ClientOptions{ClientOptions: policy.ClientOptions{
		Transport: transport,
		Retry:     policy.RetryOptions{RetryDelay: time.Millisecond, MaxRetryDelay: time.Millisecond},
	}}
}

// newTestARMSource returns an ARMSource whose clients talk to the given SDK
// fake servers. A nil server leaves that client unset.
func newTestARMSource(t *testing.T, caps *fake.CapabilitiesServer, usages *fake.SubscriptionUsagesServer) *ARMSource {
	t.Helper()

	cred := &azfake.TokenCredential{}
	a := &ARMSource{log: discard}

	var err error

	if caps != nil {
		a.caps, err = armsql.NewCapabilitiesClient(testSubscriptionID, cred, testClientOptions(fake.NewCapabilitiesServerTransport(caps)))
		if err != nil {
			t.Fatalf("NewCapabilitiesClient() = %v", err)
		}
	}

	if usages != nil {
		a.usages, err = armsql.NewSubscriptionUsagesClient(testSubscriptionID, cred, testClientOptions(fake.NewSubscriptionUsagesServerTransport(usages)))
		if err != nil {
			t.Fatalf("NewSubscriptionUsagesClient() = %v", err)
		}
	}

	return a
}

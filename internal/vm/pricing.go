package vm

import (
	"log/slog"
	"regexp"
	"strings"

	"github.com/mike-guy/azskufinder/internal/retail"
)

// HoursPerMonth is the conventional number of hours in a month used for the
// monthly price estimate.
const HoursPerMonth = 730

// pricingService is the Retail Prices API serviceName of virtual machines.
const pricingService = "Virtual Machines"

// notVMProduct matches Retail Prices products that share a SKU name with a
// virtual machine but are not one: Cloud Services roles and Dedicated Hosts.
var notVMProduct = regexp.MustCompile(`(?i)cloud ?services|dedicated ?host`)

// priceFilter is the one retail query a search makes: every virtual machine
// consumption meter in the region. Filter values are case-sensitive.
func priceFilter(location string) string {
	return "serviceName eq " + retail.Quote(pricingService) +
		" and armRegionName eq " + retail.Quote(location) +
		" and priceType eq 'Consumption'"
}

// priceEntry is the estimated hourly price of a SKU. Unknown prices are nil.
type priceEntry struct {
	onDemand *float64
	spot     *float64
}

// indexPrices builds a lookup from the Retail Prices rows, keyed by ARM SKU name.
//
// The API mixes several kinds of row under one SKU name, so rows are classified
// rather than matched by name: the meter name ends in " Spot" or " Low Priority"
// for those prices, and the product name ends in " Windows" for Windows prices.
// Low Priority is the retired predecessor of Spot and is ignored.
func indexPrices(rows []retail.Price, os OS, log *slog.Logger) map[string]priceEntry {
	wantWindows := os == OSWindows
	index := map[string]priceEntry{}

	// skipped counts the rows left out, by reason, for debug logging.
	skipped := map[string]int{}

	defer func() {
		log.Debug("indexed prices", "os", os, "rows", len(rows), "indexed", len(index), "skipped", skipped)
	}()

	for _, row := range rows {
		if row.ARMSKUName == "" || row.Type != "Consumption" || row.UnitOfMeasure != "1 Hour" {
			skipped["not-hourly"]++

			continue
		}

		if notVMProduct.MatchString(row.ProductName) {
			skipped["not-vm-product"]++

			continue
		}

		if strings.HasSuffix(row.ProductName, " Windows") != wantWindows {
			skipped["other-os"]++

			continue
		}

		entry := index[row.ARMSKUName]

		switch {
		case strings.HasSuffix(row.MeterName, " Low Priority"):
			skipped["low-priority"]++

			continue
		case strings.HasSuffix(row.MeterName, " Spot"):
			entry.spot = cheaper(entry.spot, row.RetailPrice)
		default:
			entry.onDemand = cheaper(entry.onDemand, row.RetailPrice)
		}

		index[row.ARMSKUName] = entry
	}

	return index
}

// cheaper returns the lower of the current price and next.
func cheaper(current *float64, next float64) *float64 {
	if current != nil && *current <= next {
		return current
	}

	return &next
}

// applyPrices fills in the on-demand estimates and, with includeSpot, the Spot
// block of each Spot-capable VM. A SKU without a price row keeps nil prices
// rather than zero.
func applyPrices(vms []VM, index map[string]priceEntry, includeSpot bool, log *slog.Logger) {
	for i := range vms {
		entry := index[vms[i].Name]

		// With no prices at all, the pricing warning already says why.
		if len(index) > 0 && entry.onDemand == nil {
			log.Debug("no on-demand price", "sku", vms[i].Name)
		}

		if len(index) > 0 && includeSpot && vms[i].SpotCapable && entry.spot == nil {
			log.Debug("no Spot price", "sku", vms[i].Name)
		}

		if entry.onDemand != nil {
			hourly := *entry.onDemand
			monthly := hourly * HoursPerMonth

			vms[i].Pricing = Pricing{OnDemandHourly: &hourly, OnDemandMonthly: &monthly}
		}

		if includeSpot && vms[i].SpotCapable {
			vms[i].Spot = &Spot{HourlyPrice: entry.spot}
		}
	}
}

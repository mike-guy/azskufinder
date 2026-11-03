package sql

import (
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/mike-guy/azskufinder/internal/retail"
)

// HoursPerMonth is the conventional number of hours in a month used for the
// monthly price estimate.
const HoursPerMonth = 730

const (
	// dbPricingService is the Retail Prices API serviceName of SQL Database.
	dbPricingService = "SQL Database"

	// licenceRegion is where the SQL licence meters are: they are global, not
	// regional.
	licenceRegion = "Global"

	hoursPerDay = 24
)

// priceFilter is the one retail query a search makes: every consumption meter
// of the service in the region, and the global meters, which include the SQL
// licence. Filter values are case-sensitive.
func priceFilter(service, location string) string {
	return "serviceName eq " + retail.Quote(service) +
		" and (armRegionName eq " + retail.Quote(location) + " or armRegionName eq " + retail.Quote(licenceRegion) + ")" +
		" and priceType eq 'Consumption'"
}

// vcoreMeter identifies a per-vCore price: the tier, the hardware, and
// whether it is the serverless rate.
type vcoreMeter struct {
	tier       Tier
	hardware   Hardware
	serverless bool
}

// meterPrice is an hourly price and the date it took effect.
type meterPrice struct {
	hourly float64
	start  time.Time
}

// priceIndex holds the prices a search needs, all per hour.
type priceIndex struct {
	// compute is the price of one vCore, without the SQL licence, except for
	// serverless, whose rate includes it.
	compute map[vcoreMeter]meterPrice

	// zoneRedundancy is the surcharge for one zone-redundant vCore.
	zoneRedundancy map[vcoreMeter]meterPrice

	// licence is the SQL licence for one vCore.
	licence map[Tier]meterPrice

	// dtus is the price of a DTU objective, keyed by the name the price list
	// gives it, such as S3 or B. It includes the licence.
	dtus map[string]meterPrice
}

// productHardware maps the end of a vCore compute product name to its
// hardware. Gen4, Fsv2 and M-series products are not listed, so they are left
// out.
var productHardware = []struct {
	suffix   string
	hardware Hardware
}{
	{" - Compute Gen5", HardwareStandard},
	{" - Premium Series Memory Optimized Compute", HardwarePremiumMemoryOp}, //nolint:misspell // Azure's product name
	{" - Premium Series Compute", HardwarePremium},
	{" - Compute DC-Series", HardwareDC},
}

// dtuProducts are the single-database DTU products. Elastic pool and Premium
// RS products are not single databases.
var dtuProducts = []string{
	"SQL Database Single Basic",
	"SQL Database Single Standard",
	"SQL Database Single Premium",
}

// indexPrices builds the lookups from the Retail Prices rows.
//
// The API's armSkuName is unreliable for SQL, so rows are classified by product
// and meter instead. The licence is global; everything else must be in the
// location. A meter can be listed more than once, at old and new prices, so
// the row that took effect last wins, and of rows that took effect together,
// the cheapest.
func indexPrices(rows []retail.Price, location string, log *slog.Logger) priceIndex {
	idx := priceIndex{
		compute:        map[vcoreMeter]meterPrice{},
		zoneRedundancy: map[vcoreMeter]meterPrice{},
		licence:        map[Tier]meterPrice{},
		dtus:           map[string]meterPrice{},
	}

	// skipped counts the rows left out, by reason, for debug logging.
	skipped := map[string]int{}

	defer func() {
		log.Debug("indexed prices", "rows", len(rows), "compute", len(idx.compute), "zone_redundancy", len(idx.zoneRedundancy),
			"licence", len(idx.licence), "dtu", len(idx.dtus), "skipped", skipped)
	}()

	for _, row := range rows {
		if reason := idx.add(row, location); reason != "" {
			skipped[reason]++
		}
	}

	return idx
}

// add indexes one row, or returns why it was left out.
func (p priceIndex) add(row retail.Price, location string) string {
	if row.Type != "Consumption" {
		return "not-consumption"
	}

	product := row.ProductName

	switch {
	case row.ARMRegionName == licenceRegion:
		tier, ok := productTier(product)
		if !ok || !strings.HasSuffix(product, " - SQL License") || row.MeterName != "vCore" || row.UnitOfMeasure != "1 Hour" {
			return "global-not-licence"
		}

		p.licence[tier] = later(p.licence[tier], row, row.RetailPrice)
	case !strings.EqualFold(row.ARMRegionName, location):
		return "other-region"
	case isDTUProduct(product):
		// Rows such as "S3 Secondary" are for geo-replicas.
		if row.UnitOfMeasure != "1/Day" || strings.Contains(row.SKUName, " ") {
			return "not-dtu-sku"
		}

		p.dtus[row.SKUName] = later(p.dtus[row.SKUName], row, row.RetailPrice/hoursPerDay)
	default:
		return p.addVCore(row)
	}

	return ""
}

// addVCore indexes a per-vCore compute or zone redundancy row.
func (p priceIndex) addVCore(row retail.Price) string {
	product := row.ProductName

	if row.UnitOfMeasure != "1 Hour" || strings.Contains(product, "Read Repl") {
		return "not-vcore-hour"
	}

	tier, ok := productTier(product)
	if !ok {
		return "other-product"
	}

	hardware, ok := productHardwareOf(product)
	if !ok {
		return "other-hardware"
	}

	key := vcoreMeter{tier: tier, hardware: hardware, serverless: strings.Contains(product, "Serverless")}

	perVCore := row.SKUName == "vCore" || row.SKUName == "1 vCore"

	switch {
	case row.MeterName == "Zone Redundancy vCore" && (row.SKUName == "vCore ZR Zone Redundancy" || row.SKUName == "1 vCore Zone Redundancy"):
		p.zoneRedundancy[key] = later(p.zoneRedundancy[key], row, row.RetailPrice)
	case perVCore && (row.MeterName == "vCore" || row.MeterName == "1 vCore"):
		p.compute[key] = later(p.compute[key], row, row.RetailPrice)
	default:
		// Rows for several vCores at once, free offers and add-ons.
		return "not-per-vcore"
	}

	return ""
}

// later returns whichever of the current price and the row took effect last,
// or the cheaper when they took effect together. A zero current means none yet.
func later(current meterPrice, row retail.Price, hourly float64) meterPrice {
	next := meterPrice{hourly: hourly, start: row.EffectiveStartDate}

	switch {
	case current.start.IsZero() && current.hourly == 0:
		return next
	case next.start.After(current.start):
		return next
	case next.start.Equal(current.start) && next.hourly < current.hourly:
		return next
	default:
		return current
	}
}

func productTier(product string) (Tier, bool) {
	switch {
	case strings.Contains(product, "General Purpose"):
		return TierGeneralPurpose, true
	case strings.Contains(product, "Business Critical"):
		return TierBusinessCritical, true
	case strings.Contains(product, "Hyperscale"):
		return TierHyperscale, true
	default:
		return "", false
	}
}

func productHardwareOf(product string) (Hardware, bool) {
	for _, p := range productHardware {
		if strings.HasSuffix(product, p.suffix) {
			return p.hardware, true
		}
	}

	return "", false
}

func isDTUProduct(product string) bool {
	return slices.Contains(dtuProducts, product)
}

// dtuMeterName is the price list's name for a DTU objective: the same, except
// that Basic is B.
func dtuMeterName(objective string) string {
	if objective == "Basic" {
		return "B"
	}

	return objective
}

// vcore prices a vCore database. The total is unknown when any part of it is.
func (p priceIndex) vcore(d VCoreDatabase, licence Licence, zoneRedundant bool) Pricing {
	serverless := d.Compute == ComputeServerless

	// Hyperscale has no licence fee, and the serverless rate includes it.
	withLicence := licence == LicenceIncluded && !serverless && d.Tier != TierHyperscale

	return p.vcores(vcoreMeter{tier: d.Tier, hardware: d.Hardware, serverless: serverless}, d.VCores, withLicence, zoneRedundant)
}

// dtu prices a DTU database. The price includes the SQL licence.
func (p priceIndex) dtu(d DTUDatabase) DTUPricing {
	m, ok := p.dtus[dtuMeterName(d.Name)]
	if !ok {
		return DTUPricing{}
	}

	hourly := m.hourly
	monthly := hourly * HoursPerMonth

	return DTUPricing{Hourly: &hourly, Monthly: &monthly}
}

// vcores prices n vCores of a meter, with the licence and zone redundancy if
// asked for. Zone redundancy adds a surcharge only where Azure publishes one:
// for Business Critical and Premium it is free.
func (p priceIndex) vcores(key vcoreMeter, n int, withLicence, zoneRedundant bool) Pricing {
	rate, ok := p.compute[key]
	if !ok {
		return Pricing{}
	}

	count := float64(n)
	compute := rate.hourly * count

	var licenceHourly, zoneHourly *float64

	if withLicence {
		l, ok := p.licence[key.tier]
		if !ok {
			return Pricing{ComputeHourly: &compute}
		}

		licenceHourly = new(l.hourly * count)
	}

	if z, ok := p.zoneRedundancy[key]; ok && zoneRedundant {
		zoneHourly = new(z.hourly * count)
	}

	return total(compute, licenceHourly, zoneHourly)
}

// total adds up the parts of a price.
func total(compute float64, licence, zone *float64) Pricing {
	hourly := compute
	if licence != nil {
		hourly += *licence
	}

	if zone != nil {
		hourly += *zone
	}

	monthly := hourly * HoursPerMonth

	return Pricing{
		Hourly:               &hourly,
		Monthly:              &monthly,
		ComputeHourly:        &compute,
		LicenceHourly:        licence,
		ZoneRedundancyHourly: zone,
	}
}

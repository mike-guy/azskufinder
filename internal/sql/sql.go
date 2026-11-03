// Package sql finds the Azure SQL Database service objectives that a
// subscription can deploy, by purchasing model: vCore or DTU.
//
// It owns everything SQL-specific: the result types, the filters, the
// filtering that narrows the objectives down, retail price matching and the
// ARM adapter. It takes plain inputs and returns plain data; presentation is
// left to the caller.
package sql

// VCoreDatabase is one deployable vCore service objective.
type VCoreDatabase struct {
	// Name is the service objective, such as GP_Gen5_4 or GP_S_Gen5_2.
	Name string `json:"name"`

	// SKU is the SKU name a deployment sets, such as GP_Gen5 or GP_S_Gen5.
	SKU string `json:"sku"`

	Tier     Tier     `json:"tier"`
	Compute  Compute  `json:"compute"`
	Hardware Hardware `json:"hardware"`

	// Family is the hardware family as Azure names it, such as Gen5 or 8IM.
	Family string `json:"family"`

	// VCores is the vCore count; for serverless, the most it can scale to.
	VCores int `json:"vcores"`

	// MemoryGiB is nil for serverless, whose memory scales with the workload,
	// and for hardware this package has no memory figure for.
	MemoryGiB *float64 `json:"memoryGiB"`

	// MaxStorageGiB is the largest data size the objective allows, or nil when
	// Azure did not say.
	MaxStorageGiB *float64 `json:"maxStorageGiB"`

	// ZoneRedundant is whether the objective can be made zone redundant.
	ZoneRedundant bool `json:"zoneRedundant"`

	// BackupRedundancy are the backup storage redundancies the tier offers.
	// Never nil.
	BackupRedundancy []BackupRedundancy `json:"backupRedundancy"`

	// QuotaNeeded is how much of the regional vCore quota one database uses.
	QuotaNeeded int64           `json:"quotaNeeded"`
	Quota       []QuotaHeadroom `json:"quota"`
	Pricing     Pricing         `json:"pricing"`
}

// DTUDatabase is one deployable DTU service objective.
type DTUDatabase struct {
	// Name is the service objective, such as S3 or Basic.
	Name string `json:"name"`
	Tier Tier   `json:"tier"`
	DTUs int    `json:"dtus"`

	// MaxStorageGiB is the largest data size the objective allows, or nil when
	// Azure did not say.
	MaxStorageGiB *float64 `json:"maxStorageGiB"`

	// ZoneRedundant is whether the objective can be made zone redundant.
	ZoneRedundant bool `json:"zoneRedundant"`

	// BackupRedundancy are the backup storage redundancies the tier offers.
	// Never nil.
	BackupRedundancy []BackupRedundancy `json:"backupRedundancy"`

	// QuotaNeeded is how much of the regional vCore quota one database uses:
	// one vCore for every 100 DTUs, rounded up.
	QuotaNeeded int64           `json:"quotaNeeded"`
	Quota       []QuotaHeadroom `json:"quota"`
	Pricing     DTUPricing      `json:"pricing"`
}

// QuotaHeadroom is how much of one quota is left.
type QuotaHeadroom struct {
	Name  string `json:"name"`
	Limit int64  `json:"limit"`
	Used  int64  `json:"used"`
	Free  int64  `json:"free"`
}

// Pricing holds the pay-as-you-go estimate of a vCore objective. Unknown
// prices are nil, never zero, so that a missing price cannot look like a free
// one.
type Pricing struct {
	// Hourly and Monthly are the totals: compute, plus the licence and zone
	// redundancy when they are charged separately.
	Hourly  *float64 `json:"hourly"`
	Monthly *float64 `json:"monthly"`

	// ComputeHourly is the compute price. For serverless it includes the SQL
	// licence.
	ComputeHourly *float64 `json:"computeHourly"`

	// LicenceHourly is the SQL licence, or nil when none is charged separately:
	// with Azure Hybrid Benefit, for Hyperscale and for serverless.
	LicenceHourly *float64 `json:"licenceHourly"`

	// ZoneRedundancyHourly is the zone redundancy surcharge, or nil when zone
	// redundancy was not asked for or Azure publishes no surcharge for it.
	ZoneRedundancyHourly *float64 `json:"zoneRedundancyHourly"`
}

// DTUPricing holds the pay-as-you-go estimate of a DTU objective, which
// includes the SQL licence. Unknown prices are nil.
type DTUPricing struct {
	Hourly  *float64 `json:"hourly"`
	Monthly *float64 `json:"monthly"`
}

// DBResult is the outcome of FindVCore or FindDTU. Databases and Warnings are
// never nil.
type DBResult[T any] struct {
	Location string  `json:"location"`
	Summary  Summary `json:"summary"`

	// ServerQuota is the logical server quota in the location, when Azure
	// reported it. A database needs a server, but an existing one will do, so
	// it is shown rather than used to filter.
	ServerQuota *QuotaHeadroom `json:"serverQuota"`

	// Databases are the objectives that can be deployed, in the order asked for.
	Databases []T       `json:"results"`
	Warnings  []Warning `json:"warnings"`
}

// Summary counts what was considered and what was left out, so that an empty
// result can be explained.
type Summary struct {
	// Considered is the number of objectives of the purchasing model offered in
	// the location.
	Considered int `json:"considered"`

	// Matched is how many of those meet the requirements.
	Matched int `json:"matched"`

	// Restricted is how many matching objectives Azure does not allow this
	// subscription to deploy.
	Restricted int `json:"restricted"`

	// NoQuota is how many matching, unrestricted objectives lack the quota for
	// one more.
	NoQuota int `json:"noQuota"`

	// Eligible is how many matching objectives can be deployed.
	Eligible int `json:"eligible"`

	// Shown is how many are in the results, after the limit.
	Shown int `json:"shown"`
}

// Warning is a problem that did not stop the search but may affect the results.
type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Warning codes.
const (
	WarningPricingUnavailable   = "pricing-unavailable"
	WarningQuotaMissing         = "quota-missing"
	WarningLocationUnavailable  = "location-unavailable"
	WarningServerQuotaExhausted = "server-quota-exhausted"
)

// MinQuotaFree returns the smallest amount of quota left across the limits
// that apply, and whether any quota information was found.
func (d VCoreDatabase) MinQuotaFree() (int64, bool) { return minFree(d.Quota) }

// MinQuotaFree returns the smallest amount of quota left across the limits
// that apply, and whether any quota information was found.
func (d DTUDatabase) MinQuotaFree() (int64, bool) { return minFree(d.Quota) }

func minFree(quota []QuotaHeadroom) (int64, bool) {
	if len(quota) == 0 {
		return 0, false
	}

	least := quota[0].Free
	for _, q := range quota[1:] {
		least = min(least, q.Free)
	}

	return least, true
}

// Package vm finds Azure virtual machine SKUs that a subscription can deploy.
//
// It owns everything VM-specific: the result types, the filter, the filtering
// that narrows SKUs down, retail price matching and the ARM adapter. It takes
// plain inputs and returns plain data; presentation is left to the caller.
package vm

// VM is one deployable virtual machine SKU in the results.
type VM struct {
	Name   string `json:"name"`
	Family string `json:"family"`

	// VCPUs is the vCPU count Azure bills and counts against quota.
	VCPUs int `json:"vcpus"`

	// VCPUsAvailable is the vCPU count the guest sees. It is lower than VCPUs for
	// constrained-core SKUs such as Standard_E8-4s_v5.
	VCPUsAvailable int      `json:"vcpusAvailable"`
	MemoryGiB      float64  `json:"memoryGiB"`
	GPUs           int      `json:"gpus"`
	Arch           Arch     `json:"architecture"` // x64, arm64 or unknown
	Category       Category `json:"category"`     // the most specific category, or other
	Zones          []string `json:"zones"`

	// DiskControllers are the disk controllers the SKU supports, in a fixed
	// order (scsi, then nvme). Never nil; empty when Azure reports only
	// controllers this package does not know.
	DiskControllers []DiskController `json:"diskControllers"`

	SpotCapable bool `json:"spotCapable"`

	// SpotOnly is true when there is not enough on-demand quota and the SKU was
	// kept because it fits the regional Spot quota. Only set with IncludeSpot.
	SpotOnly bool `json:"spotOnly"`

	Quota   []QuotaHeadroom `json:"quota"`
	Pricing Pricing         `json:"pricing"`

	// Spot is set only when IncludeSpot was asked for and the SKU is Spot
	// capable.
	Spot *Spot `json:"spot,omitempty"`

	// categories are all the categories the SKU belongs to, used for matching.
	// Category is the one shown.
	categories []Category
}

// QuotaHeadroom is how much of one quota is left.
type QuotaHeadroom struct {
	Name  string `json:"name"`
	Limit int64  `json:"limit"`
	Used  int64  `json:"used"`
	Free  int64  `json:"free"`
}

// Pricing holds the pay-as-you-go estimate for a SKU. Unknown prices are nil,
// never zero, so that a missing price cannot look like a free one.
type Pricing struct {
	OnDemandHourly  *float64 `json:"onDemandHourly"`
	OnDemandMonthly *float64 `json:"onDemandMonthly"`
}

// Spot is the Spot information for a Spot-capable SKU, present when it was
// asked for.
type Spot struct {
	HourlyPrice *float64 `json:"hourlyPrice"`

	// EvictionRate is the band of the hourly chance of eviction over the last 28
	// days, in percent, for example "0-5". Nil when it was not available.
	EvictionRate *string `json:"evictionRate"`
}

// MinQuotaFree returns the smallest amount of quota left across the limits that
// apply to the SKU, and whether any quota information was found.
func (v VM) MinQuotaFree() (int64, bool) {
	if len(v.Quota) == 0 {
		return 0, false
	}

	least := v.Quota[0].Free
	for _, q := range v.Quota[1:] {
		least = min(least, q.Free)
	}

	return least, true
}

// Result is the outcome of Find. VMs and Warnings are never nil.
type Result struct {
	Location string  `json:"location"`
	Summary  Summary `json:"summary"`

	// SpotQuota is the regional Spot vCPU quota, when IncludeSpot was asked for
	// and Azure reported it.
	SpotQuota *QuotaHeadroom `json:"spotQuota,omitempty"`

	// VMs are the SKUs that can be deployed, in the order asked for.
	VMs      []VM      `json:"results"`
	Warnings []Warning `json:"warnings"`
}

// Summary counts what was considered and what was left out, so that an empty
// result can be explained.
type Summary struct {
	// Considered is the number of virtual machine SKUs offered in the location.
	Considered int `json:"considered"`

	// Matched is how many of those meet the requirements.
	Matched int `json:"matched"`

	// Restricted is how many matching SKUs Azure restricts for this subscription.
	Restricted int `json:"restricted"`

	// NoQuota is how many matching, unrestricted SKUs lack the quota for one instance.
	NoQuota int `json:"noQuota"`

	// Eligible is how many matching SKUs can be deployed.
	Eligible int `json:"eligible"`

	// SpotOnly is how many eligible SKUs can be deployed only as Spot.
	SpotOnly int `json:"spotOnly"`

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
	WarningPricingUnavailable = "pricing-unavailable"
	WarningSpotUnavailable    = "spot-unavailable"
	WarningQuotaMissing       = "quota-missing"
)

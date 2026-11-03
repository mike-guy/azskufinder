package sql

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/sql/armsql/v2"

	"github.com/mike-guy/azskufinder/internal/azure"
)

// ARMSource reads the SQL Database capabilities and quota of one subscription. It is the
// only code that sees ARM SDK types: it flattens the capability tree into the
// package's own small structs straight away.
type ARMSource struct {
	caps   *armsql.CapabilitiesClient
	usages *armsql.SubscriptionUsagesClient
	log    *slog.Logger
}

// NewARMSource checks the subscription ID and creates the SDK clients. opts
// are the shared Azure client options. A nil logger discards debug output.
func NewARMSource(subscriptionID string, cred azcore.TokenCredential, opts policy.ClientOptions, log *slog.Logger) (*ARMSource, error) {
	if !azure.ValidSubscriptionID(subscriptionID) {
		return nil, fmt.Errorf("%q is not a valid subscription ID", subscriptionID)
	}

	armOpts := &arm.ClientOptions{ClientOptions: opts}

	caps, err := armsql.NewCapabilitiesClient(subscriptionID, cred, armOpts)
	if err != nil {
		return nil, fmt.Errorf("creating SQL capabilities client: %w", err)
	}

	usages, err := armsql.NewSubscriptionUsagesClient(subscriptionID, cred, armOpts)
	if err != nil {
		return nil, fmt.Errorf("creating SQL usage client: %w", err)
	}

	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	return &ARMSource{caps: caps, usages: usages, log: log.With("component", "sql.arm")}, nil
}

// The types the adapter returns: the contract between arm.go, which produces
// them, and the filtering in filter.go, which consumes them.

// availability is whether the subscription can deploy an option, and if not,
// why. It is the most restrictive status on the option's path through the
// capability tree.
type availability struct {
	deployable bool
	reason     string
}

// dbCaps are the single-database service objectives offered in a location.
type dbCaps struct {
	location   availability
	objectives []serviceObjective
}

// serviceObjective is one single-database service objective, with everything
// it inherits from its edition already resolved.
type serviceObjective struct {
	name         string // GP_Gen5_4
	skuName      string // GP_Gen5
	edition      string // GeneralPurpose
	family       string // Gen5
	computeModel string // Provisioned or Serverless
	unit         string // VCores or DTU
	performance  float64

	maxStorageGiB *float64
	zoneRedundant bool
	backup        []string // the edition's backup storage types: LRS, ZRS, GRS, GZRS
	availability  availability
}

// quota is one quota entry: how much is used out of what is allowed.
type quota struct {
	Used  int64
	Limit int64
}

// listDBCapabilities returns the single-database service objectives offered to
// the subscription in location.
func (a *ARMSource) listDBCapabilities(ctx context.Context, location string) (dbCaps, error) {
	resp, err := a.caps.ListByLocation(ctx, location, &armsql.CapabilitiesClientListByLocationOptions{
		Include: new(armsql.CapabilityGroupSupportedEditions),
	})
	if err != nil {
		return dbCaps{}, fmt.Errorf("listing SQL Database capabilities: %w", err)
	}

	caps := convertDBCaps(resp.LocationCapabilities)

	a.log.Debug("listed SQL Database capabilities", "location", location, "skus", len(caps.objectives),
		"location_deployable", caps.location.deployable)

	return caps, nil
}

// listQuota returns the SQL quota of location, from the subscription usages,
// keyed by name, such as "RegionalVCoreQuotaForSQLDBAndDW".
func (a *ARMSource) listQuota(ctx context.Context, location string) (map[string]quota, error) {
	pager := a.usages.NewListByLocationPager(location, nil)

	quotas := map[string]quota{}
	pages, incomplete := 0, 0

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing SQL usage: %w", err)
		}

		pages++

		for _, u := range page.Value {
			if u == nil || u.Name == nil || u.Properties == nil || u.Properties.CurrentValue == nil || u.Properties.Limit == nil {
				incomplete++

				continue
			}

			quotas[*u.Name] = quota{Used: int64(*u.Properties.CurrentValue), Limit: int64(*u.Properties.Limit)}
		}
	}

	a.log.Debug("listed SQL usage", "location", location, "pages", pages, "entries", len(quotas), "incomplete", incomplete)

	return quotas, nil
}

// convertDBCaps flattens the capability tree: location, server version,
// edition, service objective. An objective listed under more than one server
// version is kept once, preferring a deployable one.
func convertDBCaps(lc armsql.LocationCapabilities) dbCaps {
	location := availabilityOf("location", deref(lc.Name), lc.Status, lc.Reason)

	caps := dbCaps{
		location:   location,
		objectives: []serviceObjective{},
	}

	for _, version := range lc.SupportedServerVersions {
		if version == nil {
			continue
		}

		versionAvail := location.and(availabilityOf("server version", deref(version.Name), version.Status, version.Reason))

		for _, edition := range version.SupportedEditions {
			if edition == nil {
				continue
			}

			editionAvail := versionAvail.and(availabilityOf("edition", deref(edition.Name), edition.Status, edition.Reason))
			backup := storageTypes(edition.SupportedStorageCapabilities)

			for _, slo := range edition.SupportedServiceLevelObjectives {
				if slo == nil {
					continue
				}

				o := serviceObjective{
					name:          deref(slo.Name),
					edition:       deref(edition.Name),
					computeModel:  deref(slo.ComputeModel),
					maxStorageGiB: largestSize(slo.SupportedMaxSizes, slo.IncludedMaxSize),
					zoneRedundant: slo.ZoneRedundant != nil && *slo.ZoneRedundant,
					backup:        backup,
					availability:  editionAvail.and(availabilityOf("SKU", deref(slo.Name), slo.Status, slo.Reason)),
				}

				if slo.SKU != nil {
					o.skuName = deref(slo.SKU.Name)
					o.family = deref(slo.SKU.Family)
				}

				if slo.PerformanceLevel != nil {
					o.unit = string(deref(slo.PerformanceLevel.Unit))

					if slo.PerformanceLevel.Value != nil {
						o.performance = *slo.PerformanceLevel.Value
					}
				}

				caps.objectives = keepBest(caps.objectives, o, func(x serviceObjective) bool { return x.name == o.name },
					func(x serviceObjective) bool { return x.availability.deployable })
			}
		}
	}

	return caps
}

// availabilityOf reads one level's status. Available and Default can be
// deployed. Disabled cannot, and neither can Visible, which is shown but not
// offered. A missing status restricts nothing.
func availabilityOf(level, name string, status *armsql.CapabilityStatus, reason *string) availability {
	if status == nil {
		return availability{deployable: true}
	}

	switch *status {
	case armsql.CapabilityStatusAvailable, armsql.CapabilityStatusDefault:
		return availability{deployable: true}
	case armsql.CapabilityStatusDisabled, armsql.CapabilityStatusVisible:
	}

	why := fmt.Sprintf("%s %s is %s", level, name, *status)
	if r := deref(reason); r != "" {
		why += ": " + r
	}

	return availability{reason: why}
}

// and combines a parent's availability with a child's. The parent's reason
// wins, as it explains the most.
func (a availability) and(child availability) availability {
	if !a.deployable {
		return a
	}

	return child
}

// keepBest appends o unless an equivalent is already there. A deployable o
// replaces an equivalent that is not.
func keepBest[T any](list []T, o T, same func(T) bool, deployable func(T) bool) []T {
	i := slices.IndexFunc(list, same)

	switch {
	case i < 0:
		return append(list, o)
	case !deployable(list[i]) && deployable(o):
		list[i] = o
	}

	return list
}

// storageTypes returns the backup storage types that can be chosen.
func storageTypes(caps []*armsql.StorageCapability) []string {
	out := []string{}

	for _, c := range caps {
		if c == nil || c.StorageAccountType == nil || !availabilityOf("", "", c.Status, nil).deployable {
			continue
		}

		out = append(out, string(*c.StorageAccountType))
	}

	return out
}

// largestSize returns the largest data size that can be chosen, in GiB, across
// the ranges that are not disabled, or the included size when no range is
// given, or nil.
func largestSize(ranges []*armsql.MaxSizeRangeCapability, included *armsql.MaxSizeCapability) *float64 {
	var largest *float64

	for _, r := range ranges {
		if r == nil || !availabilityOf("", "", r.Status, nil).deployable {
			continue
		}

		if hi := gibOf(r.MaxValue); hi != nil && (largest == nil || *hi > *largest) {
			largest = hi
		}
	}

	if largest != nil {
		return largest
	}

	return gibOf(included)
}

// gibOf converts a size to GiB. Azure's gigabytes for SQL sizes are binary: a
// 1 TB limit is 1024 GB. It returns nil for a missing size or unknown unit.
func gibOf(s *armsql.MaxSizeCapability) *float64 {
	if s == nil || s.Limit == nil || s.Unit == nil {
		return nil
	}

	factor := map[armsql.MaxSizeUnit]float64{
		armsql.MaxSizeUnitMegabytes: 1.0 / 1024,
		armsql.MaxSizeUnitGigabytes: 1,
		armsql.MaxSizeUnitTerabytes: 1024,
		armsql.MaxSizeUnitPetabytes: 1024 * 1024,
	}[*s.Unit]

	if factor == 0 {
		return nil
	}

	return new(float64(*s.Limit) * factor)
}

// deref returns the value p points to, or "" for nil. T is string or one of the
// string-based enum types the SDK uses, which keeps its type.
func deref[T ~string](p *T) T {
	if p == nil {
		return ""
	}

	return *p
}

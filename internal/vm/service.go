package vm

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/mike-guy/azskufinder/internal/azure"
	"github.com/mike-guy/azskufinder/internal/retail"
)

// The consumer-side interfaces, one for each API the service reads from.
// *ARMSource satisfies armSource; *retail.Client satisfies priceSource.

// armSource is the data read from Azure Resource Manager.
type armSource interface {
	listSKUs(ctx context.Context, location string) ([]sku, error)
	listQuota(ctx context.Context, location string) (map[string]quota, error)
	listEvictionRates(ctx context.Context, location string, skus []string) (map[string]string, error)
}

// priceSource is the Retail Prices API.
type priceSource interface {
	Query(ctx context.Context, filter string) ([]retail.Price, error)
}

// The types the sources return: the contract between arm.go, which produces
// them, and the filtering in filter.go, which consumes them.

// sku is a virtual machine SKU as the adapter hands it over: SDK pointers
// resolved, and only what applies to the requested location.
type sku struct {
	Name         string
	Family       string
	Capabilities map[string]string

	// Zones are the availability zones that offer the SKU in the location,
	// before any restrictions are applied.
	Zones []string

	// LocationRestricted means the subscription cannot use the SKU in the
	// location at all.
	LocationRestricted bool

	// RestrictedZones are the zones the subscription cannot use the SKU in.
	RestrictedZones []string
}

// quota is one quota entry: how much is used out of what is allowed.
type quota struct {
	Used  int64
	Limit int64
}

// Service finds deployable virtual machine SKUs.
type Service struct {
	arm    armSource
	prices priceSource
	log    *slog.Logger
}

// NewService returns a Service that reads from the given sources. A nil logger
// discards debug output.
func NewService(arm armSource, prices priceSource, log *slog.Logger) *Service {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	return &Service{arm: arm, prices: prices, log: log.With("component", "vm")}
}

// Find returns the virtual machine SKUs in location that meet f and that the
// subscription can deploy. Failing to read SKUs or quota is an error; failing
// to read prices or Spot eviction rates is a warning in the result.
func (s *Service) Find(ctx context.Context, location string, f Filter) (Result, error) {
	// The location goes into an OData filter and a Kusto query, so it is
	// checked once here for every source.
	if !azure.ValidLocation(location) {
		return Result{}, fmt.Errorf("%q is not a valid location name", location)
	}

	if f.Sort.NeedsSpot() && !f.IncludeSpot {
		return Result{}, fmt.Errorf("sorting by %s needs Spot data, so Spot must be asked for", f.Sort.Key)
	}

	// Read SKUs, quota and prices at the same time. SKUs and quota are
	// required, so their errors cancel the others and fail the search. Prices
	// only improve the answer, so their error becomes a warning instead. Each
	// goroutine writes only its own variables, which are read after Wait
	// returns, so no locking is needed.
	var (
		skus      []sku
		quotas    map[string]quota
		rows      []retail.Price
		pricesErr error
	)

	start := time.Now()

	s.log.Debug("fetching SKUs, quota and prices", "location", location)

	group, gctx := errgroup.WithContext(ctx)

	group.Go(func() (err error) {
		defer s.logFetch("SKUs", time.Now(), func() int { return len(skus) }, &err)

		skus, err = s.arm.listSKUs(gctx, location)

		return err
	})
	group.Go(func() (err error) {
		defer s.logFetch("quota", time.Now(), func() int { return len(quotas) }, &err)

		quotas, err = s.arm.listQuota(gctx, location)

		return err
	})
	group.Go(func() error {
		defer s.logFetch("prices", time.Now(), func() int { return len(rows) }, &pricesErr)

		rows, pricesErr = s.prices.Query(gctx, priceFilter(location))

		return nil
	})

	if err := group.Wait(); err != nil {
		return Result{}, err
	}

	// A cancelled or timed-out run must fail, not become a pricing warning.
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	res := Result{Location: location, Warnings: []Warning{}}

	if pricesErr != nil {
		s.log.Debug("prices unavailable, continuing without them", "error", pricesErr)

		res.Warnings = append(res.Warnings, Warning{
			Code:    WarningPricingUnavailable,
			Message: "prices could not be fetched, so none are shown: " + azure.ErrorText(pricesErr),
		})
	}

	if q, ok := quotas[spotCores]; ok && f.IncludeSpot {
		res.SpotQuota = &QuotaHeadroom{Name: spotCores, Limit: q.Limit, Used: q.Used, Free: q.Limit - q.Used}
	}

	vms, warnings := assess(skus, quotas, indexPrices(rows, f.OS, s.log), f, &res.Summary, s.log)
	res.Warnings = append(res.Warnings, warnings...)

	// Eviction rates are looked up for the shortlist only, unless the order
	// depends on them: then every eligible VM needs one before the limit.
	spotFirst := f.IncludeSpot && f.Sort.NeedsSpot()
	if spotFirst {
		res.Warnings = append(res.Warnings, s.applyEvictions(ctx, location, vms)...)
	}

	vms = rank(vms, f, &res.Summary, s.log)

	if f.IncludeSpot && !spotFirst {
		res.Warnings = append(res.Warnings, s.applyEvictions(ctx, location, vms)...)
	}

	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	res.VMs = vms

	s.log.Debug("search complete",
		"considered", res.Summary.Considered,
		"matched", res.Summary.Matched,
		"restricted", res.Summary.Restricted,
		"no_quota", res.Summary.NoQuota,
		"spot_only", res.Summary.SpotOnly,
		"eligible", res.Summary.Eligible,
		"shown", res.Summary.Shown,
		"warnings", len(res.Warnings),
		"duration", time.Since(start),
	)

	return res, nil
}

// logFetch logs the outcome of one of the concurrent fetches. It is deferred,
// so it takes the count and error by reference.
func (s *Service) logFetch(what string, start time.Time, count func() int, err *error) {
	if *err != nil {
		s.log.Debug("fetch failed", "what", what, "duration", time.Since(start), "error", *err)

		return
	}

	s.log.Debug("fetched", "what", what, "count", count(), "duration", time.Since(start))
}

// applyEvictions sets the Spot eviction rate of each VM with Spot information.
// A failure to read them is returned as a warning.
func (s *Service) applyEvictions(ctx context.Context, location string, vms []VM) []Warning {
	var names []string

	for _, v := range vms {
		if v.Spot != nil {
			names = append(names, v.Name)
		}
	}

	if len(names) == 0 {
		s.log.Debug("no Spot-capable VMs, so no eviction rates to fetch")

		return nil
	}

	start := time.Now()

	s.log.Debug("fetching eviction rates", "count", len(names))

	rates, err := s.arm.listEvictionRates(ctx, location, names)
	if err != nil {
		s.log.Debug("eviction rates unavailable, continuing without them", "duration", time.Since(start), "error", err)

		return []Warning{{
			Code:    WarningSpotUnavailable,
			Message: "Spot eviction rates could not be fetched, so none are shown: " + azure.ErrorText(err),
		}}
	}

	missing := 0

	for i := range vms {
		if vms[i].Spot != nil {
			rate, ok := rates[strings.ToLower(vms[i].Name)]
			if !ok {
				missing++

				s.log.Debug("no eviction rate", "sku", vms[i].Name)

				continue
			}

			vms[i].Spot.EvictionRate = &rate
		}
	}

	s.log.Debug("fetched eviction rates", "found", len(names)-missing, "missing", missing, "duration", time.Since(start))

	return nil
}

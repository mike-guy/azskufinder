package sql

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/mike-guy/azskufinder/internal/azure"
	"github.com/mike-guy/azskufinder/internal/retail"
)

// The consumer-side interfaces, one for each API the service reads from.
// *ARMSource satisfies armSource; *retail.Client satisfies priceSource.

// armSource is the data read from Azure Resource Manager.
type armSource interface {
	listDBCapabilities(ctx context.Context, location string) (dbCaps, error)
	listQuota(ctx context.Context, location string) (map[string]quota, error)
}

// priceSource is the Retail Prices API.
type priceSource interface {
	Query(ctx context.Context, filter string) ([]retail.Price, error)
}

// Service finds deployable SQL Database service objectives.
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

	return &Service{arm: arm, prices: prices, log: log.With("component", "sql")}
}

// FindVCore returns the vCore service objectives in location that meet f and
// that the subscription can deploy. Failing to read capabilities or quota is
// an error; failing to read prices is a warning in the result.
func (s *Service) FindVCore(ctx context.Context, location string, f VCoreFilter) (DBResult[VCoreDatabase], error) {
	return findDB(ctx, s, location, func(prices priceIndex) model[VCoreDatabase] {
		return model[VCoreDatabase]{
			parse:   parseVCore,
			matches: func(d VCoreDatabase) bool { return matchesVCore(d, f) },
			quota:   func(d *VCoreDatabase) (int64, *[]QuotaHeadroom) { return d.QuotaNeeded, &d.Quota },
			name:    func(d VCoreDatabase) string { return d.Name },
			price:   func(d *VCoreDatabase) { d.Pricing = prices.vcore(*d, f.Licence, f.ZoneRedundant) },
		}
	}, f.Sort, vcoreSortColumns, vcoreTieBreak, f.Limit)
}

// FindDTU returns the DTU service objectives in location that meet f and that
// the subscription can deploy. Failing to read capabilities or quota is an
// error; failing to read prices is a warning in the result.
func (s *Service) FindDTU(ctx context.Context, location string, f DTUFilter) (DBResult[DTUDatabase], error) {
	return findDB(ctx, s, location, func(prices priceIndex) model[DTUDatabase] {
		return model[DTUDatabase]{
			parse:   parseDTU,
			matches: func(d DTUDatabase) bool { return matchesDTU(d, f) },
			quota:   func(d *DTUDatabase) (int64, *[]QuotaHeadroom) { return d.QuotaNeeded, &d.Quota },
			name:    func(d DTUDatabase) string { return d.Name },
			price:   func(d *DTUDatabase) { d.Pricing = prices.dtu(*d) },
		}
	}, f.Sort, dtuSortColumns, dtuTieBreak, f.Limit)
}

// findDB is the search both purchasing models share: fetch, warn about the
// location and the server quota, assess, then rank.
func findDB[T any](ctx context.Context, s *Service, location string, modelFor func(priceIndex) model[T],
	by Sort, columns []sortColumn[T], tieBreak func(a, b T) int, limit int,
) (DBResult[T], error) {
	caps, quotas, prices, warnings, err := s.fetch(ctx, location)
	if err != nil {
		return DBResult[T]{}, err
	}

	res := DBResult[T]{Location: location, Warnings: warnings}
	res.Warnings = append(res.Warnings, locationWarnings(caps.location, location)...)

	if q, ok := quotas[serverQuota]; ok {
		res.ServerQuota = &QuotaHeadroom{Name: serverQuota, Limit: q.Limit, Used: q.Used, Free: q.Limit - q.Used}

		if res.ServerQuota.Free <= 0 {
			res.Warnings = append(res.Warnings, Warning{
				Code:    WarningServerQuotaExhausted,
				Message: fmt.Sprintf("the subscription has no logical server quota left in %s, so a database needs an existing server", location),
			})
		}
	}

	rows, more := assess(caps, quotas, modelFor(prices), &res.Summary, s.log)
	res.Warnings = append(res.Warnings, more...)
	res.Databases = rank(rows, by, columns, tieBreak, limit, &res.Summary, s.log)

	s.log.Debug("search complete",
		"considered", res.Summary.Considered,
		"matched", res.Summary.Matched,
		"restricted", res.Summary.Restricted,
		"no_quota", res.Summary.NoQuota,
		"eligible", res.Summary.Eligible,
		"shown", res.Summary.Shown,
		"warnings", len(res.Warnings),
	)

	return res, nil
}

// fetch reads capabilities, quota and prices at the same time. Capabilities
// and quota are required, so their errors cancel the others and fail the
// search. Prices only improve the answer, so their error becomes a warning
// instead. Each goroutine writes only its own variables, which are read after
// Wait returns, so no locking is needed.
func (s *Service) fetch(ctx context.Context, location string) (dbCaps, map[string]quota, priceIndex, []Warning, error) {
	// The location goes into a URL and an OData filter, so it is checked once
	// here for every source.
	if !azure.ValidLocation(location) {
		return dbCaps{}, nil, priceIndex{}, nil, fmt.Errorf("%q is not a valid location name", location)
	}

	var (
		caps      dbCaps
		quotas    map[string]quota
		rows      []retail.Price
		pricesErr error
	)

	start := time.Now()

	s.log.Debug("fetching capabilities, quota and prices", "location", location)

	group, gctx := errgroup.WithContext(ctx)

	group.Go(func() (err error) {
		defer s.logFetch("capabilities", time.Now(), func() int { return len(caps.objectives) }, &err)

		caps, err = s.arm.listDBCapabilities(gctx, location)

		return err
	})
	group.Go(func() (err error) {
		defer s.logFetch("quota", time.Now(), func() int { return len(quotas) }, &err)

		quotas, err = s.arm.listQuota(gctx, location)

		return err
	})
	group.Go(func() error {
		defer s.logFetch("prices", time.Now(), func() int { return len(rows) }, &pricesErr)

		rows, pricesErr = s.prices.Query(gctx, priceFilter(dbPricingService, location))

		return nil
	})

	if err := group.Wait(); err != nil {
		return dbCaps{}, nil, priceIndex{}, nil, err
	}

	// A cancelled or timed-out run must fail, not become a pricing warning.
	if err := ctx.Err(); err != nil {
		return dbCaps{}, nil, priceIndex{}, nil, err
	}

	warnings := []Warning{}

	if pricesErr != nil {
		s.log.Debug("prices unavailable, continuing without them", "error", pricesErr)

		warnings = append(warnings, Warning{
			Code:    WarningPricingUnavailable,
			Message: "prices could not be fetched, so none are shown: " + azure.ErrorText(pricesErr),
		})
	}

	s.log.Debug("fetched everything", "duration", time.Since(start))

	return caps, quotas, indexPrices(rows, location, s.log), warnings, nil
}

// locationWarnings warns when Azure says the location itself is not open to
// the subscription, which restricts every objective in it.
func locationWarnings(a availability, location string) []Warning {
	if a.deployable {
		return nil
	}

	return []Warning{{
		Code:    WarningLocationUnavailable,
		Message: fmt.Sprintf("Azure SQL Database is not available to this subscription in %s (%s)", location, a.reason),
	}}
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

package sql

import (
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"
)

// VCoreFilter is what to look for among vCore service objectives, and how to
// order it. A nil number means no requirement.
type VCoreFilter struct {
	// Tiers keeps objectives in any of these tiers. Empty means any.
	Tiers []Tier

	// Compute keeps provisioned or serverless objectives. The empty value
	// means both.
	Compute Compute

	// Hardware keeps objectives on any of this hardware. Empty means any.
	Hardware []Hardware

	VCores    *int
	MemoryGiB *float64

	// StorageGiB is the data size the database must be able to hold. It is
	// always a minimum.
	StorageGiB *float64

	// ExactMatch makes VCores and MemoryGiB exact values instead of minimums.
	ExactMatch bool

	// ZoneRedundant keeps objectives that can be made zone redundant, and adds
	// the surcharge to the price.
	ZoneRedundant bool

	// BackupRedundancy keeps objectives whose tier offers it. The empty value
	// means any.
	BackupRedundancy BackupRedundancy

	// Licence selects whether the SQL licence is priced in.
	Licence Licence

	// Sort orders the results before the limit is applied. The zero value is
	// cheapest monthly price first.
	Sort Sort

	// Limit caps the number of results; zero means no cap.
	Limit int
}

// LogValue renders the filter for debug logging.
func (f VCoreFilter) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("tiers", joinOrAny(f.Tiers)),
		slog.String("compute", orAny(string(f.Compute))),
		slog.String("hardware", joinOrAny(f.Hardware)),
		slog.String("vcores", optional(f.VCores)),
		slog.String("memory_gib", optional(f.MemoryGiB)),
		slog.String("storage_gib", optional(f.StorageGiB)),
		slog.Bool("exact_match", f.ExactMatch),
		slog.Bool("zone_redundant", f.ZoneRedundant),
		slog.String("backup_redundancy", orAny(string(f.BackupRedundancy))),
		slog.String("licence", string(f.Licence)),
		slog.String("sort", f.Sort.String()),
		slog.Int("limit", f.Limit),
	)
}

// DTUFilter is what to look for among DTU service objectives, and how to
// order it. A nil number means no requirement.
type DTUFilter struct {
	// Tiers keeps objectives in any of these tiers. Empty means any.
	Tiers []Tier

	DTUs *int

	// StorageGiB is the data size the database must be able to hold. It is
	// always a minimum.
	StorageGiB *float64

	// ExactMatch makes DTUs an exact value instead of a minimum.
	ExactMatch bool

	// ZoneRedundant keeps objectives that can be made zone redundant.
	ZoneRedundant bool

	// BackupRedundancy keeps objectives whose tier offers it. The empty value
	// means any.
	BackupRedundancy BackupRedundancy

	// Sort orders the results before the limit is applied. The zero value is
	// cheapest monthly price first.
	Sort Sort

	// Limit caps the number of results; zero means no cap.
	Limit int
}

// LogValue renders the filter for debug logging.
func (f DTUFilter) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("tiers", joinOrAny(f.Tiers)),
		slog.String("dtus", optional(f.DTUs)),
		slog.String("storage_gib", optional(f.StorageGiB)),
		slog.Bool("exact_match", f.ExactMatch),
		slog.Bool("zone_redundant", f.ZoneRedundant),
		slog.String("backup_redundancy", orAny(string(f.BackupRedundancy))),
		slog.String("sort", f.Sort.String()),
		slog.Int("limit", f.Limit),
	)
}

// optional formats a requirement that may be unset.
func optional[T int | float64](p *T) string {
	if p == nil {
		return "any"
	}

	return fmt.Sprint(*p)
}

func orAny(s string) string {
	if s == "" {
		return "any"
	}

	return s
}

func joinOrAny[T ~string](values []T) string {
	if len(values) == 0 {
		return "any"
	}

	return strings.Join(names(values), ",")
}

// Tier is a service tier, such as General Purpose or Standard.
type Tier string

// Tiers a requirement can ask for. General Purpose, Business Critical and
// Hyperscale are the vCore tiers; Basic, Standard and Premium the DTU tiers.
const (
	TierGeneralPurpose   Tier = "general-purpose"
	TierBusinessCritical Tier = "business-critical"
	TierHyperscale       Tier = "hyperscale"
	TierBasic            Tier = "basic"
	TierStandard         Tier = "standard"
	TierPremium          Tier = "premium"
)

func vcoreTiers() []Tier { return []Tier{TierGeneralPurpose, TierBusinessCritical, TierHyperscale} }

func dtuTiers() []Tier { return []Tier{TierBasic, TierStandard, TierPremium} }

// VCoreTierNames returns the vCore tiers, for help text and completion.
func VCoreTierNames() []string { return names(vcoreTiers()) }

// DTUTierNames returns the DTU tiers, for help text and completion.
func DTUTierNames() []string { return names(dtuTiers()) }

// ParseVCoreTiers reads a comma-separated list of vCore tiers. An empty
// string means no requirement and returns nil.
func ParseVCoreTiers(s string) ([]Tier, error) { return parseList(s, vcoreTiers(), "tier") }

// ParseDTUTiers reads a comma-separated list of DTU tiers. An empty string
// means no requirement and returns nil.
func ParseDTUTiers(s string) ([]Tier, error) { return parseList(s, dtuTiers(), "tier") }

// dbEditions maps the edition names Azure uses for single databases to tiers.
// Other editions, such as Free, DataWarehouse and System, are not offered.
var dbEditions = map[string]Tier{
	"GeneralPurpose":   TierGeneralPurpose,
	"BusinessCritical": TierBusinessCritical,
	"Hyperscale":       TierHyperscale,
	"Basic":            TierBasic,
	"Standard":         TierStandard,
	"Premium":          TierPremium,
}

// Compute is whether a database's compute is provisioned or serverless.
type Compute string

// Compute models.
const (
	ComputeProvisioned Compute = "provisioned"
	ComputeServerless  Compute = "serverless"
)

// ComputeNames returns the values accepted for the compute model, for help
// text and completion.
func ComputeNames() []string {
	return []string{string(ComputeProvisioned), string(ComputeServerless), "any"}
}

// ParseCompute reads a compute model. "any" and "" mean no requirement and
// return "".
func ParseCompute(s string) (Compute, error) {
	return parseOne(s, []Compute{ComputeProvisioned, ComputeServerless}, "compute model", ComputeNames())
}

// Hardware is a hardware configuration, such as standard-series (Gen5).
type Hardware string

// Hardware configurations a requirement can ask for.
const (
	HardwareStandard        Hardware = "standard-series"
	HardwarePremium         Hardware = "premium-series"
	HardwarePremiumMemoryOp Hardware = "premium-series-memory-optimised"
	HardwareDC              Hardware = "dc-series"
)

// hardwareOther labels a hardware family this package does not know, such as
// the retired Gen4 and Fsv2.
const hardwareOther Hardware = "other"

func allHardware() []Hardware {
	return []Hardware{HardwareStandard, HardwarePremium, HardwarePremiumMemoryOp, HardwareDC}
}

// HardwareNames returns the hardware, for help text and completion.
func HardwareNames() []string { return names(allHardware()) }

// ParseHardware reads a comma-separated list of hardware. An empty string
// means no requirement and returns nil.
func ParseHardware(s string) ([]Hardware, error) { return parseList(s, allHardware(), "hardware") }

// families maps the hardware families as Azure names them, in lower case, to
// hardware.
var families = map[string]Hardware{
	"gen5": HardwareStandard,
	"8im":  HardwarePremium,
	"8ih":  HardwarePremiumMemoryOp,
	"dc":   HardwareDC,
}

func hardwareOf(family string) Hardware {
	if h, ok := families[strings.ToLower(family)]; ok {
		return h
	}

	return hardwareOther
}

// memoryPerVCore is the documented memory per vCore in GB of provisioned
// compute. SQL Database does not report memory, so it comes from here.
var memoryPerVCore = map[Hardware]float64{
	HardwareStandard:        5.1,
	HardwarePremium:         5.1,
	HardwarePremiumMemoryOp: 10.2,
	HardwareDC:              4.5,
}

// BackupRedundancy is how the backups are stored.
type BackupRedundancy string

// Backup storage redundancies.
const (
	BackupLocal   BackupRedundancy = "local"
	BackupZone    BackupRedundancy = "zone"
	BackupGeo     BackupRedundancy = "geo"
	BackupGeoZone BackupRedundancy = "geo-zone"
)

func backupRedundancies() []BackupRedundancy {
	return []BackupRedundancy{BackupLocal, BackupZone, BackupGeo, BackupGeoZone}
}

// BackupRedundancyNames returns the values accepted for backup redundancy, for
// help text and completion.
func BackupRedundancyNames() []string { return append(names(backupRedundancies()), "any") }

// ParseBackupRedundancy reads a backup redundancy. "any" and "" mean no
// requirement and return "".
func ParseBackupRedundancy(s string) (BackupRedundancy, error) {
	return parseOne(s, backupRedundancies(), "backup redundancy", BackupRedundancyNames())
}

// storageAccountTypes maps the storage types Azure reports to backup
// redundancies.
var storageAccountTypes = map[string]BackupRedundancy{
	"LRS":  BackupLocal,
	"ZRS":  BackupZone,
	"GRS":  BackupGeo,
	"GZRS": BackupGeoZone,
}

// backupRedundancyOf converts the storage types Azure reports, in a fixed
// order. Never nil.
func backupRedundancyOf(types []string) []BackupRedundancy {
	out := []BackupRedundancy{}

	for _, b := range backupRedundancies() {
		for _, t := range types {
			if storageAccountTypes[strings.ToUpper(t)] == b && !slices.Contains(out, b) {
				out = append(out, b)
			}
		}
	}

	return out
}

// Licence is whether the price includes the SQL licence, or leaves it out
// because Azure Hybrid Benefit covers it.
type Licence string

// Licence options.
const (
	LicenceIncluded Licence = "included"
	LicenceAHB      Licence = "ahb"
)

// LicenceNames returns the values accepted for the licence, for help text and
// completion.
func LicenceNames() []string { return []string{string(LicenceIncluded), string(LicenceAHB)} }

// ParseLicence reads a licence option. Empty means included. Unlike the other
// options, it has no "any", as a price needs one licence or the other.
func ParseLicence(s string) (Licence, error) {
	if strings.EqualFold(strings.TrimSpace(s), "any") {
		return "", fmt.Errorf("unknown licence %q: use one of %s", "any", strings.Join(LicenceNames(), ", "))
	}

	l, err := parseOne(s, []Licence{LicenceIncluded, LicenceAHB}, "licence", LicenceNames())
	if l == "" && err == nil {
		l = LicenceIncluded
	}

	return l, err
}

// parseOne reads one of the allowed values, case-insensitively. "any" and ""
// return "".
func parseOne[T ~string](s string, allowed []T, what string, accepted []string) (T, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || s == "any" {
		return "", nil
	}

	if v := T(s); slices.Contains(allowed, v) {
		return v, nil
	}

	return "", fmt.Errorf("unknown %s %q: use one of %s", what, s, strings.Join(accepted, ", "))
}

// parseList reads a comma-separated list of allowed values, without
// duplicates. An empty string returns nil.
func parseList[T ~string](s string, allowed []T, what string) ([]T, error) {
	var out []T

	for part := range strings.SplitSeq(s, ",") {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" {
			continue
		}

		v := T(part)
		if !slices.Contains(allowed, v) {
			return nil, fmt.Errorf("unknown %s %q: use one of %s", what, part, strings.Join(names(allowed), ", "))
		}

		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}

	return out, nil
}

func names[T ~string](values []T) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = string(v)
	}

	return out
}

// The filtering: everything below turns the fetched capabilities, quota and
// prices into ranked, deployable objectives.

const (
	// regionalVCores is the usage entry for the regional vCore quota shared by
	// SQL Database and Azure Synapse dedicated SQL pools.
	regionalVCores = "RegionalVCoreQuotaForSQLDBAndDW"

	// serverQuota is the usage entry for the logical servers in a region.
	serverQuota = "ServerQuota"

	// dtusPerVCore is how many DTUs count as one vCore of quota.
	dtusPerVCore = 100

	// hyperscaleMaxStorageGiB is the documented, fixed maximum data size of a
	// Hyperscale database: 128 TB.
	hyperscaleMaxStorageGiB = 128 * 1024

	// memoryTolerance is how far apart two memory sizes in GiB may be and still
	// be called equal.
	memoryTolerance = 0.01
)

// model is what the shared filtering needs to know about one purchasing
// model's rows: how to read one from a service objective, and how to match,
// count and name it.
type model[T any] struct {
	// parse reports false for an objective of the other purchasing model, or
	// one this package does not offer.
	parse   func(serviceObjective) (T, bool)
	matches func(T) bool
	quota   func(*T) (needed int64, headroom *[]QuotaHeadroom)
	name    func(T) string
	price   func(*T)
}

// assess returns the deployable rows, unsorted, counting the summary as far as
// quota, with any warnings.
func assess[T any](caps dbCaps, quotas map[string]quota, m model[T], summary *Summary, log *slog.Logger) ([]T, []Warning) {
	out := []T{}

	for _, o := range caps.objectives {
		row, ok := m.parse(o)
		if !ok {
			log.Debug("skipping SKU", "sku", o.name, "reason", "other-purchase-model-or-edition", "edition", o.edition, "unit", o.unit)

			continue
		}

		summary.Considered++

		if !m.matches(row) {
			continue
		}

		summary.Matched++

		if !o.availability.deployable {
			summary.Restricted++

			log.Debug("skipping SKU", "sku", o.name, "reason", "restricted", "why", o.availability.reason)

			continue
		}

		out = append(out, row)
	}

	out, warnings := applyQuota(out, quotas, m.quota, m.name, summary, log)

	for i := range out {
		m.price(&out[i])
	}

	return out, warnings
}

// applyQuota attaches the regional vCore quota left to each row and removes
// the ones that cannot fit one more. When Azure did not report the quota, it
// is not checked, and that becomes a warning.
func applyQuota[T any](rows []T, quotas map[string]quota, need func(*T) (int64, *[]QuotaHeadroom),
	name func(T) string, summary *Summary, log *slog.Logger,
) ([]T, []Warning) {
	q, ok := quotas[regionalVCores]
	if !ok {
		log.Debug("quota entry missing", "key", regionalVCores)

		return rows, []Warning{{
			Code:    WarningQuotaMissing,
			Message: fmt.Sprintf("Azure reported no quota entry %q, so that limit could not be checked", regionalVCores),
		}}
	}

	kept := make([]T, 0, len(rows))
	free := q.Limit - q.Used

	for _, row := range rows {
		needed, headroom := need(&row)
		*headroom = append(*headroom, QuotaHeadroom{Name: regionalVCores, Limit: q.Limit, Used: q.Used, Free: free})

		if free < needed {
			summary.NoQuota++

			log.Debug("skipping SKU", "sku", name(row), "reason", "no-quota", "needed", needed, "free", free)

			continue
		}

		kept = append(kept, row)
	}

	return kept, nil
}

// parseVCore turns a vCore service objective into a VCoreDatabase.
func parseVCore(o serviceObjective) (VCoreDatabase, bool) {
	tier, ok := dbEditions[o.edition]
	if !ok || tier.isDTU() || !strings.EqualFold(o.unit, "VCores") {
		return VCoreDatabase{}, false
	}

	vcores := int(math.Round(o.performance))
	if vcores < 1 {
		return VCoreDatabase{}, false
	}

	d := VCoreDatabase{
		Name:             o.name,
		SKU:              o.skuName,
		Tier:             tier,
		Compute:          ComputeProvisioned,
		Hardware:         hardwareOf(o.family),
		Family:           o.family,
		VCores:           vcores,
		MaxStorageGiB:    o.maxStorageGiB,
		ZoneRedundant:    o.zoneRedundant,
		BackupRedundancy: backupRedundancyOf(o.backup),
		QuotaNeeded:      int64(vcores),
		Quota:            []QuotaHeadroom{},
	}

	if strings.EqualFold(o.computeModel, "Serverless") {
		d.Compute = ComputeServerless
	}

	// Azure reports no size range for Hyperscale, whose limit is fixed.
	if d.MaxStorageGiB == nil && tier == TierHyperscale {
		d.MaxStorageGiB = new(float64(hyperscaleMaxStorageGiB))
	}

	if perVCore, ok := memoryPerVCore[d.Hardware]; ok && d.Compute == ComputeProvisioned {
		d.MemoryGiB = new(roundMemory(perVCore * float64(vcores)))
	}

	return d, true
}

// parseDTU turns a DTU service objective into a DTUDatabase.
func parseDTU(o serviceObjective) (DTUDatabase, bool) {
	tier, ok := dbEditions[o.edition]
	if !ok || !tier.isDTU() || !strings.EqualFold(o.unit, "DTU") {
		return DTUDatabase{}, false
	}

	dtus := int(math.Round(o.performance))
	if dtus < 1 {
		return DTUDatabase{}, false
	}

	return DTUDatabase{
		Name:             o.name,
		Tier:             tier,
		DTUs:             dtus,
		MaxStorageGiB:    o.maxStorageGiB,
		ZoneRedundant:    o.zoneRedundant,
		BackupRedundancy: backupRedundancyOf(o.backup),
		// Rounded up: a 5 DTU Basic database still takes some of a vCore.
		QuotaNeeded: int64(math.Ceil(float64(dtus) / dtusPerVCore)),
		Quota:       []QuotaHeadroom{},
	}, true
}

// isDTU reports whether the tier belongs to the DTU purchasing model.
func (t Tier) isDTU() bool { return slices.Contains(dtuTiers(), t) }

// roundMemory rounds a memory size worked out from a ratio to two decimal
// places, so that 3 vCores of 5.1 GB show as 15.3, not 15.299999999999999.
func roundMemory(gb float64) float64 {
	return math.Round(gb*100) / 100
}

// matchesVCore reports whether a vCore database meets the filter's
// requirements. An unknown value never matches a requirement on it: assuming
// would be a guess.
func matchesVCore(d VCoreDatabase, f VCoreFilter) bool {
	switch {
	case len(f.Tiers) > 0 && !slices.Contains(f.Tiers, d.Tier),
		f.Compute != "" && d.Compute != f.Compute,
		len(f.Hardware) > 0 && !slices.Contains(f.Hardware, d.Hardware),
		f.VCores != nil && !matchesInt(d.VCores, *f.VCores, f.ExactMatch),
		f.MemoryGiB != nil && (d.MemoryGiB == nil || !matchesFloat(*d.MemoryGiB, *f.MemoryGiB, f.ExactMatch)):
		return false
	default:
		return matchesCommon(d.MaxStorageGiB, d.ZoneRedundant, d.BackupRedundancy, f.StorageGiB, f.ZoneRedundant, f.BackupRedundancy)
	}
}

// matchesDTU reports whether a DTU database meets the filter's requirements.
func matchesDTU(d DTUDatabase, f DTUFilter) bool {
	switch {
	case len(f.Tiers) > 0 && !slices.Contains(f.Tiers, d.Tier),
		f.DTUs != nil && !matchesInt(d.DTUs, *f.DTUs, f.ExactMatch):
		return false
	default:
		return matchesCommon(d.MaxStorageGiB, d.ZoneRedundant, d.BackupRedundancy, f.StorageGiB, f.ZoneRedundant, f.BackupRedundancy)
	}
}

// matchesCommon checks the requirements both purchasing models have.
func matchesCommon(maxStorage *float64, zoneRedundant bool, backup []BackupRedundancy,
	wantStorage *float64, wantZoneRedundant bool, wantBackup BackupRedundancy,
) bool {
	switch {
	case wantStorage != nil && (maxStorage == nil || *maxStorage < *wantStorage),
		wantZoneRedundant && !zoneRedundant,
		wantBackup != "" && !slices.Contains(backup, wantBackup):
		return false
	default:
		return true
	}
}

func matchesInt(have, want int, exact bool) bool {
	if exact {
		return have == want
	}

	return have >= want
}

func matchesFloat(have, want float64, exact bool) bool {
	if exact {
		return math.Abs(have-want) <= memoryTolerance
	}

	return have >= want
}

// rank sorts the rows, counts them, and applies the limit.
func rank[T any](rows []T, by Sort, columns []sortColumn[T], tieBreak func(a, b T) int, limit int, summary *Summary, log *slog.Logger) []T {
	sortRows(rows, by, columns, tieBreak)

	summary.Eligible = len(rows)

	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}

	summary.Shown = len(rows)

	log.Debug("ranked", "eligible", summary.Eligible, "shown", summary.Shown, "sort", by, "limit", limit)

	return rows
}

package vm

import (
	"fmt"
	"log/slog"
	"maps"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Filter is what to look for and how to order it. A nil number means no
// requirement.
type Filter struct {
	CPUs      *int
	MemoryGiB *float64
	GPUs      *int

	// NICs is the number of network interfaces the SKU must be able to take.
	NICs *int

	// ExactMatch makes CPUs, MemoryGiB, GPUs and NICs exact values instead of
	// minimums.
	ExactMatch bool

	Arch       Arch
	Categories []Category

	// DiskController keeps only SKUs that support this disk controller. The
	// empty value means any.
	DiskController DiskController

	// OS selects which operating system's prices are used.
	OS OS

	// IncludeSpot asks for Spot prices and eviction rates as well, and keeps
	// SKUs that only the Spot quota has room for.
	IncludeSpot bool

	// Sort orders the VMs before the limit is applied, so the limit keeps the
	// first of them. The zero value is cheapest monthly price first.
	Sort Sort

	// Limit caps the number of VMs returned; zero means no cap.
	Limit int
}

// LogValue renders the filter for debug logging, with the requirements read
// through their pointers and an unset one shown as "any".
func (f Filter) LogValue() slog.Value {
	arch := string(f.Arch)
	if arch == "" {
		arch = "any"
	}

	categories := "any"
	if len(f.Categories) > 0 {
		names := make([]string, len(f.Categories))
		for i, c := range f.Categories {
			names[i] = string(c)
		}

		categories = strings.Join(names, ",")
	}

	diskController := string(f.DiskController)
	if diskController == "" {
		diskController = "any"
	}

	return slog.GroupValue(
		slog.String("cpus", optional(f.CPUs)),
		slog.String("memory_gib", optional(f.MemoryGiB)),
		slog.String("gpus", optional(f.GPUs)),
		slog.String("nics", optional(f.NICs)),
		slog.Bool("exact_match", f.ExactMatch),
		slog.String("arch", arch),
		slog.String("categories", categories),
		slog.String("disk_controller", diskController),
		slog.String("os", string(f.OS)),
		slog.Bool("include_spot", f.IncludeSpot),
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

// Arch is a CPU architecture. The empty value means any.
type Arch string

// Architectures a requirement can ask for.
const (
	ArchX64   Arch = "x64"
	ArchARM64 Arch = "arm64"
)

// archUnknown is what a SKU reports when Azure does not say.
const archUnknown Arch = "unknown"

// ArchNames returns the values accepted for the architecture requirement, for
// help text and completion.
func ArchNames() []string { return []string{"any", string(ArchX64), string(ArchARM64)} }

// ParseArch reads an architecture requirement. "any" and "" mean no
// requirement and return "".
func ParseArch(s string) (Arch, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "any":
		return "", nil
	case string(ArchX64):
		return ArchX64, nil
	case string(ArchARM64):
		return ArchARM64, nil
	default:
		return "", fmt.Errorf("unknown architecture %q: use one of %s", s, strings.Join(ArchNames(), ", "))
	}
}

// DiskController is a disk controller a SKU can attach disks with. The empty
// value means any.
//
// It matters because of the image: an image without NVMe drivers cannot boot
// on a SKU that supports only NVMe, and an image tagged as needing NVMe cannot
// be used on a SKU that supports only SCSI.
type DiskController string

// Disk controllers a requirement can ask for.
const (
	DiskControllerSCSI DiskController = "scsi"
	DiskControllerNVMe DiskController = "nvme"
)

// DiskControllerNames returns the values accepted for the disk controller
// requirement, for help text and completion.
func DiskControllerNames() []string {
	return []string{"any", string(DiskControllerSCSI), string(DiskControllerNVMe)}
}

// ParseDiskController reads a disk controller requirement. "any" and "" mean
// no requirement and return "".
func ParseDiskController(s string) (DiskController, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "any":
		return "", nil
	case string(DiskControllerSCSI):
		return DiskControllerSCSI, nil
	case string(DiskControllerNVMe):
		return DiskControllerNVMe, nil
	default:
		return "", fmt.Errorf("unknown disk controller %q: use one of %s", s, strings.Join(DiskControllerNames(), ", "))
	}
}

// Category is a kind of virtual machine, such as general purpose or memory
// optimised.
//
// Azure does not publish a category for a SKU, so it is worked out from the
// series letter in the SKU's name (the D in Standard_D4s_v5).
type Category string

// Categories a requirement can ask for.
const (
	CategoryGeneral Category = "general-purpose"
	CategoryBurst   Category = "burstable"
	CategoryCompute Category = "compute"
	CategoryMemory  Category = "memory"
	CategoryStorage Category = "storage"
	CategoryGPU     Category = "gpu"
	CategoryHPC     Category = "hpc"
)

// categoryOther labels a SKU whose series has no known category.
const categoryOther Category = "other"

// allCategories lists every category, in the order they are documented.
func allCategories() []Category {
	return []Category{CategoryGeneral, CategoryBurst, CategoryCompute, CategoryMemory, CategoryStorage, CategoryGPU, CategoryHPC}
}

// CategoryNames returns the category names, for help text and completion.
func CategoryNames() []string {
	names := make([]string, 0, len(allCategories()))
	for _, c := range allCategories() {
		names = append(names, string(c))
	}

	return names
}

// ParseCategories reads a comma-separated list such as "memory,gpu". An empty
// string means no requirement and returns nil.
func ParseCategories(s string) ([]Category, error) {
	var out []Category

	for part := range strings.SplitSeq(s, ",") {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" {
			continue
		}

		c := Category(part)
		if !slices.Contains(allCategories(), c) {
			return nil, fmt.Errorf("unknown category %q: use one of %s", part, strings.Join(CategoryNames(), ", "))
		}

		if !slices.Contains(out, c) {
			out = append(out, c)
		}
	}

	return out, nil
}

// OS selects which operating system's prices are used.
type OS string

// Operating systems the Retail Prices API distinguishes.
const (
	OSLinux   OS = "linux"
	OSWindows OS = "windows"
)

// ParseOS reads an operating system. Empty means Linux.
func ParseOS(s string) (OS, error) {
	switch os := OS(strings.ToLower(strings.TrimSpace(s))); os {
	case "":
		return OSLinux, nil
	case OSLinux, OSWindows:
		return os, nil
	default:
		return "", fmt.Errorf("unknown operating system %q: use %s or %s", s, OSLinux, OSWindows)
	}
}

// The filtering: everything below turns the fetched SKUs, quota and prices
// into ranked, deployable VMs.

const (
	// regionalCores is the quota entry for the regional vCPU limit.
	regionalCores = "cores"

	// spotCores is the quota entry for the regional Spot vCPU limit.
	spotCores = "lowPriorityCores"

	// memoryTolerance is how far apart two memory sizes in GiB may be and still
	// be called equal. Azure reports sizes such as 3.5 and 218.75.
	memoryTolerance = 0.01
)

// assess returns the eligible VMs, unsorted, counting the summary as far as
// quota, with any quota warnings.
func assess(skus []sku, quotas map[string]quota, prices map[string]priceEntry, f Filter, summary *Summary, log *slog.Logger) ([]VM, []Warning) {
	vms := candidates(skus, f, summary, log)
	vms, warnings := applyQuota(vms, quotas, f.IncludeSpot, summary, log)
	applyPrices(vms, prices, f.IncludeSpot, log)

	return vms, warnings
}

// rank sorts the VMs, counts them, and applies the limit.
func rank(vms []VM, f Filter, summary *Summary, log *slog.Logger) []VM {
	sortVMs(vms, f.Sort)

	summary.Eligible = len(vms)

	if f.Limit > 0 && len(vms) > f.Limit {
		vms = vms[:f.Limit]
	}

	summary.Shown = len(vms)

	log.Debug("ranked VMs", "eligible", summary.Eligible, "shown", summary.Shown, "sort", f.Sort, "limit", f.Limit)

	return vms
}

// candidates parses and matches the SKUs, and removes the ones Azure restricts
// for the subscription in the location.
func candidates(skus []sku, f Filter, summary *Summary, log *slog.Logger) []VM {
	out := []VM{}

	for _, s := range skus {
		summary.Considered++

		v, ok := parseVM(s)
		if !ok {
			log.Debug("skipping SKU", "sku", s.Name, "reason", "unreadable-capabilities", "capabilities", s.Capabilities)

			continue
		}

		if !matches(v, f) {
			continue
		}

		summary.Matched++

		if s.LocationRestricted {
			summary.Restricted++

			log.Debug("skipping SKU", "sku", s.Name, "reason", "location-restricted")

			continue
		}

		v.Zones = usableZones(s)
		if len(v.Zones) < len(s.Zones) {
			log.Debug("zones restricted", "sku", s.Name, "zones", v.Zones, "restricted", s.RestrictedZones)
		}

		out = append(out, v)
	}

	return out
}

// usableZones returns the zones of the SKU that are not restricted, sorted.
func usableZones(s sku) []string {
	zones := []string{}

	for _, z := range s.Zones {
		if !slices.Contains(s.RestrictedZones, z) {
			zones = append(zones, z)
		}
	}

	slices.Sort(zones)

	return zones
}

// applyQuota attaches the quota left to each VM and removes the ones that cannot
// fit one more instance. A VM is limited by its family quota and by the regional
// vCPU quota, and one instance uses its billed vCPUs of each. With includeSpot, a
// Spot-capable VM without that quota is kept, as Spot only, if it fits the
// regional Spot quota instead.
func applyQuota(vms []VM, quotas map[string]quota, includeSpot bool, summary *Summary, log *slog.Logger) ([]VM, []Warning) {
	kept := make([]VM, 0, len(vms))
	missing := map[string]bool{}

	for _, v := range vms {
		keys := []string{regionalCores}
		if v.Family != "" {
			keys = []string{v.Family, regionalCores}
		}

		fits := true

		var short []string

		for _, key := range keys {
			q, ok := quotas[key]
			if !ok {
				missing[key] = true

				continue
			}

			free := q.Limit - q.Used
			v.Quota = append(v.Quota, QuotaHeadroom{Name: key, Limit: q.Limit, Used: q.Used, Free: free})

			if free < int64(v.VCPUs) {
				fits = false

				short = append(short, fmt.Sprintf("%s: %d of %d free", key, free, q.Limit))
			}
		}

		// Spot VMs use one regional Spot quota instead of the family and regional
		// limits, so a SKU without on-demand quota may still deploy as Spot. A
		// missing Spot entry keeps nothing: it would be a guess.
		if !fits && includeSpot && v.SpotCapable {
			spot, ok := quotas[spotCores]

			switch {
			case !ok:
				missing[spotCores] = true
			case spot.Limit-spot.Used >= int64(v.VCPUs):
				v.SpotOnly = true
				fits = true
				summary.SpotOnly++

				log.Debug("keeping SKU as Spot only", "sku", v.Name, "vcpus", v.VCPUs,
					"short_quota", short, "spot_free", spot.Limit-spot.Used)
			}
		}

		if !fits {
			summary.NoQuota++

			log.Debug("skipping SKU", "sku", v.Name, "reason", "no-quota", "vcpus", v.VCPUs, "short_quota", short)

			continue
		}

		kept = append(kept, v)
	}

	var warnings []Warning

	for _, key := range slices.Sorted(maps.Keys(missing)) {
		log.Debug("quota entry missing", "key", key)

		warnings = append(warnings, Warning{
			Code:    WarningQuotaMissing,
			Message: fmt.Sprintf("Azure reported no quota entry %q, so that limit could not be checked", key),
		})
	}

	return kept, warnings
}

var seriesPattern = regexp.MustCompile(`^Standard_([A-Za-z])`)

// seriesCategories maps the series letter of a SKU name to the categories it
// belongs to. Burstable is a sub-type of general purpose. A series not listed,
// such as the FPGA PB-series, is labelled other.
var seriesCategories = map[string][]Category{
	"A": {CategoryGeneral},
	"B": {CategoryGeneral, CategoryBurst},
	"D": {CategoryGeneral},
	"E": {CategoryMemory},
	"G": {CategoryMemory},
	"M": {CategoryMemory},
	"F": {CategoryCompute},
	"L": {CategoryStorage},
	"H": {CategoryHPC},
	"N": {CategoryGPU},
}

// seriesLetter returns the upper-case series letter of a SKU name, or "".
func seriesLetter(name string) string {
	m := seriesPattern.FindStringSubmatch(name)
	if m == nil {
		return ""
	}

	return strings.ToUpper(m[1])
}

// categoriesOf returns the categories a SKU belongs to. A SKU with GPUs is a
// GPU SKU whatever its name.
func categoriesOf(series string, gpus int) []Category {
	cats := slices.Clone(seriesCategories[series])
	if gpus > 0 && !slices.Contains(cats, CategoryGPU) {
		cats = append(cats, CategoryGPU)
	}

	return cats
}

// categoryLabel is the single label shown for a SKU: the most specific category
// it belongs to, which is the last of its categories. A burstable SKU is also
// general purpose, but it is labelled burstable, so that filtering by
// burstable shows what was asked for. categoriesOf puts gpu last, so a SKU
// with GPUs is labelled gpu.
func categoryLabel(cats []Category) Category {
	if len(cats) == 0 {
		return categoryOther
	}

	return cats[len(cats)-1]
}

// parseVM reads the capabilities of a virtual machine SKU. It reports false when
// a capability the filtering depends on is missing or not understood, because
// guessing would produce wrong answers.
func parseVM(s sku) (VM, bool) {
	vcpus, ok := parseInt(s.Capabilities["vCPUs"], 1)
	if !ok {
		return VM{}, false
	}

	available := vcpus

	if v, present := s.Capabilities["vCPUsAvailable"]; present {
		if available, ok = parseInt(v, 1); !ok {
			return VM{}, false
		}
	}

	memory, ok := parseNonNegativeFloat(s.Capabilities["MemoryGB"])
	if !ok {
		return VM{}, false
	}

	gpus := 0

	if v, present := s.Capabilities["GPUs"]; present {
		if gpus, ok = parseInt(v, 0); !ok {
			return VM{}, false
		}
	}

	// A NIC limit that cannot be read is unknown rather than a reason to skip
	// the SKU, so it cannot hide a SKU from a search that does not ask about
	// NICs.
	var nics *int
	if n, ok := parseInt(s.Capabilities["MaxNetworkInterfaces"], 1); ok {
		nics = &n
	}

	cats := categoriesOf(seriesLetter(s.Name), gpus)

	_, hasControllers := s.Capabilities["DiskControllerTypes"]

	return VM{
		Name:           s.Name,
		Family:         s.Family,
		VCPUs:          vcpus,
		VCPUsAvailable: available,
		MemoryGiB:      memory,
		GPUs:           gpus,
		MaxNICs:        nics,
		Arch:           archFromCapability(s.Capabilities["CpuArchitectureType"]),
		Category:       categoryLabel(cats),
		Zones:          []string{},
		SpotCapable:    strings.EqualFold(s.Capabilities["LowPriorityCapable"], "true"),
		Quota:          []QuotaHeadroom{},
		categories:     cats,

		DiskControllers: diskControllersFromCapability(s.Capabilities["DiskControllerTypes"], hasControllers),
	}, true
}

// diskControllersFromCapability reads a SKU's DiskControllerTypes capability,
// such as "SCSI", "NVMe" or "SCSI,NVMe" (Azure uses either order). Azure
// leaves the capability out of older SKUs and documents SCSI as the default
// then, so that is not a guess. Names it does not know are dropped rather than
// failing the SKU, so a new controller cannot hide a SKU from a search that
// does not ask about controllers.
func diskControllersFromCapability(s string, present bool) []DiskController {
	if !present {
		return []DiskController{DiskControllerSCSI}
	}

	var scsi, nvme bool

	for part := range strings.SplitSeq(s, ",") {
		switch DiskController(strings.ToLower(strings.TrimSpace(part))) {
		case DiskControllerSCSI:
			scsi = true
		case DiskControllerNVMe:
			nvme = true
		}
	}

	out := []DiskController{}
	if scsi {
		out = append(out, DiskControllerSCSI)
	}

	if nvme {
		out = append(out, DiskControllerNVMe)
	}

	return out
}

// archFromCapability reads a SKU's CpuArchitectureType capability as Azure
// reports it ("x64", "Arm64"). Unlike ParseArch, which reads what the user
// asked for, a value it does not know is labelled unknown rather than rejected.
func archFromCapability(s string) Arch {
	switch a := Arch(strings.ToLower(s)); a {
	case ArchX64, ArchARM64:
		return a
	default:
		return archUnknown
	}
}

// matches reports whether a parsed VM meets the filter's requirements.
func matches(v VM, f Filter) bool {
	if f.Arch != "" && v.Arch != f.Arch {
		return false
	}

	if len(f.Categories) > 0 &&
		!slices.ContainsFunc(f.Categories, func(want Category) bool { return slices.Contains(v.categories, want) }) {
		return false
	}

	if f.DiskController != "" && !slices.Contains(v.DiskControllers, f.DiskController) {
		return false
	}

	// CPUs is what the guest sees, which is lower than the billed count for
	// constrained-core SKUs.
	if f.CPUs != nil && !matchesInt(v.VCPUsAvailable, *f.CPUs, f.ExactMatch) {
		return false
	}

	if f.GPUs != nil && !matchesInt(v.GPUs, *f.GPUs, f.ExactMatch) {
		return false
	}

	// An unknown NIC limit never matches: assuming it would be a guess.
	if f.NICs != nil && (v.MaxNICs == nil || !matchesInt(*v.MaxNICs, *f.NICs, f.ExactMatch)) {
		return false
	}

	if f.MemoryGiB != nil && !matchesMemory(v.MemoryGiB, *f.MemoryGiB, f.ExactMatch) {
		return false
	}

	return true
}

func matchesInt(have, want int, exact bool) bool {
	if exact {
		return have == want
	}

	return have >= want
}

func matchesMemory(have, want float64, exact bool) bool {
	if exact {
		return math.Abs(have-want) <= memoryTolerance
	}

	return have >= want
}

// parseInt reads an integer capability and checks it is at least lowest.
func parseInt(s string, lowest int) (int, bool) {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || v < lowest {
		return 0, false
	}

	return v, true
}

// parseNonNegativeFloat reads a decimal capability. ParseFloat accepts "NaN"
// and "Inf", which would silently defeat every comparison, so they are rejected.
func parseNonNegativeFloat(s string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0, false
	}

	return v, true
}

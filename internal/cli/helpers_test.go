package cli

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	azruntime "github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/google/go-cmp/cmp"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/mike-guy/azskufinder/internal/app"
	"github.com/mike-guy/azskufinder/internal/vm"
)

var update = flag.Bool("update", false, "rewrite golden files")

// golden compares got with testdata/<name>.golden, or rewrites the file with
// -update.
func golden(t *testing.T, name string, got []byte) {
	t.Helper()

	path := filepath.Join("testdata", name+".golden")

	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("creating testdata: %v", err)
		}

		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}

		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s (run with -update to create it): %v", path, err)
	}

	if diff := cmp.Diff(string(want), string(got)); diff != "" {
		t.Errorf("%s mismatch (-want +got):\n%s", path, diff)
	}
}

// newTestRoot returns the root command with its output captured.
func newTestRoot(t *testing.T) (root *cobra.Command, stdout, stderr *bytes.Buffer) {
	t.Helper()

	root = newRoot(app.New("test"))
	stdout, stderr = &bytes.Buffer{}, &bytes.Buffer{}
	root.SetOut(stdout)
	root.SetErr(stderr)

	return root, stdout, stderr
}

// vmCmd returns the vm subcommand of root.
func vmCmd(t *testing.T, root *cobra.Command) *cobra.Command {
	t.Helper()

	cmd, _, err := root.Find([]string{"vm"})
	if err != nil || cmd.Name() != "vm" {
		t.Fatalf("root.Find(vm) = %v, %v, want the vm command", cmd, err)
	}

	return cmd
}

// isolateEnv clears the environment variable of every flag, so that the
// developer's own AZSKUFINDER_* settings cannot change a test that reads the
// environment through viper. An empty variable counts as unset. It uses
// t.Setenv, so the test cannot call t.Parallel, though its subtests can.
func isolateEnv(t *testing.T) {
	t.Helper()

	root, _, _ := newTestRoot(t)

	for _, flags := range []*pflag.FlagSet{root.PersistentFlags(), vmCmd(t, root).Flags()} {
		flags.VisitAll(func(f *pflag.Flag) { t.Setenv(envName(f.Name), "") })
	}
}

// vmViper returns a viper bound to the real root and vm flags, so that their
// defaults apply, with each entry of set applied on top as if it came from the
// environment or config file. It does not read the environment.
func vmViper(t *testing.T, set map[string]any) *viper.Viper {
	t.Helper()

	root, _, _ := newTestRoot(t)
	v := viper.New()

	if err := v.BindPFlags(root.PersistentFlags()); err != nil {
		t.Fatalf("binding root flags: %v", err)
	}

	if err := v.BindPFlags(vmCmd(t, root).Flags()); err != nil {
		t.Fatalf("binding vm flags: %v", err)
	}

	for k, val := range set {
		v.Set(k, val)
	}

	return v
}

// validVM is the smallest set of settings readVMOptions accepts.
func validVM() map[string]any {
	return map[string]any{
		"subscription-id": testSubscriptionID,
		"location":        "uksouth",
	}
}

const testSubscriptionID = "00000000-0000-0000-0000-000000000000"

// with returns a copy of base with extra applied on top.
func with(base, extra map[string]any) map[string]any {
	out := maps.Clone(base)
	maps.Copy(out, extra)

	return out
}

// mapEnv is a getenv that reads from m.
func mapEnv(m map[string]string) func(string) string {
	return func(key string) string { return m[key] }
}

// responseError returns an Azure response error as the SDK builds it.
func responseError(t *testing.T, status int, code string) error {
	t.Helper()

	resp := &http.Response{
		Status:     http.StatusText(status),
		StatusCode: status,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"code":"` + code + `","message":"denied"}}`)),
		Request:    httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://management.azure.com/subscriptions", nil),
	}

	return azruntime.NewResponseError(resp)
}

var errWrite = errors.New("write failed")

// errWriter is an io.Writer that always fails.
type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errWrite }

// sampleVMs covers each way a cell can be shown: a constrained-core SKU, GPUs,
// an unknown price, several quotas, no quota, Spot only, and each eviction
// value.
func sampleVMs() []vm.VM {
	return []vm.VM{
		{
			Name: "Standard_D4s_v5", Family: "standardDSv5Family",
			VCPUs: 4, VCPUsAvailable: 4, MemoryGiB: 16,
			Arch: vm.ArchX64, Category: vm.CategoryGeneral, Zones: []string{"1", "2", "3"},
			SpotCapable: true,
			Quota: []vm.QuotaHeadroom{
				{Name: "standardDSv5Family", Limit: 100, Used: 20, Free: 80},
				{Name: "cores", Limit: 50, Used: 10, Free: 40},
			},
			Pricing: vm.Pricing{OnDemandHourly: new(0.192), OnDemandMonthly: new(140.16)},
			Spot:    &vm.Spot{HourlyPrice: new(0.0384), EvictionRate: new("0-5")},
		},
		{
			Name: "Standard_E8-4s_v5", Family: "standardESv5Family",
			VCPUs: 8, VCPUsAvailable: 4, MemoryGiB: 64,
			Arch: vm.ArchX64, Category: vm.CategoryMemory, Zones: []string{},
			SpotCapable: true,
			Quota:       []vm.QuotaHeadroom{{Name: "standardESv5Family", Limit: 24, Used: 0, Free: 24}},
			Pricing:     vm.Pricing{OnDemandHourly: new(0.504), OnDemandMonthly: new(367.92)},
			Spot:        &vm.Spot{HourlyPrice: new(0.1008)},
		},
		{
			Name: "Standard_NC4as_T4_v3", Family: "standardNCASv3_T4Family",
			VCPUs: 4, VCPUsAvailable: 4, MemoryGiB: 28, GPUs: 1,
			Arch: vm.ArchX64, Category: vm.CategoryGPU, Zones: []string{"2"},
			SpotCapable: true, SpotOnly: true,
			Quota:   []vm.QuotaHeadroom{{Name: "standardNCASv3_T4Family", Limit: 0, Used: 0, Free: 0}},
			Pricing: vm.Pricing{OnDemandHourly: new(0.526), OnDemandMonthly: new(383.98)},
			Spot:    &vm.Spot{HourlyPrice: new(0.1052), EvictionRate: new("5-10")},
		},
		{
			Name: "Standard_D2pls_v5", Family: "standardDPLSv5Family",
			VCPUs: 2, VCPUsAvailable: 2, MemoryGiB: 4,
			Arch: vm.ArchARM64, Category: vm.CategoryGeneral, Zones: []string{},
			Quota: []vm.QuotaHeadroom{{Name: "standardDPLSv5Family", Limit: 10, Used: 10, Free: 0}},
		},
	}
}

// sampleResult is a result with every kind of VM, more eligible than shown,
// and SKUs left out for restrictions and quota.
func sampleResult() vm.Result {
	vms := sampleVMs()

	return vm.Result{
		Location: "uksouth",
		Summary: vm.Summary{
			Considered: 900, Matched: 12, Restricted: 2, NoQuota: 3,
			Eligible: 7, SpotOnly: 1, Shown: len(vms),
		},
		SpotQuota: &vm.QuotaHeadroom{Name: "lowPriorityCores", Limit: 100, Used: 4, Free: 96},
		VMs:       vms,
		Warnings:  []vm.Warning{},
	}
}

func TestIsTerminal(t *testing.T) {
	t.Parallel()

	file, err := os.Create(filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatalf("creating file: %v", err)
	}

	t.Cleanup(func() { _ = file.Close() })

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating pipe: %v", err)
	}

	t.Cleanup(func() {
		_ = r.Close()
		_ = w.Close()
	})

	// A true case needs a pseudo-terminal, which a unit test cannot open
	// portably.
	tests := []struct {
		name string
		w    io.Writer
	}{
		{name: "buffer", w: &bytes.Buffer{}},
		{name: "regular file", w: file},
		{name: "pipe", w: w},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if isTerminal(tc.w) {
				t.Errorf("isTerminal(%s) = true, want false", tc.name)
			}
		})
	}
}

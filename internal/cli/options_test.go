package cli

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/spf13/viper"

	"github.com/mike-guy/azskufinder/internal/output"
	"github.com/mike-guy/azskufinder/internal/vm"
)

// checkUsageError fails the test unless err is a usage error with message
// wantMsg and hint wantHint.
func checkUsageError(t *testing.T, err error, wantMsg, wantHint string) {
	t.Helper()

	var usageErr *usageError
	if !errors.As(err, &usageErr) {
		t.Fatalf("error = %v (%T), want a usage error", err, err)
	}

	if got := err.Error(); got != wantMsg {
		t.Errorf("error = %q, want %q", got, wantMsg)
	}

	if got := hintFor(err); got != wantHint {
		t.Errorf("hintFor(error) = %q, want %q", got, wantHint)
	}
}

// newSetViper returns a viper with no flags bound and value set for key, or
// nothing set when value is nil.
func newSetViper(key string, value any) *viper.Viper {
	v := viper.New()
	if value != nil {
		v.Set(key, value)
	}

	return v
}

func TestReadOptionalInt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value any
		want  *int
	}{
		{name: "unset", value: nil, want: nil},
		{name: "number", value: "4", want: new(4)},
		{name: "spaces", value: " 4 ", want: new(4)},
		{name: "zero is set", value: "0", want: new(0)},
		{name: "int value", value: 8, want: new(8)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := readOptionalInt(newSetViper("cpus", tc.value), "cpus")
			if err != nil {
				t.Fatalf("readOptionalInt(%v) error = %v", tc.value, err)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("readOptionalInt(%v) mismatch (-want +got):\n%s", tc.value, diff)
			}
		})
	}
}

func TestReadOptionalInt_errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		wantMsg string
	}{
		{name: "not a number", value: "abc", wantMsg: `--cpus must be a whole number, got "abc"`},
		{name: "decimal", value: "1.5", wantMsg: `--cpus must be a whole number, got "1.5"`},
		{name: "negative", value: "-1", wantMsg: "--cpus must not be negative, got -1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := readOptionalInt(newSetViper("cpus", tc.value), "cpus")
			checkUsageError(t, err, tc.wantMsg, "")
		})
	}
}

func TestReadOptionalFloat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value any
		want  *float64
	}{
		{name: "unset", value: nil, want: nil},
		{name: "whole", value: "16", want: new(16.0)},
		{name: "decimal", value: " 1.5 ", want: new(1.5)},
		{name: "zero is set", value: "0", want: new(0.0)},
		{name: "at the bound", value: "1e9", want: new(1e9)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := readOptionalFloat(newSetViper("memory", tc.value), "memory")
			if err != nil {
				t.Fatalf("readOptionalFloat(%v) error = %v", tc.value, err)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("readOptionalFloat(%v) mismatch (-want +got):\n%s", tc.value, diff)
			}
		})
	}
}

func TestReadOptionalFloat_errors(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"abc", "-1", "NaN", "Inf", "1e10"} {
		t.Run(strings.ToLower(value), func(t *testing.T) {
			t.Parallel()

			_, err := readOptionalFloat(newSetViper("memory", value), "memory")
			checkUsageError(t, err, `--memory must be a number of at least 0, got "`+value+`"`, "")
		})
	}
}

func TestReadBool(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value any
		want  bool
	}{
		{name: "unset", value: nil, want: false},
		{name: "true", value: "true", want: true},
		{name: "one", value: "1", want: true},
		{name: "false with spaces", value: " false ", want: false},
		{name: "bool value", value: true, want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := readBool(newSetViper("debug", tc.value), "debug")
			if err != nil {
				t.Fatalf("readBool(%v) error = %v", tc.value, err)
			}

			if got != tc.want {
				t.Errorf("readBool(%v) = %t, want %t", tc.value, got, tc.want)
			}
		})
	}
}

func TestReadBool_error(t *testing.T) {
	t.Parallel()

	_, err := readBool(newSetViper("debug", "yes"), "debug")
	checkUsageError(t, err, `--debug must be true or false, got "yes"`, "")
}

func TestReadVMOptions(t *testing.T) {
	t.Parallel()

	// defaults is what the flag defaults give with only the required
	// settings.
	defaults := func() vmOptions {
		return vmOptions{
			commonOptions: commonOptions{
				SubscriptionID: testSubscriptionID,
				Location:       "uksouth",
				Output:         output.FormatTable,
				Currency:       "GBP",
				Limit:          25,
				Timeout:        2 * time.Minute,
			},
			Filter: vm.Filter{OS: vm.OSLinux, Sort: vm.Sort{Key: vm.SortMonthly}, Limit: 25},
		}
	}

	tests := []struct {
		name string
		set  map[string]any
		want func(o *vmOptions)
	}{
		{
			name: "defaults",
			set:  validVM(),
			want: func(*vmOptions) {},
		},
		{
			name: "trimmed and normalised",
			set: map[string]any{
				"subscription-id": " " + testSubscriptionID + " ",
				"location":        " UKSouth ",
				"currency":        " usd ",
				"output":          "JSON",
			},
			want: func(o *vmOptions) {
				o.Currency = "USD"
				o.Output = output.FormatJSON
			},
		},
		{
			name: "no limit",
			set:  with(validVM(), map[string]any{"limit": 0}),
			want: func(o *vmOptions) {
				o.Limit = 0
				o.Filter.Limit = 0
			},
		},
		{
			name: "exact requirements",
			set:  with(validVM(), map[string]any{"cpus": 4, "memory": "16", "gpus": 0, "exact-match": true}),
			want: func(o *vmOptions) {
				o.Filter.CPUs = new(4)
				o.Filter.MemoryGiB = new(16.0)
				o.Filter.GPUs = new(0)
				o.Filter.ExactMatch = true
			},
		},
		{
			name: "timeout",
			set:  with(validVM(), map[string]any{"timeout": " 90s "}),
			want: func(o *vmOptions) { o.Timeout = 90 * time.Second },
		},
		{
			name: "no timeout",
			set:  with(validVM(), map[string]any{"timeout": "0"}),
			want: func(o *vmOptions) { o.Timeout = 0 },
		},
		{
			name: "windows",
			set:  with(validVM(), map[string]any{"os": "Windows"}),
			want: func(o *vmOptions) { o.Filter.OS = vm.OSWindows },
		},
		{
			name: "arm64",
			set:  with(validVM(), map[string]any{"arch": "arm64"}),
			want: func(o *vmOptions) { o.Filter.Arch = vm.ArchARM64 },
		},
		{
			name: "categories",
			set:  with(validVM(), map[string]any{"category": "memory,gpu"}),
			want: func(o *vmOptions) { o.Filter.Categories = []vm.Category{vm.CategoryMemory, vm.CategoryGPU} },
		},
		{
			name: "sort descending",
			set:  with(validVM(), map[string]any{"sort-by": "memory:desc"}),
			want: func(o *vmOptions) { o.Filter.Sort = vm.Sort{Key: vm.SortMemory, Desc: true} },
		},
		{
			name: "sort by spot with spot",
			set:  with(validVM(), map[string]any{"sort-by": "spot", "include-spot": "true"}),
			want: func(o *vmOptions) {
				o.Filter.Sort = vm.Sort{Key: vm.SortSpot}
				o.Filter.IncludeSpot = true
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			want := defaults()
			tc.want(&want)

			got, err := readVMOptions(vmViper(t, tc.set))
			if err != nil {
				t.Fatalf("readVMOptions(%v) error = %v", tc.set, err)
			}

			if diff := cmp.Diff(want, got, cmp.AllowUnexported(vmOptions{})); diff != "" {
				t.Errorf("readVMOptions(%v) mismatch (-want +got):\n%s", tc.set, diff)
			}
		})
	}
}

func TestReadVMOptions_errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		set      map[string]any
		wantMsg  string
		wantHint string
	}{
		{
			name:     "missing subscription",
			set:      map[string]any{"location": "uksouth"},
			wantMsg:  "--subscription-id is required",
			wantHint: "Use --subscription-id or set AZSKUFINDER_SUBSCRIPTION_ID.",
		},
		{
			name:    "malformed subscription",
			set:     with(validVM(), map[string]any{"subscription-id": "my-sub"}),
			wantMsg: `--subscription-id must look like 00000000-0000-0000-0000-000000000000, got "my-sub"`,
		},
		{
			name:     "missing location",
			set:      map[string]any{"subscription-id": testSubscriptionID},
			wantMsg:  "--location is required",
			wantHint: "Use --location or set AZSKUFINDER_LOCATION.",
		},
		{
			name:    "bad location quotes the input",
			set:     with(validVM(), map[string]any{"location": "UK South"}),
			wantMsg: `--location must be an Azure location name such as uksouth, got "UK South"`,
		},
		{
			name:    "bad output",
			set:     with(validVM(), map[string]any{"output": "xml"}),
			wantMsg: `--output: unknown output format "xml": use one of table, json`,
		},
		{
			name:    "bad currency",
			set:     with(validVM(), map[string]any{"currency": "pounds"}),
			wantMsg: `--currency must be one of GBP, USD, EUR, got "pounds"`,
		},
		{
			name:    "unsupported currency",
			set:     with(validVM(), map[string]any{"currency": "XYZ"}),
			wantMsg: `--currency must be one of GBP, USD, EUR, got "XYZ"`,
		},
		{
			name:    "negative limit",
			set:     with(validVM(), map[string]any{"limit": "-1"}),
			wantMsg: "--limit must not be negative, got -1",
		},
		{
			name:    "timeout without a unit",
			set:     with(validVM(), map[string]any{"timeout": 30}),
			wantMsg: `--timeout must be a duration such as 90s or 2m, got "30"`,
		},
		{
			name:    "bad timeout",
			set:     with(validVM(), map[string]any{"timeout": "abc"}),
			wantMsg: `--timeout must be a duration such as 90s or 2m, got "abc"`,
		},
		{
			name:    "bad os",
			set:     with(validVM(), map[string]any{"os": "macos"}),
			wantMsg: `--os: unknown operating system "macos": use linux or windows`,
		},
		{
			name:    "bad include-spot",
			set:     with(validVM(), map[string]any{"include-spot": "yes"}),
			wantMsg: `--include-spot must be true or false, got "yes"`,
		},
		{
			name:    "bad sort",
			set:     with(validVM(), map[string]any{"sort-by": "price"}),
			wantMsg: `--sort-by: unknown column "price", choose one of: ` + strings.Join(vm.SortKeyNames(), ", "),
		},
		{
			name:     "sort by eviction without spot",
			set:      with(validVM(), map[string]any{"sort-by": "eviction"}),
			wantMsg:  "--sort-by eviction needs Spot data",
			wantHint: "Add --include-spot.",
		},
		{
			name:    "bad cpus",
			set:     with(validVM(), map[string]any{"cpus": "four"}),
			wantMsg: `--cpus must be a whole number, got "four"`,
		},
		{
			name:    "bad memory",
			set:     with(validVM(), map[string]any{"memory": "lots"}),
			wantMsg: `--memory must be a number of at least 0, got "lots"`,
		},
		{
			name:    "bad gpus",
			set:     with(validVM(), map[string]any{"gpus": "-2"}),
			wantMsg: "--gpus must not be negative, got -2",
		},
		{
			name:    "bad exact-match",
			set:     with(validVM(), map[string]any{"exact-match": "maybe"}),
			wantMsg: `--exact-match must be true or false, got "maybe"`,
		},
		{
			name:    "bad arch",
			set:     with(validVM(), map[string]any{"arch": "riscv"}),
			wantMsg: `--arch: unknown architecture "riscv": use one of any, x64, arm64`,
		},
		{
			name:    "bad category",
			set:     with(validVM(), map[string]any{"category": "memory,fast"}),
			wantMsg: `--category: unknown category "fast": use one of ` + strings.Join(vm.CategoryNames(), ", "),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := readVMOptions(vmViper(t, tc.set))
			checkUsageError(t, err, tc.wantMsg, tc.wantHint)
		})
	}
}

// TestReadVMOptions_flags reads options from parsed flags, as RunE does, rather
// than from v.Set, which always counts as set. It checks that a number given
// as 0 is told apart from one not given, which relies on viper's IsSet
// treating an unchanged flag as unset. It reads the environment, so it cannot
// run in parallel.
func TestReadVMOptions_flags(t *testing.T) { //nolint:paralleltest,tparallel // isolateEnv uses t.Setenv
	isolateEnv(t)

	tests := []struct {
		name      string
		args      []string
		config    string
		wantGPUs  *int
		wantLimit int
	}{
		{name: "gpus not given", wantLimit: defaultLimit},
		{name: "gpus flag of zero", args: []string{"--gpus", "0"}, wantGPUs: new(0), wantLimit: defaultLimit},
		{name: "gpus of zero in config file", config: "gpus: 0\n", wantGPUs: new(0), wantLimit: defaultLimit},
		{name: "limit flag of zero", args: []string{"--limit", "0"}, wantLimit: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root, _, _ := newTestRoot(t)
			cmd := vmCmd(t, root)

			args := append([]string{
				"--config", writeConfig(t, tc.config),
				"--subscription-id", testSubscriptionID,
				"--location", "uksouth",
			}, tc.args...)
			if err := cmd.ParseFlags(args); err != nil {
				t.Fatalf("ParseFlags(%q) error = %v", args, err)
			}

			v := viper.New()
			if _, err := setupViper(v, cmd); err != nil {
				t.Fatalf("setupViper() error = %v", err)
			}

			got, err := readVMOptions(v)
			if err != nil {
				t.Fatalf("readVMOptions() error = %v", err)
			}

			if diff := cmp.Diff(tc.wantGPUs, got.Filter.GPUs); diff != "" {
				t.Errorf("readVMOptions() GPUs mismatch (-want +got):\n%s", diff)
			}

			if got.Limit != tc.wantLimit || got.Filter.Limit != tc.wantLimit {
				t.Errorf("readVMOptions() Limit = %d, Filter.Limit = %d, want %d", got.Limit, got.Filter.Limit, tc.wantLimit)
			}
		})
	}
}

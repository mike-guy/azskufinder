package cli

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/spf13/viper"

	"github.com/mike-guy/azskufinder/internal/app"
	"github.com/mike-guy/azskufinder/internal/output"
	"github.com/mike-guy/azskufinder/internal/vm"
)

// writeConfig writes a config file into a new temporary directory and returns
// its path.
func writeConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	return path
}

func TestReadConfigFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		configYAML  string // the file --config names; "" means no --config
		hasDefault  bool   // whether there is a home directory
		defaultYAML string // the file at the default path; "" means none
		want        string // which file is read: "config", "default" or ""
		wantLoc     string
	}{
		{name: "explicit file", configYAML: "location: westeurope\n", want: "config", wantLoc: "westeurope"},
		{name: "default file missing", hasDefault: true},
		{name: "default file present", hasDefault: true, defaultYAML: "location: northeurope\n", want: "default", wantLoc: "northeurope"},
		{name: "no home directory"},
		{
			name:       "explicit file wins over default",
			configYAML: "location: westeurope\n", hasDefault: true, defaultYAML: "location: northeurope\n",
			want: "config", wantLoc: "westeurope",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			v := viper.New()
			paths := map[string]string{"": ""}

			if tc.configYAML != "" {
				paths["config"] = writeConfig(t, tc.configYAML)
				v.Set("config", paths["config"])
			}

			var defaultPath string

			switch {
			case tc.defaultYAML != "":
				defaultPath = writeConfig(t, tc.defaultYAML)
			case tc.hasDefault:
				defaultPath = filepath.Join(t.TempDir(), "config.yaml")
			}

			paths["default"] = defaultPath

			got, err := readConfigFile(v, defaultPath)
			if err != nil {
				t.Fatalf("readConfigFile() error = %v", err)
			}

			if want := paths[tc.want]; got != want {
				t.Errorf("readConfigFile() = %q, want %q (the %s file)", got, want, tc.want)
			}

			if loc := v.GetString("location"); loc != tc.wantLoc {
				t.Errorf("location after readConfigFile() = %q, want %q", loc, tc.wantLoc)
			}
		})
	}
}

func TestReadConfigFile_explicitMissing(t *testing.T) {
	t.Parallel()

	v := viper.New()
	v.Set("config", filepath.Join(t.TempDir(), "missing.yaml"))

	_, err := readConfigFile(v, writeConfig(t, "location: northeurope\n"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("readConfigFile() error = %v, want one wrapping fs.ErrNotExist", err)
	}

	var usageErr *usageError
	if !errors.As(err, &usageErr) {
		t.Errorf("readConfigFile() error = %v, want a usage error", err)
	}
}

func TestReadConfigFile_invalidYAML(t *testing.T) {
	t.Parallel()

	bad := writeConfig(t, "location: [uksouth\n")

	tests := []struct {
		name     string
		explicit bool
		wantHint string
	}{
		{name: "named by --config", explicit: true},
		{name: "default file", wantHint: "Fix or remove " + bad + "."},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			v := viper.New()
			defaultPath := bad

			if tc.explicit {
				v.Set("config", bad)
				defaultPath = ""
			}

			_, err := readConfigFile(v, defaultPath)

			var usageErr *usageError
			if !errors.As(err, &usageErr) {
				t.Fatalf("readConfigFile() error = %v, want a usage error", err)
			}

			if got := hintFor(err); got != tc.wantHint {
				t.Errorf("hintFor(readConfigFile()) = %q, want %q", got, tc.wantHint)
			}
		})
	}
}

// TestSetupViper_precedence checks flag over environment over config file over
// default. It sets environment variables, so it cannot run in parallel.
func TestSetupViper_precedence(t *testing.T) {
	isolateEnv(t)
	t.Setenv("AZSKUFINDER_LOCATION", "northeurope")
	t.Setenv("AZSKUFINDER_CURRENCY", "EUR")
	t.Setenv("AZSKUFINDER_INCLUDE_SPOT", "true")

	config := writeConfig(t, "location: westeurope\ncurrency: USD\nlimit: 10\n")

	root := newRoot(app.New("test"))
	if err := root.ParseFlags([]string{"--location", "uksouth", "--config", config}); err != nil {
		t.Fatalf("ParseFlags() error = %v", err)
	}

	v := viper.New()

	got, err := setupViper(v, root)
	if err != nil {
		t.Fatalf("setupViper() error = %v", err)
	}

	if got != config {
		t.Errorf("setupViper() = %q, want %q", got, config)
	}

	tests := []struct {
		key, want, from string
	}{
		{key: "location", want: "uksouth", from: "the flag"},
		{key: "currency", want: "EUR", from: "the environment"},
		{key: "limit", want: "10", from: "the config file"},
		{key: "output", want: output.FormatTable, from: "the default"},
		{key: "include-spot", want: "true", from: "AZSKUFINDER_INCLUDE_SPOT"},
	}

	for _, tc := range tests {
		if got := v.GetString(tc.key); got != tc.want {
			t.Errorf("%s = %q, want %q from %s", tc.key, got, tc.want, tc.from)
		}
	}
}

func TestSortCompletions(t *testing.T) {
	t.Parallel()

	keys := []string{"name", "memory"}

	got := sortCompletions(keys)

	want := []string{"name", "memory", "name:desc", "memory:desc"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("sortCompletions() mismatch (-want +got):\n%s", diff)
	}

	if diff := cmp.Diff([]string{"name", "memory"}, keys); diff != "" {
		t.Errorf("sortCompletions() changed its input (-want +got):\n%s", diff)
	}
}

func TestEnvName(t *testing.T) {
	t.Parallel()

	if got, want := envName("subscription-id"), "AZSKUFINDER_SUBSCRIPTION_ID"; got != want {
		t.Errorf("envName(%q) = %q, want %q", "subscription-id", got, want)
	}
}

// TestFlagCompletions_parse checks that every value offered when completing a
// flag is one its parser accepts, so the two cannot drift apart.
func TestFlagCompletions_parse(t *testing.T) {
	t.Parallel()

	parseErr := func(ok bool) error {
		if !ok {
			return errors.New("not valid")
		}

		return nil
	}

	tests := []struct {
		flag  string
		parse func(string) error
	}{
		{flag: "output", parse: func(s string) error { _, err := output.ParseFormat(s); return err }},
		{flag: "currency", parse: func(s string) error { return parseErr(slices.Contains(currencies, s)) }},
		{flag: "os", parse: func(s string) error { _, err := vm.ParseOS(s); return err }},
		{flag: "sort-by", parse: func(s string) error { _, err := vm.ParseSort(s); return err }},
		{flag: "arch", parse: func(s string) error { _, err := vm.ParseArch(s); return err }},
		{flag: "category", parse: func(s string) error { _, err := vm.ParseCategories(s); return err }},
	}

	for _, tc := range tests {
		t.Run(tc.flag, func(t *testing.T) {
			t.Parallel()

			root, _, _ := newTestRoot(t)
			cmd := vmCmd(t, root)

			complete, ok := cmd.GetFlagCompletionFunc(tc.flag)
			if !ok {
				t.Fatalf("no completion registered for --%s", tc.flag)
			}

			values, _ := complete(cmd, nil, "")
			if len(values) == 0 {
				t.Fatalf("completion for --%s offered nothing", tc.flag)
			}

			for _, value := range values {
				if err := tc.parse(value); err != nil {
					t.Errorf("completion for --%s offers %q, which its parser rejects: %v", tc.flag, value, err)
				}
			}
		})
	}
}

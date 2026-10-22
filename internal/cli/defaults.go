package cli

import (
	"os"
	"path/filepath"
	"time"

	"github.com/mike-guy/azskufinder/internal/output"
	"github.com/mike-guy/azskufinder/internal/vm"
)

const envPrefix = "AZSKUFINDER"

const (
	defaultOutput   = output.FormatTable
	defaultCurrency = "GBP"
	defaultLimit    = 25
	defaultTimeout  = 2 * time.Minute
	defaultOS       = string(vm.OSLinux)
)

// currencies are the --currency values accepted. The Retail Prices API takes
// more, but an unsupported code fails only after a slow request, so the list
// is kept to these.
var currencies = []string{"GBP", "USD", "EUR"}

// defaultConfigPath is where the viper config file lives unless --config says
// otherwise: ~/.azskufinder/config.yaml on every OS, next to ~/.azure.
func defaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, ".azskufinder", "config.yaml"), nil
}

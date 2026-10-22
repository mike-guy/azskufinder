package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// setupViper binds the flags, environment and config file to v. It returns the
// path of the config file it read, or "" when there was none.
func setupViper(v *viper.Viper, cmd *cobra.Command) (string, error) {
	// Tell Viper to read environment variables (e.g., AZSKUFINDER_LOCATION)
	v.SetEnvPrefix(envPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.AutomaticEnv()

	// Bind all Cobra flags to Viper
	if err := v.BindPFlags(cmd.Flags()); err != nil {
		return "", fmt.Errorf("binding flags: %w", err)
	}

	// No home directory means no default config file.
	defaultPath, err := defaultConfigPath()
	if err != nil {
		defaultPath = ""
	}

	return readConfigFile(v, defaultPath)
}

// readConfigFile reads the config file named by --config, or the one at
// defaultPath if it exists. defaultPath is "" when there is no config
// directory. It returns the path it read, or "" when there was none.
func readConfigFile(v *viper.Viper, defaultPath string) (string, error) {
	path := strings.TrimSpace(v.GetString("config"))
	explicit := path != ""

	if !explicit {
		if defaultPath == "" {
			return "", nil
		}

		path = defaultPath
	}

	v.SetConfigFile(path)

	err := v.ReadInConfig()

	switch {
	case err == nil:
		return path, nil
	case !explicit && errors.Is(err, fs.ErrNotExist):
		return "", nil
	case explicit:
		return "", usageErrorf("reading config file: %w", err)
	default:
		// The default file is read without being asked for, so say which it is.
		return "", withHint(usageErrorf("reading config file: %w", err), "Fix or remove "+path+".")
	}
}

// registerFixed offers a fixed set of values when completing a flag.
func registerFixed(cmd *cobra.Command, flag string, values ...string) {
	_ = cmd.RegisterFlagCompletionFunc(flag, cobra.FixedCompletions(values, cobra.ShellCompDirectiveNoFileComp))
}

// sortCompletions offers each sort key for --sort-by, and each key in
// descending order: "memory", ..., "memory:desc", ...
func sortCompletions(keys []string) []string {
	values := slices.Clone(keys)

	for _, k := range keys {
		values = append(values, k+":desc")
	}

	return values
}

// envName returns the environment variable that sets a flag.
func envName(flag string) string {
	return envPrefix + "_" + strings.ToUpper(strings.ReplaceAll(flag, "-", "_"))
}

// usage appends the environment variable to a flag's help text.
func usage(flag, text string) string {
	return text + " (env: " + envName(flag) + ")"
}

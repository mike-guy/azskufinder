package cli

import (
	"github.com/spf13/cobra"

	"github.com/mike-guy/azskufinder/internal/app"
)

func newSQLDBCmd(application *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "db",
		Short: "Find deployable Azure SQL Database SKUs",
		Long: `Find Azure SQL Database SKUs that meet your
requirements and can be deployed, by purchasing model:

  * 'sql db vcore' for the vCore model: General Purpose, Business Critical and
    Hyperscale, provisioned or serverless, sized by vCores and hardware
  * 'sql db dtu' for the DTU model: Basic, Standard and Premium, sized by DTUs

Each has its own flags, table and JSON output, as the two models share
little beyond a name, a tier and a price.`,
		Example: `  azskufinder sql db vcore --location uksouth --vcores 4
  azskufinder sql db dtu --location uksouth --dtus 100`,
		// As on the root: an unknown model is a usage error, and no model
		// shows the help.
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newSQLDBVCoreCmd(application), newSQLDBDTUCmd(application))

	return cmd
}

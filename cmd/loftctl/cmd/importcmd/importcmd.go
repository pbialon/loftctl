package importcmd

import (
	"github.com/loft-sh/loftctl/v3/cmd/loftctl/flags"
	"github.com/loft-sh/loftctl/v3/pkg/upgrade"
	"github.com/spf13/cobra"
)

// NewImportCmd creates a new command
func NewImportCmd(globalFlags *flags.GlobalFlags) *cobra.Command {
	description := `
#######################################################
###################### loft import ####################
#######################################################

Imports a specified resource into a Loft project.
	`
	if upgrade.IsPlugin == "true" {
		description = `
#######################################################
#################### devspace import ##################
#######################################################

Imports a specified resource into a Loft project.
	`
	}
	importCmd := &cobra.Command{
		Use:   "import",
		Short: "Imports loft resources",
		Long:  description,
		Args:  cobra.NoArgs,
	}

	importCmd.AddCommand(NewVClusterCmd(globalFlags))
	importCmd.AddCommand(NewSpaceCmd(globalFlags))
	return importCmd
}

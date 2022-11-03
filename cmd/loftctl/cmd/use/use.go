package use

import (
	"github.com/loft-sh/loftctl/v2/cmd/loftctl/flags"
	"github.com/loft-sh/loftctl/v2/pkg/upgrade"
	"github.com/spf13/cobra"
)

// NewUseCmd creates a new cobra command
func NewUseCmd(globalFlags *flags.GlobalFlags) *cobra.Command {
	description := `
#######################################################
###################### loft use #######################
#######################################################

Activates a kube context for the given cluster / space / vcluster / management.
	`
	if upgrade.IsPlugin == "true" {
		description = `
#######################################################
#################### devspace use #####################
#######################################################

Activates a kube context for the given cluster / space / vcluster / management.
	`
	}
	useCmd := &cobra.Command{
		Use:   "use",
		Short: "Uses loft resources",
		Long:  description,
		Args:  cobra.NoArgs,
	}

	useCmd.AddCommand(NewClusterCmd(globalFlags))
	useCmd.AddCommand(NewManagementCmd(globalFlags))
	useCmd.AddCommand(NewSpaceCmd(globalFlags))
	useCmd.AddCommand(NewVirtualClusterCmd(globalFlags))
	return useCmd
}

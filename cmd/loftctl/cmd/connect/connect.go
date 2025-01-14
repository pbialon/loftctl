package connect

import (
	"github.com/loft-sh/loftctl/v3/cmd/loftctl/flags"
	"github.com/loft-sh/loftctl/v3/pkg/upgrade"
	"github.com/spf13/cobra"
)

// NewConnectCmd creates a new command
func NewConnectCmd(globalFlags *flags.GlobalFlags) *cobra.Command {
	description := `
#######################################################
#################### loft connect #####################
#######################################################
	`
	if upgrade.IsPlugin == "true" {
		description = `
#######################################################
################### devspace connect ##################
#######################################################
	`
	}
	connectCmd := &cobra.Command{
		Use:   "connect",
		Short: "Connects a cluster to Loft",
		Long:  description,
		Args:  cobra.NoArgs,
	}

	connectCmd.AddCommand(NewClusterCmd(globalFlags))
	return connectCmd
}

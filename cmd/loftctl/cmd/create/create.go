package create

import (
	"github.com/loft-sh/loftctl/v3/cmd/loftctl/flags"
	pdefaults "github.com/loft-sh/loftctl/v3/pkg/defaults"
	"github.com/loft-sh/loftctl/v3/pkg/upgrade"
	"github.com/spf13/cobra"
)

// NewCreateCmd creates a new cobra command
func NewCreateCmd(globalFlags *flags.GlobalFlags, defaults *pdefaults.Defaults) *cobra.Command {
	description := `
#######################################################
##################### loft create #####################
#######################################################
	`
	if upgrade.IsPlugin == "true" {
		description = `
#######################################################
##################### loft create #####################
#######################################################
	`
	}
	c := &cobra.Command{
		Use:   "create",
		Short: "Creates loft resources",
		Long:  description,
		Args:  cobra.NoArgs,
	}
	c.AddCommand(NewSpaceCmd(globalFlags, defaults), NewVirtualClusterCmd(globalFlags, defaults))
	return c
}

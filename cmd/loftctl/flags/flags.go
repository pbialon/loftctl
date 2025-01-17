package flags

import (
	"github.com/loft-sh/loftctl/v3/pkg/client"
	flag "github.com/spf13/pflag"
)

// GlobalFlags is the flags that contains the global flags
type GlobalFlags struct {
	Silent    bool
	Debug     bool
	Config    string
	LogOutput string
}

// SetGlobalFlags applies the global flags
func SetGlobalFlags(flags *flag.FlagSet) *GlobalFlags {
	globalFlags := &GlobalFlags{}

	flags.StringVar(&globalFlags.LogOutput, "log-output", "plain", "The log format to use. Can be either plain, raw or json")
	flags.StringVar(&globalFlags.Config, "config", client.DefaultCacheConfig, "The loft config to use (will be created if it does not exist)")
	flags.BoolVar(&globalFlags.Debug, "debug", false, "Prints the stack trace if an error occurs")
	flags.BoolVar(&globalFlags.Silent, "silent", false, "Run in silent mode and prevents any loft log output except panics & fatals")

	return globalFlags
}

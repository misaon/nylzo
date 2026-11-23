//go:build !windows

package operator

import (
	"flag"

	"github.com/misaon/nylzo/internal/cli/command"
)

func requireSupportedSystem() error {
	return nil
}

func parseHostFlags(flags *flag.FlagSet, environment *command.Environment, arguments []string) (int, bool) {
	return command.ParseFlags(flags, environment, arguments)
}

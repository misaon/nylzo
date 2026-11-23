package operator

import (
	"flag"

	"github.com/misaon/nylzo/internal/cli/command"
)

// The server needs a directory fsync and free space checks, which Windows does not offer.
func requireSupportedSystem() error {
	return errServerUnsupported
}

func parseHostFlags(flags *flag.FlagSet, environment *command.Environment, arguments []string) (int, bool) {
	if code, done := command.ParseFlags(flags, environment, arguments); done {
		return code, true
	}
	return command.Failf(environment, "%s", errServerUnsupported), true
}

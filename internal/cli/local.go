package cli

import (
	"context"
	"flag"
	"fmt"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/cli/local"
	"github.com/misaon/nylzo/internal/cli/messages"
	"github.com/misaon/nylzo/internal/client"
	"github.com/misaon/nylzo/internal/server/operatoraddress"
)

type localOptions struct {
	server    string
	noBrowser bool
}

func localFlags(environment *command.Environment, name string) (*flag.FlagSet, *localOptions) {
	options := &localOptions{}
	flags := command.NewFlags(environment, name)
	flags.StringVar(
		&options.server,
		"server",
		"",
		"the instance, such as https://nylzo.example (or "+command.EnvironmentServer+")",
	)
	flags.BoolVar(&options.noBrowser, "no-browser", false, "print the address instead of opening a browser")
	return flags, options
}

func runWeb(ctx context.Context, environment *command.Environment, arguments []string) int {
	flags, options := localFlags(environment, "web")
	if code, done := command.FlagsDone(command.ParseFlags(flags, environment, arguments)); done {
		return code
	}
	address, ok := command.ServerAddress(environment, "web", options.server)
	if !ok {
		return command.ExitUsage
	}
	instance, err := localInstance(address)
	if err != nil {
		return command.Failf(environment, "%s", command.Explain(err))
	}
	build, err := environment.LocalWebClient()
	if err != nil {
		return command.Failf(environment, "%s", err)
	}
	return local.Serve(ctx, environment, local.Options{
		Instance:    instance,
		Build:       build,
		OpenBrowser: !options.noBrowser,
	})
}

// runOpen hands a link to the local client in memory, so it reaches neither an argument nor a browser address.
func runOpen(ctx context.Context, environment *command.Environment, arguments []string) int {
	flags, options := localFlags(environment, "open")
	linkFile := flags.String("link-file", "", "read the link from this file (or "+command.EnvironmentLink+", or stdin)")
	if code, done := command.FlagsDone(command.ParseFlags(flags, environment, arguments)); done {
		return code
	}
	address, ok := command.ServerAddress(environment, "open", options.server)
	if !ok {
		return command.ExitUsage
	}
	instance, err := localInstance(address)
	if err != nil {
		return command.Failf(environment, "%s", command.Explain(err))
	}
	raw, err := messages.ReadLink(environment, *linkFile)
	if err != nil {
		return command.Failf(environment, "link: %s", command.Explain(err))
	}
	link, err := messages.HandOver(raw, instance)
	if err != nil {
		return command.Failf(environment, "link: %s", command.Explain(err))
	}
	build, err := environment.LocalWebClient()
	if err != nil {
		return command.Failf(environment, "%s", err)
	}
	return local.Serve(ctx, environment, local.Options{
		Instance:    instance,
		Build:       build,
		OpenBrowser: !options.noBrowser,
		Link:        &local.Link{Path: link.Path, Fragment: link.Fragment, Lifetime: local.HandoffLifetime},
	})
}

// localInstance names the instance the local client relays to; its pages carry it, so it follows the rules of a public URL.
func localInstance(server string) (string, error) {
	instance, err := client.Origin(server)
	if err != nil {
		return "", err
	}
	if err = operatoraddress.CheckPublicURL(instance); err != nil {
		return "", fmt.Errorf("the instance address %s: %w", instance, err)
	}
	return instance, nil
}

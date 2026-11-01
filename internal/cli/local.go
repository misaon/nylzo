package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"strings"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/cli/local"
	"github.com/misaon/nylzo/internal/client"
)

type localOptions struct {
	server    string
	noBrowser bool
}

func localFlags(environment *command.Environment, name string) (*flag.FlagSet, *localOptions) {
	options := &localOptions{}
	flags := command.NewFlags(environment, name)
	flags.StringVar(&options.server, "server", "", "the instance, such as https://nylzo.example (or "+envServer+")")
	flags.BoolVar(&options.noBrowser, "no-browser", false, "print the address instead of opening a browser")
	return flags, options
}

func runWeb(ctx context.Context, environment *command.Environment, arguments []string) int {
	flags, options := localFlags(environment, "web")
	if code, done := command.FlagsDone(command.ParseFlags(flags, environment, arguments)); done {
		return code
	}
	if options.server == "" {
		options.server = environment.Getenv(envServer)
	}
	if options.server == "" {
		fmt.Fprintln(environment.Stderr, command.Program+" web needs --server or "+envServer)
		return command.ExitUsage
	}
	instance, err := client.Origin(options.server)
	if err != nil {
		return command.Failf(environment, "%s", explain(err))
	}
	build, err := webClient(environment)
	if err != nil || !build.Built {
		return command.Failf(environment, "%s", errNoLocalClient)
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
	linkFile := flags.String("link-file", "", "read the link from this file (or "+envLink+", or stdin)")
	if code, done := command.FlagsDone(command.ParseFlags(flags, environment, arguments)); done {
		return code
	}
	if options.server == "" {
		options.server = environment.Getenv(envServer)
	}
	if options.server == "" {
		fmt.Fprintln(environment.Stderr, command.Program+" open needs --server or "+envServer)
		return command.ExitUsage
	}
	instance, err := client.Origin(options.server)
	if err != nil {
		return command.Failf(environment, "%s", explain(err))
	}
	raw, err := readLink(environment, *linkFile)
	if err != nil {
		return command.Failf(environment, "link: %s", explain(err))
	}
	link, err := handedOverLink(raw, instance)
	if err != nil {
		return command.Failf(environment, "link: %s", explain(err))
	}
	build, err := webClient(environment)
	if err != nil || !build.Built {
		return command.Failf(environment, "%s", errNoLocalClient)
	}
	return local.Serve(ctx, environment, local.Options{
		Instance:    instance,
		Build:       build,
		OpenBrowser: !options.noBrowser,
		Link:        &local.Link{Path: link.path, Fragment: link.fragment, Lifetime: local.HandoffLifetime},
	})
}

type handedOver struct {
	path     string
	fragment string
}

func handedOverLink(raw, instance string) (handedOver, error) {
	parsed, err := parseOpenLink(raw)
	if err != nil {
		return handedOver{}, err
	}
	parsed.wipe()
	if parsed.origin != instance {
		return handedOver{}, errors.New("the link belongs to another instance than --server or " + envServer)
	}
	head, fragment, _ := strings.Cut(raw, "#")
	address, err := url.Parse(head)
	if err != nil {
		return handedOver{}, errors.New("the link is incomplete or damaged")
	}
	return handedOver{path: address.EscapedPath(), fragment: fragment}, nil
}

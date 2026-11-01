package cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os/signal"
	"regexp"
	"slices"
	"strings"
	"syscall"

	"github.com/misaon/nylzo/internal/buildinfo"
	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/cli/operator"
	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server"
	"github.com/misaon/nylzo/internal/server/webclient"
)

type subcommand struct {
	name    string
	summary string
	run     func(context.Context, *command.Environment, []string) int
}

var (
	fragmentLike = regexp.MustCompile(
		fmt.Sprintf(
			`#[0-9]+\.[0-9]+\.(?:[A-Za-z0-9_-]{%d}\.)?[A-Za-z0-9_-]{%d}`,
			base64.RawURLEncoding.EncodedLen(core.IDLen),
			base64.RawURLEncoding.EncodedLen(core.SecretLen),
		),
	)
	commandLike = regexp.MustCompile(`^[a-z][a-z-]{0,19}$`)
)

func webClient(environment *command.Environment) (webclient.Build, error) {
	if environment.WebClient != nil {
		return environment.WebClient()
	}
	return server.EmbeddedWebClient()
}

func commands() []subcommand {
	return []subcommand{
		{"create", "encrypt text from stdin or a file and print its link", runCreate},
		{"read", "read a link from stdin and decrypt the message into private files", runRead},
		{"open", "open a link from stdin in the browser with the verified web client of this nylzo", runOpen},
		{"web", "serve the verified web client of this nylzo on this computer for an instance", runWeb},
		{"status", "show the state of a message from its manage token", runStatus},
		{"cancel", "end a message with its manage token", runCancel},
		{"report", "report a message for abuse from its link on stdin", runReport},
		{"burn", "delete the remaining opens of a message from its link", runBurn},
		{"serve", "run the server with the embedded web client", operator.RunServe},
		{"sweep-once", "run one sweep of the server store and exit", operator.RunSweepOnce},
		{"takedown", "remove reported messages by id from stdin, on the server host", operator.RunTakedown},
		{"report-key", "create the key pair that seals abuse reports for the operator", operator.RunReportKey},
		{"reports", "open sealed abuse reports from stdin with the operator's private key", operator.RunReports},
		{"verify-client", "check that a server sends the same web client as this nylzo", runVerifyClient},
		{"version", "print the version", runVersion},
	}
}

// Run refuses a link with its key anywhere in the arguments, where other processes could read it (AK14).
func Run(arguments []string, environment *command.Environment) int {
	if slices.ContainsFunc(arguments, fragmentLike.MatchString) {
		fmt.Fprintln(
			environment.Stderr,
			command.Program+": a link with its key must not be on the command line; pass it on stdin, with --link-file or in NYLZO_LINK",
		)
		return command.ExitUsage
	}
	if len(arguments) > 0 && arguments[0] == "--version" {
		arguments = append([]string{"version"}, arguments[1:]...)
	}
	if len(arguments) == 0 {
		fmt.Fprint(environment.Stderr, usage())
		return command.ExitUsage
	}
	if slices.Contains([]string{"help", "-h", "--help"}, arguments[0]) {
		fmt.Fprint(environment.Stdout, usage())
		return command.ExitOK
	}
	index := slices.IndexFunc(commands(), func(candidate subcommand) bool { return candidate.name == arguments[0] })
	if index < 0 {
		name := "unknown command"
		if commandLike.MatchString(arguments[0]) {
			name += " " + arguments[0]
		}
		fmt.Fprintf(environment.Stderr, "%s: %s\n\n%s", command.Program, name, usage())
		return command.ExitUsage
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		stop()
	}()
	return commands()[index].run(ctx, environment, arguments[1:])
}

func usage() string {
	var out strings.Builder
	fmt.Fprintf(&out, "usage: %s <command> [flags]\n\n", command.Program)
	width := len("help")
	for _, listed := range commands() {
		width = max(width, len(listed.name))
	}
	for _, listed := range commands() {
		fmt.Fprintf(&out, "  %-*s %s\n", width, listed.name, listed.summary)
	}
	fmt.Fprintf(&out, `  %-*s print this help

Links, passphrases and manage tokens never go on the command line: they come
from stdin, from files, from NYLZO_* variables or from the terminal without
echo. Run "%s <command> -h" for the flags of a command. The server reads its
configuration from NYLZO_* variables, described in README.md.
`, width, "help", command.Program)
	return out.String()
}

func runVersion(_ context.Context, environment *command.Environment, arguments []string) int {
	flags := command.NewFlags(environment, "version")
	asJSON := flags.Bool("json", false, "print version and source build metadata as JSON")
	if code, done := command.FlagsDone(command.ParseFlags(flags, environment, arguments)); done {
		return code
	}
	information := environment.BuildInformation()
	if *asJSON {
		if err := json.NewEncoder(environment.Stdout).Encode(information); err != nil {
			return command.Failf(environment, "could not write build metadata")
		}
	} else {
		fmt.Fprintln(environment.Stdout, information.Version)
	}
	return command.ExitOK
}

func buildVersion(version string) string {
	return buildinfo.Read(version, "", "", "").Version
}

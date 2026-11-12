package cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/cli/harden"
	"github.com/misaon/nylzo/internal/cli/messages"
	"github.com/misaon/nylzo/internal/cli/operator"
	"github.com/misaon/nylzo/internal/cli/stopsignal"
	"github.com/misaon/nylzo/internal/cli/verifyclient"
	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/debugenv"
)

type subcommand struct {
	name    string
	summary string
	run     func(context.Context, *command.Environment, []string) int
	protect harden.Level
	stop    stopRule
}

// stopRule tells how the process ends after a stop signal stopped the command: by that signal, or, for the server,
// with its exit code, so that its supervisor sees a failed shutdown.
type stopRule int

const (
	endsBySignal stopRule = iota
	endsWithCode
)

// Exit is how the process ends after a command: by Signal when a stop signal ended the command, otherwise with Code.
type Exit struct {
	Code   int
	Signal os.Signal
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

func commands() []subcommand {
	return []subcommand{
		{
			"create",
			"encrypt text from stdin or a file and print its link",
			messages.RunCreate,
			harden.Full,
			endsBySignal,
		},
		{
			"read",
			"read a link from stdin and decrypt the message into private files",
			messages.RunRead,
			harden.Full,
			endsBySignal,
		},
		{
			"open",
			"open a link from stdin in the browser with the verified web client of this nylzo",
			runOpen,
			harden.Full,
			endsBySignal,
		},
		{
			"web",
			"serve the verified web client of this nylzo on this computer for an instance",
			runWeb,
			harden.Full,
			endsBySignal,
		},
		{"status", "show the state of a message from its manage token", messages.RunStatus, harden.Full, endsBySignal},
		{"cancel", "end a message with its manage token", messages.RunCancel, harden.Full, endsBySignal},
		{"report", "report a message for abuse from its link on stdin", messages.RunReport, harden.Full, endsBySignal},
		{"burn", "delete the remaining opens of a message from its link", messages.RunBurn, harden.Full, endsBySignal},
		{
			"serve",
			"run the server with the embedded web client",
			operator.RunServe,
			harden.ProcessOnly,
			endsWithCode,
		},
		{"sweep-once", "run one sweep of the server store and exit", operator.RunSweepOnce, harden.Full, endsBySignal},
		{
			"store-check",
			"tell, without writing, whether this build can open the data directory",
			operator.RunStoreCheck,
			harden.Full,
			endsBySignal,
		},
		{
			"takedown",
			"remove reported messages by id from stdin, on the server host",
			operator.RunTakedown,
			harden.Full,
			endsBySignal,
		},
		{
			"report-key",
			"create the key pair that seals abuse reports for the operator",
			operator.RunReportKey,
			harden.Full,
			endsBySignal,
		},
		{
			"reports",
			"open sealed abuse reports from stdin with the operator's private key",
			operator.RunReports,
			harden.Full,
			endsBySignal,
		},
		{
			"verify-client",
			"check that a server sends the same web client as this nylzo",
			verifyclient.Run,
			harden.Off,
			endsBySignal,
		},
		{"version", "print the version", runVersion, harden.Off, endsBySignal},
	}
}

// Main runs a command and then ends the process as the command stopped.
func Main(arguments []string, environment *command.Environment) {
	exit := Run(arguments, environment)
	if exit.Signal != nil {
		stopsignal.Exit(exit.Signal)
	}
	os.Exit(exit.Code)
}

// Run refuses a link with its key anywhere in the arguments, where other processes could read it (AK14).
func Run(arguments []string, environment *command.Environment) Exit {
	if slices.ContainsFunc(arguments, fragmentLike.MatchString) {
		fmt.Fprintln(
			environment.Stderr,
			command.Program+": a link with its key must not be on the command line; pass it on stdin, with --link-file or in NYLZO_LINK",
		)
		return Exit{Code: command.ExitUsage}
	}
	if len(arguments) > 0 && arguments[0] == "--version" {
		arguments = append([]string{"version"}, arguments[1:]...)
	}
	if len(arguments) == 0 {
		fmt.Fprint(environment.Stderr, usage())
		return Exit{Code: command.ExitUsage}
	}
	if slices.Contains([]string{"help", "-h", "--help"}, arguments[0]) {
		fmt.Fprint(environment.Stdout, usage())
		return Exit{Code: command.ExitOK}
	}
	index := slices.IndexFunc(commands(), func(candidate subcommand) bool { return candidate.name == arguments[0] })
	if index < 0 {
		name := "unknown command"
		if commandLike.MatchString(arguments[0]) {
			name += " " + arguments[0]
		}
		fmt.Fprintf(environment.Stderr, "%s: %s\n\n%s", command.Program, name, usage())
		return Exit{Code: command.ExitUsage}
	}
	listed := commands()[index]
	if code, ok := protect(environment, listed.protect); !ok {
		return Exit{Code: code}
	}
	running := *environment
	running.Stop = new(stopsignal.Pending)
	ctx, cancel := context.WithCancelCause(context.Background())
	release := stopOnSignal(running.Stop, cancel)
	exit := Exit{Code: listed.run(ctx, &running, arguments[1:])}
	release()
	exit.Signal = listed.endingSignal(exit.Code, running.Stop)
	return exit
}

// endingSignal is the stop signal the process ends by after the command returned code: one the command held back,
// or one that stopped it, so that it failed. A command that finished its work ends with its code, and so does serve.
func (s *subcommand) endingSignal(code int, pending *stopsignal.Pending) os.Signal {
	if s.stop == endsWithCode || code == command.ExitOK && !pending.Held() {
		return nil
	}
	return pending.Signal()
}

// stopOnSignal cancels the command on the first Ctrl-C or termination request and keeps that signal in pending; a
// second one takes its default action and ends the process at once. The returned function stops listening, cancels
// the command and keeps a signal that arrived meanwhile.
func stopOnSignal(pending *stopsignal.Pending, cancel context.CancelCauseFunc) func() {
	received := make(chan os.Signal, 1)
	stopsignal.Notify(received, os.Interrupt, syscall.SIGTERM)
	listened := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(listened)
		select {
		case stop := <-received:
			signal.Stop(received)
			pending.Keep(stop)
			cancel(stopsignal.StopError{Signal: stop})
		case <-stopped:
		}
	}()
	return func() {
		signal.Stop(received)
		cancel(nil)
		close(stopped)
		<-listened
		select {
		case stop := <-received:
			pending.Keep(stop)
		default:
		}
	}
}

// protect hardens the process before the command reads anything secret, and fails closed without the hook (F07) or
// with a setting that would log request headers (F11).
func protect(environment *command.Environment, level harden.Level) (int, bool) {
	if level == harden.Off {
		return command.ExitOK, true
	}
	if setting := debugenv.HTTP2HeaderDump(environment.Getenv); setting != "" {
		return command.Failf(
			environment,
			"%s makes Go write request headers, which carry keys and passphrase verifiers, to the log; unset it",
			setting,
		), false
	}
	if environment.Harden == nil {
		return command.Failf(environment, "this build cannot protect its process, so it reads no secret"), false
	}
	warning, err := environment.Harden(level)
	if err != nil {
		return command.Failf(environment, "could not protect the process before reading secrets: %s", err), false
	}
	if allowed, _ := strconv.ParseBool(environment.Getenv(command.EnvironmentAllowSwap)); warning != "" && !allowed {
		command.Warnf(environment, "%s; %s=1 hides this warning", warning, command.EnvironmentAllowSwap)
	}
	return command.ExitOK, true
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

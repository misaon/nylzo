package cli

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"regexp"
	"runtime/debug"
	"slices"
	"strings"
	"syscall"
	"unicode"
	"unicode/utf8"

	"github.com/misaon/nylzo/internal/cli/console"
	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server"
	"github.com/misaon/nylzo/internal/server/webclient"
)

const (
	program   = "nylzo"
	exitOK    = 0
	exitError = 1
	exitUsage = 2

	exitInterrupted = 130
)

type Env struct {
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
	Getenv    func(string) string
	Terminal  console.Terminal
	Version   string
	TempDir   string
	WebClient func() (webclient.Build, error)
}

func (e *Env) webClient() (webclient.Build, error) {
	if e.WebClient != nil {
		return e.WebClient()
	}
	return server.EmbeddedWebClient()
}

func (e *Env) tempDir() string {
	if e.TempDir != "" {
		return e.TempDir
	}
	return os.TempDir()
}

type command struct {
	name    string
	summary string
	run     func(context.Context, *Env, []string) int
}

var (
	errUsage     = errors.New("usage")
	errHelp      = errors.New("help")
	keyLength    = base64.RawURLEncoding.EncodedLen(core.SecretLen)
	keyLike      = regexp.MustCompile(fmt.Sprintf(`[A-Za-z0-9_-]{%d,}`, keyLength))
	fragmentLike = regexp.MustCompile(fmt.Sprintf(`#[0-9]+\.[0-9]+\.[A-Za-z0-9_-]{%d}`, keyLength))
	badFlagValue = regexp.MustCompile(`invalid (?:boolean )?value .* for flag -([a-z-]+)`)
	commandLike  = regexp.MustCompile(`^[a-z][a-z-]{0,19}$`)
)

func commands() []command {
	return []command{
		{"create", "encrypt text from stdin or a file and print its link", runCreate},
		{"read", "read a link from stdin and decrypt the message into private files", runRead},
		{"status", "show the state of a message from its manage token", runStatus},
		{"cancel", "end a message with its manage token", runCancel},
		{"report", "report a message for abuse from its link on stdin", runReport},
		{"burn", "delete the remaining opens of a message from its link", runBurn},
		{"serve", "run the server with the embedded web client", runServe},
		{"sweep-once", "run one sweep of the server store and exit", runSweepOnce},
		{"takedown", "end reported messages by id from stdin, on the server host", runTakedown},
		{"verify-client", "check that a server sends the same web client as this nylzo", runVerifyClient},
		{"version", "print the version", runVersion},
	}
}

// Run refuses a link with its key anywhere in the arguments, where other processes could read it (AK14).
func Run(args []string, env *Env) int {
	if slices.ContainsFunc(args, fragmentLike.MatchString) {
		fmt.Fprintln(
			env.Stderr,
			program+": a link with its key must not be on the command line; pass it on stdin, with --link-file or in NYLZO_LINK",
		)
		return exitUsage
	}
	if len(args) == 0 {
		fmt.Fprint(env.Stderr, usage())
		return exitUsage
	}
	if slices.Contains([]string{"help", "-h", "--help"}, args[0]) {
		fmt.Fprint(env.Stdout, usage())
		return exitOK
	}
	index := slices.IndexFunc(commands(), func(candidate command) bool { return candidate.name == args[0] })
	if index < 0 {
		name := "unknown command"
		if commandLike.MatchString(args[0]) {
			name += " " + args[0]
		}
		fmt.Fprintf(env.Stderr, "%s: %s\n\n%s", program, name, usage())
		return exitUsage
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		stop()
	}()
	return commands()[index].run(ctx, env, args[1:])
}

func usage() string {
	var out strings.Builder
	fmt.Fprintf(&out, "usage: %s <command> [flags]\n\n", program)
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
`, width, "help", program)
	return out.String()
}

func newFlags(env *Env, name string) *flag.FlagSet {
	flags := flag.NewFlagSet(program+" "+name, flag.ContinueOnError)
	flags.SetOutput(env.Stderr)
	return flags
}

// parseFlags reports bad flags without echoing their values, which could hold a secret.
func parseFlags(flags *flag.FlagSet, env *Env, args []string) error {
	flags.SetOutput(io.Discard)
	err := flags.Parse(args)
	flags.SetOutput(env.Stderr)
	switch {
	case errors.Is(err, flag.ErrHelp):
		flags.SetOutput(env.Stdout)
		flags.Usage()
		return errHelp
	case err != nil:
		if match := badFlagValue.FindStringSubmatch(err.Error()); match != nil {
			fmt.Fprintf(env.Stderr, "%s: invalid value for -%s; run %s -h\n", flags.Name(), match[1], flags.Name())
		} else {
			fmt.Fprintf(env.Stderr, "%s: unknown or incomplete flag; run %s -h\n", flags.Name(), flags.Name())
		}
		return errUsage
	case flags.NArg() > 0:
		reason := "takes no arguments"
		if keyLike.MatchString(strings.Join(flags.Args(), " ")) {
			reason = "takes no arguments, and secrets never go on the command line"
		}
		fmt.Fprintf(
			env.Stderr,
			"%s %s; pass links, passphrases and tokens on stdin, in files or from the terminal\n",
			flags.Name(),
			reason,
		)
		return errUsage
	default:
		return nil
	}
}

func flagsDone(err error) (int, bool) {
	switch {
	case errors.Is(err, errHelp):
		return exitOK, true
	case err != nil:
		return exitUsage, true
	default:
		return exitOK, false
	}
}

func failf(env *Env, format string, arguments ...any) int {
	fmt.Fprintln(env.Stderr, program+": "+printable(fmt.Sprintf(format, arguments...)))
	return exitError
}

func printable(text string) string {
	var out strings.Builder
	for _, r := range text {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == utf8.RuneError {
			fmt.Fprintf(&out, "\\u%04x", r)
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

func runVersion(_ context.Context, env *Env, args []string) int {
	flags := newFlags(env, "version")
	if code, done := flagsDone(parseFlags(flags, env, args)); done {
		return code
	}
	fmt.Fprintln(env.Stdout, buildVersion(env.Version))
	return exitOK
}

func buildVersion(version string) string {
	if version != "" && version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

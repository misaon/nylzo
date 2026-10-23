package cli

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"os/signal"
	"regexp"
	"slices"
	"strings"
	"syscall"

	"github.com/misaon/nylzo/internal/core"
)

const (
	program   = "nylzo"
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

type Env struct {
	Stdin    io.Reader
	Stdout   io.Writer
	Stderr   io.Writer
	Getenv   func(string) string
	Terminal Terminal
	Version  string
}

type command struct {
	name    string
	summary string
	run     func(context.Context, Env, []string) int
}

var (
	errUsage = errors.New("usage")
	keyLike  = regexp.MustCompile(fmt.Sprintf(`[A-Za-z0-9_-]{%d,}`, base64.RawURLEncoding.EncodedLen(core.SecretLen)))
)

func commands() []command {
	return []command{
		{"create", "encrypt text from stdin or a file and print its link", runCreate},
		{"read", "read a link from stdin and decrypt the message into private files", runRead},
		{"status", "show the state of a message from its manage token", runStatus},
		{"cancel", "end a message with its manage token", runCancel},
		{"burn", "delete the remaining opens of a message from its link", runBurn},
		{"serve", "run the server with the embedded web client", runServe},
		{"sweep-once", "run one sweep of the server store and exit", runSweepOnce},
		{"version", "print the version", runVersion},
	}
}

func Run(args []string, env Env) int {
	if secretInArguments(args) {
		fmt.Fprintln(env.Stderr, program+": a link with its key must not be on the command line; pass it on stdin, with --link-file or in NYLZO_LINK")
		return exitUsage
	}
	if len(args) == 0 || slices.Contains([]string{"help", "-h", "--help"}, args[0]) {
		out := env.Stdout
		if len(args) == 0 {
			out = env.Stderr
		}
		fmt.Fprint(out, usage())
		if len(args) == 0 {
			return exitUsage
		}
		return exitOK
	}
	i := slices.IndexFunc(commands(), func(c command) bool { return c.name == args[0] })
	if i < 0 {
		fmt.Fprintf(env.Stderr, "%s: unknown command %q\n\n%s", program, args[0], usage())
		return exitUsage
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return commands()[i].run(ctx, env, args[1:])
}

func usage() string {
	var b strings.Builder
	fmt.Fprintf(&b, "usage: %s <command> [flags]\n\n", program)
	for _, c := range commands() {
		fmt.Fprintf(&b, "  %-11s %s\n", c.name, c.summary)
	}
	fmt.Fprintf(&b, `  %-11s print this help

Links, passphrases and manage tokens never go on the command line: they come
from stdin, from files, from NYLZO_* variables or from the terminal without
echo. Run "%s <command> -h" for the flags of a command. The server reads its
configuration from NYLZO_* variables, described in README.md.
`, "help", program)
	return b.String()
}

func secretInArguments(args []string) bool {
	for _, arg := range args {
		if _, fragment, ok := strings.Cut(arg, "#"); ok {
			if _, _, err := core.ParseFragment(fragment); err == nil {
				return true
			}
		}
	}
	return false
}

func newFlags(env Env, name string) *flag.FlagSet {
	fs := flag.NewFlagSet(program+" "+name, flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	return fs
}

func parseFlags(fs *flag.FlagSet, env Env, args []string) error {
	if err := fs.Parse(args); err != nil {
		return errUsage
	}
	if fs.NArg() > 0 {
		reason := "takes no arguments"
		if keyLike.MatchString(strings.Join(fs.Args(), " ")) {
			reason = "takes no arguments, and secrets never go on the command line"
		}
		fmt.Fprintf(env.Stderr, "%s %s; pass links, passphrases and tokens on stdin, in files or from the terminal\n", fs.Name(), reason)
		return errUsage
	}
	return nil
}

func failf(env Env, format string, a ...any) int {
	fmt.Fprintf(env.Stderr, program+": "+format+"\n", a...)
	return exitError
}

func runVersion(_ context.Context, env Env, args []string) int {
	fs := newFlags(env, "version")
	if parseFlags(fs, env, args) != nil {
		return exitUsage
	}
	fmt.Fprintln(env.Stdout, buildVersion(env.Version))
	return exitOK
}

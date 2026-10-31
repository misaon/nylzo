package command

import (
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/misaon/nylzo/internal/buildinfo"
	"github.com/misaon/nylzo/internal/cli/console"
	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server/webclient"
)

const (
	Program   = "nylzo"
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2

	ExitInterrupted = 130

	PrivateFileMode = 0o600
)

type Environment struct {
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
	Getenv    func(string) string
	Terminal  console.Terminal
	Version   string
	Commit    string
	Date      string
	Dirty     string
	WebClient func() (webclient.Build, error)
}

func (e *Environment) BuildInformation() buildinfo.Info {
	return buildinfo.Read(e.Version, e.Commit, e.Date, e.Dirty)
}

var (
	errUsage   = errors.New("usage")
	errHelp    = errors.New("help")
	errTooLong = errors.New("the input is longer than nylzo accepts")
	keyLike    = regexp.MustCompile(
		fmt.Sprintf(`[A-Za-z0-9_-]{%d,}`, base64.RawURLEncoding.EncodedLen(core.SecretLen)),
	)
	badFlagValue = regexp.MustCompile(`invalid (?:boolean )?value .* for flag -([a-z-]+)`)
)

func NewFlags(environment *Environment, name string) *flag.FlagSet {
	flags := flag.NewFlagSet(Program+" "+name, flag.ContinueOnError)
	flags.SetOutput(environment.Stderr)
	return flags
}

// ParseFlags reports bad flags without echoing their values, which could hold a secret.
func ParseFlags(flags *flag.FlagSet, environment *Environment, arguments []string) error {
	flags.SetOutput(io.Discard)
	err := flags.Parse(arguments)
	flags.SetOutput(environment.Stderr)
	switch {
	case errors.Is(err, flag.ErrHelp):
		flags.SetOutput(environment.Stdout)
		flags.Usage()
		return errHelp
	case err != nil:
		if match := badFlagValue.FindStringSubmatch(err.Error()); match != nil {
			fmt.Fprintf(
				environment.Stderr,
				"%s: invalid value for -%s; run %s -h\n",
				flags.Name(),
				match[1],
				flags.Name(),
			)
		} else {
			fmt.Fprintf(environment.Stderr, "%s: unknown or incomplete flag; run %s -h\n", flags.Name(), flags.Name())
		}
		return errUsage
	case flags.NArg() > 0:
		reason := "takes no arguments"
		if keyLike.MatchString(strings.Join(flags.Args(), " ")) {
			reason = "takes no arguments, and secrets never go on the command line"
		}
		fmt.Fprintf(
			environment.Stderr,
			"%s %s; pass links, passphrases and tokens on stdin, in files or from the terminal\n",
			flags.Name(),
			reason,
		)
		return errUsage
	default:
		return nil
	}
}

func FlagsDone(err error) (int, bool) {
	switch {
	case errors.Is(err, errHelp):
		return ExitOK, true
	case err != nil:
		return ExitUsage, true
	default:
		return ExitOK, false
	}
}

func Failf(environment *Environment, format string, arguments ...any) int {
	fmt.Fprintln(environment.Stderr, Program+": "+printable(fmt.Sprintf(format, arguments...)))
	return ExitError
}

func printable(text string) string {
	var out strings.Builder
	for _, character := range text {
		if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) || character == utf8.RuneError {
			fmt.Fprintf(&out, "\\u%04x", character)
			continue
		}
		out.WriteRune(character)
	}
	return out.String()
}

func ReadLimited(reader io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errTooLong
	}
	return data, nil
}

func ReadFileLimited(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, Unreadable(err)
	}
	defer file.Close()
	data, err := ReadLimited(file, limit)
	if err != nil && !errors.Is(err, errTooLong) {
		return nil, Unreadable(err)
	}
	return data, err
}

// Unreadable drops the path from a file error, so no message repeats a flag value.
func Unreadable(err error) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return errors.New("the file does not exist")
	case errors.Is(err, fs.ErrPermission):
		return errors.New("the file is not readable")
	default:
		return errors.New("the file cannot be read")
	}
}

package operator

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/server/config"
	"github.com/misaon/nylzo/internal/server/logging"
	"github.com/misaon/nylzo/internal/store"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

const (
	maxTakedownInput = 1 << 20
	maxTakedownIDs   = 10000
)

func RunTakedown(ctx context.Context, environment *command.Environment, arguments []string) int {
	flags := command.NewFlags(environment, "takedown")
	flags.Usage = func() {
		fmt.Fprintf(
			flags.Output(),
			"usage: %s takedown < ids\n\nRemoves every message whose id stands on its own line of stdin; readers then see deleted. Run it on the server host; it reads NYLZO_DATA_DIR like sweep-once.\n",
			command.Program,
		)
	}
	if code, done := command.FlagsDone(command.ParseFlags(flags, environment, arguments)); done {
		return code
	}
	if !serverSupported {
		return command.Failf(environment, "%s", errServerUnsupported)
	}
	if environment.Terminal.StdinIsTerminal() {
		fmt.Fprintln(
			environment.Stderr,
			command.Program+" takedown reads the ids from a pipe or a file on stdin, one per line",
		)
		return command.ExitUsage
	}
	ids, err := readIDs(environment.Stdin)
	if err != nil {
		fmt.Fprintln(environment.Stderr, command.Program+" takedown: "+err.Error())
		return command.ExitUsage
	}
	settings, err := config.FromEnvironment(environment.Getenv, false)
	if err != nil {
		return command.Failf(environment, "%s", err)
	}
	if err = requireStore(settings.DataDirectory); err != nil {
		return command.Failf(environment, "%s", err)
	}
	database, blobs, err := openStore(
		ctx,
		settings.DataDirectory,
		settings.Development,
		blobPool(&settings),
		sqlite.OpenCurrent,
		slog.New(slog.NewTextHandler(environment.Stderr, nil)),
	)
	if err != nil {
		return command.Failf(environment, "%s", logging.ErrorText(err))
	}
	defer database.Close()
	return takeDown(ctx, environment, database, blobs, ids)
}

// takeDown removes each message without opening its record; readers then see deleted (D63, D101).
func takeDown(
	ctx context.Context,
	environment *command.Environment,
	database store.Store,
	blobs store.Blobs,
	ids []core.ID,
) int {
	failed := 0
	for _, id := range ids {
		if ctx.Err() != nil {
			fmt.Fprintln(environment.Stderr, command.Program+": takedown stopped before every id was handled")
			return command.ExitInterrupted
		}
		removal, err := store.TakeDown(ctx, database, blobs, seal.LocatorOf(id[:]), time.Now())
		switch {
		case errors.Is(err, store.ErrNotFound):
			fmt.Fprintf(environment.Stdout, "%s no message\n", id)
		case err != nil:
			failed++
			fmt.Fprintf(environment.Stdout, "%s failed: %s\n", id, logging.ErrorText(err))
		case removal == store.AlreadyEnded:
			fmt.Fprintf(environment.Stdout, "%s already ended\n", id)
		default:
			fmt.Fprintf(environment.Stdout, "%s deleted\n", id)
		}
	}
	if failed > 0 {
		return command.Failf(
			environment,
			"%d of %d messages could not be removed; run takedown again for them",
			failed,
			len(ids),
		)
	}
	return command.ExitOK
}

var (
	errTakedownInputTooLarge = errors.New("give at most 1 MiB of ids on stdin")
	errLineNotID             = errors.New("is not a message id; give one id per line, never a link")
	errLineTooLong           = errors.New("a line on stdin is too long to be a message id")
	errNoIDs                 = errors.New("give the ids to end on stdin, one per line")
	errTooManyIDs            = fmt.Errorf("give at most %d ids at once", maxTakedownIDs)
)

// readIDs refuses the whole input when one line is not an id, so a pasted link never ends anything.
func readIDs(reader io.Reader) ([]core.ID, error) {
	data, err := command.ReadLimited(reader, maxTakedownInput)
	if err != nil {
		return nil, errTakedownInputTooLarge
	}
	seen := map[string]bool{}
	var ids []core.ID
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		id, parseErr := core.ParseID(text)
		if parseErr != nil {
			return nil, fmt.Errorf("line %d %w", line, errLineNotID)
		}
		if !seen[id.String()] {
			seen[id.String()] = true
			ids = append(ids, id)
		}
	}
	if scanner.Err() != nil {
		return nil, errLineTooLong
	}
	if len(ids) == 0 {
		return nil, errNoIDs
	}
	if len(ids) > maxTakedownIDs {
		return nil, errTooManyIDs
	}
	return ids, nil
}

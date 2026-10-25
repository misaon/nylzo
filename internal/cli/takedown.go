package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/misaon/nylzo/internal/server/config"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server/logging"
	"github.com/misaon/nylzo/internal/store"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

const (
	maxTakedownInput = 1 << 20
	maxTakedownIDs   = 10000
)

func runTakedown(ctx context.Context, env Env, args []string) int {
	fs := newFlags(env, "takedown")
	fs.Usage = func() {
		fmt.Fprintf(
			fs.Output(),
			"usage: %s takedown < ids\n\nEnds every message whose id stands on its own line of stdin, as deleted. Run it on the server host; it reads NYLZO_DATA_DIR like sweep-once.\n",
			program,
		)
	}
	if code, done := flagsDone(parseFlags(fs, env, args)); done {
		return code
	}
	if !serverSupported {
		return failf(env, "%s", errServerUnsupported)
	}
	if env.Terminal.StdinIsTerminal() {
		fmt.Fprintln(env.Stderr, program+" takedown reads the ids from a pipe or a file on stdin, one per line")
		return exitUsage
	}
	ids, err := readIDs(env.Stdin)
	if err != nil {
		fmt.Fprintln(env.Stderr, program+" takedown: "+err.Error())
		return exitUsage
	}
	cfg, err := config.FromEnv(env.Getenv, false)
	if err != nil {
		return failf(env, "%s", err)
	}
	if err := requireStore(cfg.DataDir); err != nil {
		return failf(env, "%s", err)
	}
	st, blobs, err := open(cfg, sqlite.OpenCurrent)
	if err != nil {
		return failf(env, "%s", logging.ErrorText(err))
	}
	defer st.Close()
	failed := 0
	for _, id := range ids {
		if ctx.Err() != nil {
			fmt.Fprintln(env.Stderr, program+": takedown stopped before every id was handled")
			return exitInterrupted
		}
		final, err := store.EndMessage(ctx, st, blobs, id, store.StateDeleted, time.Now())
		switch {
		case errors.Is(err, store.ErrNotFound):
			fmt.Fprintf(env.Stdout, "%s no live message\n", id)
		case err != nil:
			failed++
			fmt.Fprintf(env.Stdout, "%s failed: %s\n", id, logging.ErrorText(err))
		case final != store.StateDeleted:
			fmt.Fprintf(env.Stdout, "%s already ended as %s\n", id, final)
		default:
			fmt.Fprintf(env.Stdout, "%s deleted\n", id)
		}
	}
	if failed > 0 {
		return failf(env, "%d of %d messages could not be ended; run takedown again for them", failed, len(ids))
	}
	return exitOK
}

func readIDs(r io.Reader) ([]string, error) {
	data, err := readLimited(r, maxTakedownInput)
	if err != nil {
		return nil, errors.New("give at most 1 MiB of ids on stdin")
	}
	seen := map[string]bool{}
	var ids []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		id, err := core.ParseID(text)
		if err != nil {
			return nil, fmt.Errorf("line %d is not a message id; give one id per line, never a link", line)
		}
		if !seen[id.String()] {
			seen[id.String()] = true
			ids = append(ids, id.String())
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, errors.New("a line on stdin is too long to be a message id")
	}
	if len(ids) == 0 {
		return nil, errors.New("give the ids to end on stdin, one per line")
	}
	if len(ids) > maxTakedownIDs {
		return nil, fmt.Errorf("give at most %d ids at once", maxTakedownIDs)
	}
	return ids, nil
}

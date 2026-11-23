package operator

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/store"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

type stdinTerminal struct{ noTerminal }

func (stdinTerminal) StdinIsTerminal() bool { return true }

const storedBlobBytes = 4096

func storeMessage(t *testing.T, directory string) core.ID {
	t.Helper()
	database, blobs, err := openStore(
		t.Context(),
		directory,
		testDevelopment,
		testPool,
		sqlite.Open,
		slog.New(slog.DiscardHandler),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	id, blobID := core.NewID(), core.NewID()
	now := time.Now().UTC().Truncate(time.Minute)
	message := &store.Message{
		Version:        1,
		CreatedAt:      now,
		ExpiresAt:      now.Add(time.Hour),
		TTLSeconds:     3600,
		OpensMax:       1,
		OpensRemaining: 1,
		Size:           storedBlobBytes,
	}
	record := store.NewRecord(
		seal.NewKey(),
		seal.LocatorOf(id[:]),
		message,
		core.NewSecret()[:],
		core.NewSecret()[:],
		core.Encode(blobID[:]),
		new(seal.NewBlobKey()),
	)
	blob := bytes.NewReader(make([]byte, storedBlobBytes))
	if _, err = blobs.PutExpiring(
		t.Context(),
		record.Row.BlobReference,
		blob,
		storedBlobBytes,
		time.Now().Add(time.Hour),
	); err != nil {
		t.Fatal(err)
	}
	if err = database.Create(t.Context(), &record.Row); err != nil {
		t.Fatal(err)
	}
	return id
}

func hasContent(t *testing.T, directory string, id core.ID) bool {
	t.Helper()
	database, _, err := openStore(
		t.Context(),
		directory,
		testDevelopment,
		testPool,
		sqlite.Open,
		slog.New(slog.DiscardHandler),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	row, err := database.Load(t.Context(), seal.LocatorOf(id[:]))
	if err != nil {
		t.Fatal(err)
	}
	return row.BlobWrap != nil
}

func TestTakedownEndsReportedMessages(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	reported, kept := storeMessage(t, directory), storeMessage(t, directory)
	unknown := core.NewID()
	variables := map[string]string{"NYLZO_DATA_DIR": directory}

	input := reported.String() + "\n\n" + reported.String() + "\n" + unknown.String() + "\n"
	out := run(t, input, variables, nil, RunTakedown)
	if out.code != command.ExitOK || out.stdout != reported.String()+" deleted\n"+unknown.String()+" no message\n" {
		t.Fatalf("%+v", out)
	}
	if hasContent(t, directory, reported) || !hasContent(t, directory, kept) {
		t.Fatal("takedown must remove the reported message and nothing else")
	}
	again := run(t, reported.String()+"\n", variables, nil, RunTakedown)
	if again.code != command.ExitOK || again.stdout != reported.String()+" already ended\n" {
		t.Fatalf("%+v", again)
	}
}

func TestTakedownChecksEveryLineFirst(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	id := storeMessage(t, directory)
	link := core.FormatLink("https://nylzo.example", id, core.KDFNone, core.NewSecret(), core.NewSecret())
	variables := map[string]string{"NYLZO_DATA_DIR": directory}
	for _, input := range []string{
		id.String() + "\n" + link + "\n",
		id.String() + "\nnot-an-id\n",
		"\n\n",
		strings.Repeat("A", 70000) + "\n",
		manyIDs(10001),
	} {
		result := run(t, input, variables, nil, RunTakedown)
		if result.code != command.ExitUsage || result.stdout != "" {
			t.Fatalf("want a usage error before any change, got %+v", result)
		}
		if strings.Contains(result.stderr, "#") || strings.Contains(result.stderr, "not-an-id") {
			t.Fatalf("the error must not repeat the input: %q", result.stderr)
		}
	}
	if !hasContent(t, directory, id) {
		t.Fatal("a refused takedown must change nothing")
	}
}

func TestTakedownNeedsTheStoreAndAPipe(t *testing.T) {
	t.Parallel()
	id := core.NewID().String()
	result := run(t, id+"\n", map[string]string{"NYLZO_DATA_DIR": t.TempDir()}, nil, RunTakedown)
	if result.code != command.ExitError || !strings.Contains(result.stderr, "found no store") {
		t.Fatalf("%+v", result)
	}
	result = run(t, "", nil, stdinTerminal{}, RunTakedown)
	if result.code != command.ExitUsage || !strings.Contains(result.stderr, "from a pipe or a file") {
		t.Fatalf("%+v", result)
	}
}

func manyIDs(count int) string {
	var builder strings.Builder
	for range count {
		builder.WriteString(core.NewID().String() + "\n")
	}
	return builder.String()
}

func TestTakedownStopsBeforeItOpensAStoreItCannotUse(t *testing.T) {
	t.Parallel()
	id := core.NewID().String() + "\n"
	linkedAdmin := t.TempDir()
	createStore(t, linkedAdmin)
	if err := os.Symlink(filepath.Join(t.TempDir(), "admin"), adminStorePath(linkedAdmin)); err != nil {
		t.Fatal(err)
	}
	for name, testCase := range map[string]struct {
		input     string
		variables map[string]string
		code      int
	}{
		"an invalid setting":      {id, map[string]string{"NYLZO_RATE_SOURCE_BURST": "0.5"}, command.ExitError},
		"a linked admin database": {id, map[string]string{"NYLZO_DATA_DIR": linkedAdmin}, command.ExitError},
		"too many bytes":          {strings.Repeat(id, maxTakedownInput/len(id)+1), nil, command.ExitUsage},
	} {
		if result := run(t, testCase.input, testCase.variables, nil, RunTakedown); result.code != testCase.code ||
			result.stdout != "" {
			t.Errorf("takedown with %s: %+v", name, result)
		}
	}
}

func TestTakedownReportsEveryIDItCouldNotHandle(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	ids := []core.ID{storeMessage(t, directory), storeMessage(t, directory)}
	database, blobs, err := openStore(
		t.Context(),
		directory,
		testDevelopment,
		testPool,
		sqlite.Open,
		slog.New(slog.DiscardHandler),
	)
	if err != nil {
		t.Fatal(err)
	}
	database.Close()
	var stdout, stderr bytes.Buffer
	environment := &command.Environment{Stdout: &stdout, Stderr: &stderr}
	if code := takeDown(t.Context(), environment, database, blobs, ids); code != command.ExitError ||
		strings.Count(stdout.String(), " failed: ") != len(ids) || !strings.Contains(stderr.String(), "2 of 2") {
		t.Fatalf("takedown on a closed store: exit %d, %s%s", code, stdout.String(), stderr.String())
	}
	stopped, cancel := context.WithCancel(t.Context())
	cancel()
	stdout.Reset()
	if code := takeDown(stopped, environment, database, blobs, ids); code != command.ExitInterrupted ||
		stdout.String() != "" {
		t.Fatalf("a stopped takedown: exit %d, %s", code, stdout.String())
	}
}

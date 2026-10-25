package cli

import (
	"strings"
	"testing"

	"github.com/misaon/nylzo/internal/core"
)

type stdinTerminal struct{ noTerminal }

func (stdinTerminal) StdinIsTerminal() bool { return true }

func linkID(t *testing.T, link string) string {
	t.Helper()
	parsed, err := core.ParseLink(link)
	if err != nil {
		t.Fatal(err)
	}
	return parsed.ID.String()
}

func TestTakedownEndsReportedMessages(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	reported, result := createText(t, instance, "podvod")
	if result.code != exitOK {
		t.Fatalf("%+v", result)
	}
	kept, _ := createText(t, instance, "v pořádku")
	unknown := core.NewID().String()
	id := linkID(t, reported)
	variables := map[string]string{"NYLZO_DATA_DIR": instance.directory}

	out := runWith(t, id+"\n\n"+id+"\n"+unknown+"\n", variables, nil, "takedown")
	if out.code != exitOK || out.stdout != id+" deleted\n"+unknown+" no message\n" {
		t.Fatalf("%+v", out)
	}
	if read := runWith(
		t,
		reported,
		nil,
		nil,
		"read",
		"--stdout",
	); read.code != exitError ||
		!strings.Contains(read.stderr, "the message was deleted") {
		t.Fatalf("a taken down message must read as deleted: %+v", read)
	}
	if read := runWith(t, kept, nil, nil, "read", "--stdout"); read.code != exitOK || read.stdout != "v pořádku\n" {
		t.Fatalf("takedown touched another message: %+v", read)
	}
	if again := runWith(
		t,
		id+"\n",
		variables,
		nil,
		"takedown",
	); again.code != exitOK ||
		again.stdout != id+" already ended\n" {
		t.Fatalf("%+v", again)
	}
}

func TestTakedownChecksEveryLineFirst(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	link, _ := createText(t, instance, "zůstane")
	id := linkID(t, link)
	variables := map[string]string{"NYLZO_DATA_DIR": instance.directory}
	for _, input := range []string{
		id + "\n" + link + "\n",
		id + "\nnot-an-id\n",
		"\n\n",
		strings.Repeat("A", 70000) + "\n",
		manyIDs(10001),
	} {
		result := runWith(t, input, variables, nil, "takedown")
		if result.code != exitUsage || result.stdout != "" {
			t.Fatalf("want a usage error before any change, got %+v", result)
		}
		if strings.Contains(result.stderr, "#") || strings.Contains(result.stderr, "not-an-id") {
			t.Fatalf("the error must not repeat the input: %q", result.stderr)
		}
	}
	if read := runWith(t, link, nil, nil, "read", "--stdout"); read.code != exitOK {
		t.Fatalf("a refused takedown must change nothing: %+v", read)
	}
}

func TestTakedownNeedsTheStoreAndAPipe(t *testing.T) {
	t.Parallel()
	id := core.NewID().String()
	if result := runWith(
		t,
		id+"\n",
		map[string]string{"NYLZO_DATA_DIR": t.TempDir()},
		nil,
		"takedown",
	); result.code != exitError ||
		!strings.Contains(result.stderr, "found no store") {
		t.Fatalf("%+v", result)
	}
	if result := runWith(
		t,
		"",
		nil,
		stdinTerminal{},
		"takedown",
	); result.code != exitUsage ||
		!strings.Contains(result.stderr, "from a pipe or a file") {
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

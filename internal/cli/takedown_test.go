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
	ts := newTestServer(t, nil)
	reported, res := createText(t, ts, "podvod")
	if res.code != exitOK {
		t.Fatalf("%+v", res)
	}
	kept, _ := createText(t, ts, "v pořádku")
	unknown := core.NewID().String()
	id := linkID(t, reported)
	vars := map[string]string{"NYLZO_DATA_DIR": ts.dir}

	out := runWith(t, id+"\n\n"+id+"\n"+unknown+"\n", vars, nil, "takedown")
	if out.code != exitOK || out.stdout != id+" deleted\n"+unknown+" no live message\n" {
		t.Fatalf("%+v", out)
	}
	if read := runWith(t, reported, nil, nil, "read", "--stdout"); read.code != exitError || !strings.Contains(read.stderr, "the message was deleted") {
		t.Fatalf("a taken down message must read as deleted: %+v", read)
	}
	if read := runWith(t, kept, nil, nil, "read", "--stdout"); read.code != exitOK || read.stdout != "v pořádku\n" {
		t.Fatalf("takedown touched another message: %+v", read)
	}
	if again := runWith(t, id+"\n", vars, nil, "takedown"); again.code != exitOK || again.stdout != id+" no live message\n" {
		t.Fatalf("%+v", again)
	}
}

func TestTakedownChecksEveryLineFirst(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, nil)
	link, _ := createText(t, ts, "zůstane")
	id := linkID(t, link)
	vars := map[string]string{"NYLZO_DATA_DIR": ts.dir}
	for _, input := range []string{
		id + "\n" + link + "\n",
		id + "\nnot-an-id\n",
		"\n\n",
		strings.Repeat("A", 70000) + "\n",
		manyIDs(10001),
	} {
		res := runWith(t, input, vars, nil, "takedown")
		if res.code != exitUsage || res.stdout != "" {
			t.Fatalf("want a usage error before any change, got %+v", res)
		}
		if strings.Contains(res.stderr, "#") || strings.Contains(res.stderr, "not-an-id") {
			t.Fatalf("the error must not repeat the input: %q", res.stderr)
		}
	}
	if read := runWith(t, link, nil, nil, "read", "--stdout"); read.code != exitOK {
		t.Fatalf("a refused takedown must change nothing: %+v", read)
	}
}

func TestTakedownNeedsTheStoreAndAPipe(t *testing.T) {
	t.Parallel()
	id := core.NewID().String()
	if res := runWith(t, id+"\n", map[string]string{"NYLZO_DATA_DIR": t.TempDir()}, nil, "takedown"); res.code != exitError || !strings.Contains(res.stderr, "found no store") {
		t.Fatalf("%+v", res)
	}
	if res := runWith(t, "", nil, stdinTerminal{}, "takedown"); res.code != exitUsage || !strings.Contains(res.stderr, "from a pipe or a file") {
		t.Fatalf("%+v", res)
	}
}

func manyIDs(n int) string {
	var b strings.Builder
	for range n {
		b.WriteString(core.NewID().String() + "\n")
	}
	return b.String()
}

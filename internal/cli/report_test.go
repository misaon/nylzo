package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/misaon/nylzo/internal/core"
)

func TestReportSendsARedactedReason(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, nil)
	link, _ := createText(t, ts, "podvod")
	parsed, err := core.ParseLink(link)
	if err != nil {
		t.Fatal(err)
	}
	res := runWith(t, link, nil, nil, "report", "--reason", "phishing, see https://evil.example/#1.0.abc")
	if res.code != exitOK || !strings.Contains(res.stdout, "the report was sent") {
		t.Fatalf("%+v", res)
	}
	files, err := filepath.Glob(filepath.Join(ts.dir, "abuse", "*.jsonl"))
	if err != nil || len(files) != 1 {
		t.Fatalf("abuse files %v %v", files, err)
	}
	stored, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stored), parsed.ID.String()) || !strings.Contains(string(stored), "https://evil.example/[redacted]") || strings.Contains(string(stored), "#1.0") {
		t.Fatalf("stored report %s", stored)
	}
}

func TestReportRefusesALongReasonBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, nil)
	link, _ := createText(t, ts, "podvod")
	before := ts.requests.Load()
	res := runWith(t, link, nil, nil, "report", "--reason", strings.Repeat("a", core.MaxReasonBytes+1))
	if res.code != exitUsage || !strings.Contains(res.stderr, "longer than 1000 bytes") || ts.requests.Load() != before {
		t.Fatalf("%+v, %d requests", res, ts.requests.Load()-before)
	}
}

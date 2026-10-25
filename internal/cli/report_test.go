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
	instance := newTestServer(t, nil)
	link, _ := createText(t, instance, "podvod")
	parsed, err := core.ParseLink(link)
	if err != nil {
		t.Fatal(err)
	}
	result := runWith(
		t,
		link,
		nil,
		nil,
		"report",
		"--reason-file",
		writeSecretFile(t, "phishing, see https://evil.example/#1.0.abc\n"),
	)
	if result.code != exitOK || !strings.Contains(result.stdout, "the report was sent") {
		t.Fatalf("%+v", result)
	}
	files, err := filepath.Glob(filepath.Join(instance.directory, "abuse", "*.jsonl"))
	if err != nil || len(files) != 1 {
		t.Fatalf("abuse files %v %v", files, err)
	}
	stored, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stored), parsed.ID.String()) ||
		!strings.Contains(string(stored), "https://evil.example/[redacted]") ||
		strings.Contains(string(stored), "#1.0") {
		t.Fatalf("stored report %s", stored)
	}
}

func TestReportRefusesALongReasonBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	link, _ := createText(t, instance, "podvod")
	before := instance.requests.Load()
	for _, reason := range []string{strings.Repeat("ab ", 400), strings.Repeat("# ", 150)} {
		result := runWith(t, link, nil, nil, "report", "--reason-file", writeSecretFile(t, reason))
		if result.code != exitUsage || !strings.Contains(result.stderr, "longer than 1000 bytes") ||
			instance.requests.Load() != before {
			t.Fatalf("%+v, %d requests", result, instance.requests.Load()-before)
		}
	}
	short := strings.Repeat("# ", 50)
	if result := runWith(
		t,
		link,
		nil,
		nil,
		"report",
		"--reason-file",
		writeSecretFile(t, short),
	); result.code != exitOK {
		t.Fatalf("a reason that fits after redaction must pass: %+v", result)
	}
}

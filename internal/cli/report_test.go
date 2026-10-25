package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server/abuse"
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
		"--contact",
		" jana@example.cz",
		"--good-faith",
	)
	if result.code != exitOK || !strings.Contains(result.stdout, "the report was sent") {
		t.Fatalf("%+v", result)
	}
	files, err := filepath.Glob(filepath.Join(instance.directory, "abuse", "*"+abuse.FileExtension))
	if err != nil || len(files) != 1 {
		t.Fatalf("abuse files %v %v", files, err)
	}
	sealed, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	opened, err := abuse.Open(testReportKey, sealed)
	if err != nil || len(opened) != 1 {
		t.Fatalf("stored reports %+v %v", opened, err)
	}
	stored := opened[0]
	if stored.ID != parsed.ID.String() ||
		stored.Reason != "phishing, see https://evil.example/[redacted]" ||
		!stored.GoodFaith ||
		stored.Contact != "jana@example.cz" {
		t.Fatalf("stored report %+v", stored)
	}
}

func TestReportSaysWhenTheDailyLimitDroppedIt(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	link, _ := createText(t, instance, "podvod")
	for range 3 {
		if result := runWith(t, link, nil, nil, "report"); result.code != exitOK {
			t.Fatalf("a report within the daily budget: %+v", result)
		}
	}
	result := runWith(t, link, nil, nil, "report")
	if result.code != exitError || !strings.Contains(result.stderr, "already has three reports today") ||
		strings.Contains(result.stdout, "the report was sent") {
		t.Fatalf("a dropped report must not read as sent: %+v", result)
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
	refused := runWith(t, link, nil, nil, "report", "--contact", "jana@example")
	if refused.code != exitUsage || !strings.Contains(refused.stderr, "must be an email address") ||
		instance.requests.Load() != before {
		t.Fatalf("an implausible contact: %+v, %d requests", refused, instance.requests.Load()-before)
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

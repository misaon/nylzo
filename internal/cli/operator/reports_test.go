package operator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/server/abuse"
)

// TestReportKeyAndReportsOpenOnlyWithTheOperatorsKey covers D101: the private key lives in a new private file.
func TestReportKeyAndReportsOpenOnlyWithTheOperatorsKey(t *testing.T) {
	t.Parallel()
	keyFile := filepath.Join(t.TempDir(), "report-key")
	created := run(t, "", nil, nil, RunReportKey, "--key-file", keyFile)
	public, found := strings.CutPrefix(strings.TrimSpace(created.stdout), "NYLZO_REPORT_KEY=")
	if created.code != command.ExitOK || !found {
		t.Fatalf("report-key: %+v", created)
	}
	if info, err := os.Stat(keyFile); err != nil || info.Mode().Perm() != command.PrivateFileMode {
		t.Fatalf("the private key file must be private: %v %v", info, err)
	}
	if again := run(t, "", nil, nil, RunReportKey, "--key-file", keyFile); again.code == command.ExitOK {
		t.Fatal("report-key overwrote an existing private key")
	}
	publicKey, err := abuse.ParsePublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	report := &abuse.Report{
		ID:        "AAAAAAAAAAAAAAAAAAAAAA",
		Reason:    "fraud",
		Contact:   "notifier@example.cz",
		GoodFaith: true,
	}
	sealed, err := abuse.Seal(publicKey, time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC), report)
	if err != nil {
		t.Fatal(err)
	}
	opened := run(t, string(sealed), nil, nil, RunReports, "--key-file", keyFile)
	var stored abuse.Stored
	if opened.code != command.ExitOK || json.Unmarshal([]byte(opened.stdout), &stored) != nil ||
		stored.ID != report.ID || stored.Contact != report.Contact || stored.Date != "2026-10-22" {
		t.Fatalf("reports: %+v", opened)
	}
	otherKey := filepath.Join(t.TempDir(), "other")
	run(t, "", nil, nil, RunReportKey, "--key-file", otherKey)
	if wrong := run(t, string(sealed), nil, nil, RunReports, "--key-file", otherKey); wrong.code == command.ExitOK ||
		strings.Contains(wrong.stdout, report.Contact) {
		t.Fatalf("another private key opened a report: %+v", wrong)
	}
}

func TestReportsNameWhyTheKeyFileCannotBeUsed(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	oversized := filepath.Join(directory, "oversized")
	if err := os.WriteFile(oversized, make([]byte, maxReportKeyFile+1), 0o600); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		filepath.Join(directory, "missing"): "nylzo: --key-file: the file does not exist\n",
		oversized:                           "nylzo: --key-file: the input is longer than nylzo accepts\n",
	} {
		result := run(t, "", nil, nil, RunReports, "--key-file", path)
		if result.code != command.ExitError || result.stderr != want {
			t.Errorf("%s: exit %d, stderr %q, want %q", filepath.Base(path), result.code, result.stderr, want)
		}
	}
}

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/server/abuse"
)

// TestReportKeyAndReportsOpenOnlyWithTheOperatorsKey covers D101: the private key lives in a new private file.
func TestReportKeyAndReportsOpenOnlyWithTheOperatorsKey(t *testing.T) {
	t.Parallel()
	keyFile := filepath.Join(t.TempDir(), "report-key")
	created := runWith(t, "", nil, nil, "report-key", "--key-file", keyFile)
	public, found := strings.CutPrefix(strings.TrimSpace(created.stdout), "NYLZO_REPORT_KEY=")
	if created.code != exitOK || !found {
		t.Fatalf("report-key: %+v", created)
	}
	if info, err := os.Stat(keyFile); err != nil || info.Mode().Perm() != privateFileMode {
		t.Fatalf("the private key file must be private: %v %v", info, err)
	}
	if again := runWith(t, "", nil, nil, "report-key", "--key-file", keyFile); again.code == exitOK {
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
	opened := runWith(t, string(sealed), nil, nil, "reports", "--key-file", keyFile)
	var stored abuse.Stored
	if opened.code != exitOK || json.Unmarshal([]byte(opened.stdout), &stored) != nil ||
		stored.ID != report.ID || stored.Contact != report.Contact || stored.Date != "2026-10-22" {
		t.Fatalf("reports: %+v", opened)
	}
	otherKey := filepath.Join(t.TempDir(), "other")
	runWith(t, "", nil, nil, "report-key", "--key-file", otherKey)
	if wrong := runWith(t, string(sealed), nil, nil, "reports", "--key-file", otherKey); wrong.code == exitOK ||
		strings.Contains(wrong.stdout, report.Contact) {
		t.Fatalf("another private key opened a report: %+v", wrong)
	}
}

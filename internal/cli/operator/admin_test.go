package operator

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server/adminstore"
	"github.com/misaon/nylzo/internal/server/config"
	"github.com/misaon/nylzo/internal/server/passkey"
)

// Without NYLZO_PUBLIC_URL the enrollment link names this host as localhost, the one host the admin area accepts then,
// also when the server listens on every interface or only on a port.
func TestTheEnrollmentLinkWithoutAPublicURLPointsAtLocalhost(t *testing.T) {
	t.Parallel()
	for listen, want := range map[string]string{
		":8080":               "http://localhost:8080/admin",
		"0.0.0.0:8080":        "http://localhost:8080/admin",
		"127.0.0.1:8080":      "http://localhost:8080/admin",
		"[::]:8080":           "http://localhost:8080/admin",
		"[::1]:8080":          "http://localhost:8080/admin",
		"127.0.0.2:8080":      "http://127.0.0.2:8080/admin",
		"10.0.0.2:8080":       "http://10.0.0.2:8080/admin",
		"nylzo.example":       "http://nylzo.example/admin",
		"[fe80::1%eth0]:8080": "http://[fe80::1%25eth0]:8080/admin",
	} {
		if got := adminAddress(&config.Server{Listen: listen}); got != want {
			t.Errorf("listen %s: %s, want %s", listen, got, want)
		}
	}
}

func adminStore(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	createStore(t, directory)
	admin, err := adminstore.Open(t.Context(), adminStorePath(directory))
	if err != nil {
		t.Fatal(err)
	}
	if err = admin.Close(); err != nil {
		t.Fatal(err)
	}
	return directory
}

func TestTheAdminCommandsManageThePasskeysOfTheStore(t *testing.T) {
	t.Parallel()
	directory := adminStore(t)
	variables := map[string]string{"NYLZO_DATA_DIR": directory, "NYLZO_PUBLIC_URL": "https://nylzo.example"}
	enroll := run(t, "", variables, nil, RunAdminEnroll)
	if enroll.code != command.ExitOK || !strings.HasPrefix(enroll.stdout, "https://nylzo.example/admin#enroll=") {
		t.Fatalf("admin-enroll: %+v", enroll)
	}
	admin, err := adminstore.Open(t.Context(), adminStorePath(directory))
	if err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("sqlite", "file:"+adminStorePath(directory))
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.Exec(`INSERT INTO passkeys VALUES (x'0102030405060708', x'00', -8, 0, 1, 1, 1700000000, 0)`)
	database.Close()
	admin.Close()
	if err != nil {
		t.Fatal(err)
	}
	listed := run(t, "", variables, nil, RunAdminPasskeys)
	if listed.code != command.ExitOK || !strings.HasPrefix(listed.stdout, core.Encode([]byte{1, 2, 3, 4, 5, 6})+" ") {
		t.Fatalf("admin-passkeys: %+v", listed)
	}
	if reset := run(t, "", variables, nil, RunAdminReset); reset.code != command.ExitOK {
		t.Fatalf("admin-reset: %+v", reset)
	}
	if empty := run(t, "", variables, nil, RunAdminPasskeys); empty.code != command.ExitOK ||
		strings.Contains(empty.stdout, core.Encode([]byte{1, 2, 3, 4, 5, 6})) {
		t.Fatalf("admin-passkeys after admin-reset: %+v", empty)
	}
}

func TestAPasskeyListingTellsSyncedAndUsedPasskeysApart(t *testing.T) {
	t.Parallel()
	added := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	deviceBound := describePasskey(&adminstore.Passkey{ID: []byte{1, 2}, Algorithm: passkey.ES256, CreatedAt: added})
	synced := describePasskey(&adminstore.Passkey{
		ID:             []byte{1, 2},
		Algorithm:      passkey.ES256,
		CreatedAt:      added,
		BackupEligible: true,
		LastUsedAt:     added.Add(time.Hour),
	})
	if deviceBound == synced || !strings.HasPrefix(synced, core.Encode([]byte{1, 2})+" ") {
		t.Fatalf("two different passkeys read the same: %q", synced)
	}
}

func TestTheAdminCommandsNeedTheAdminDatabaseOfServe(t *testing.T) {
	t.Parallel()
	withoutAdmin := t.TempDir()
	createStore(t, withoutAdmin)
	damaged := t.TempDir()
	createStore(t, damaged)
	if err := os.WriteFile(adminStorePath(damaged), bytes.Repeat([]byte("damaged"), 1000), 0o600); err != nil {
		t.Fatal(err)
	}
	refusing := adminStore(t)
	database, err := sql.Open("sqlite", "file:"+adminStorePath(refusing))
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO passkeys VALUES (x'01', x'00', 'damaged', 0, 0, 0, 1700000000, 0)`,
		`CREATE TRIGGER refuse_enrollment BEFORE INSERT ON enrollment BEGIN SELECT RAISE(ABORT, 'refused'); END`,
		`CREATE TRIGGER refuse_removal BEFORE DELETE ON passkeys BEGIN SELECT RAISE(ABORT, 'refused'); END`,
	} {
		if _, err = database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	database.Close()
	for name, variables := range map[string]map[string]string{
		"an invalid setting":        {"NYLZO_DATA_DIR": t.TempDir(), "NYLZO_RATE_SOURCE_BURST": "0.5"},
		"no store":                  {"NYLZO_DATA_DIR": t.TempDir()},
		"no admin database":         {"NYLZO_DATA_DIR": withoutAdmin},
		"a damaged admin database":  {"NYLZO_DATA_DIR": damaged},
		"a refusing admin database": {"NYLZO_DATA_DIR": refusing},
	} {
		for _, adminCommand := range []func(context.Context, *command.Environment, []string) int{
			RunAdminEnroll, RunAdminPasskeys, RunAdminReset,
		} {
			if result := run(t, "", variables, nil, adminCommand); result.code != command.ExitError ||
				result.stdout != "" {
				t.Errorf("%s: %+v", name, result)
			}
		}
	}
}

func TestTheAdminCommandsExplainThemselves(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	environment := &command.Environment{Stdout: &stdout, Stderr: new(bytes.Buffer), Getenv: getenvFrom(nil)}
	if code := RunAdminReset(t.Context(), environment, []string{"-h"}); code != command.ExitOK ||
		!strings.Contains(stdout.String(), "admin-reset") {
		t.Fatalf("admin-reset -h: exit %d, %s", code, stdout.String())
	}
}

package adminstore

import (
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/server/passkey"
)

func openStore(t *testing.T, path string) *Store {
	t.Helper()
	store, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func storePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), FileName)
}

func breakStore(t *testing.T, store *Store, statements ...string) {
	t.Helper()
	for _, statement := range statements {
		if _, err := store.database.ExecContext(t.Context(), statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
}

func unix(moment time.Time) string {
	return strconv.FormatInt(moment.Unix(), 10)
}

const (
	overflowingValue = `abs(-9223372036854775808)`
	refuseStatement  = `BEGIN SELECT RAISE(ABORT, 'refused'); END`
)

func TestOpenRefusesAFileItCannotServe(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	if _, err := Open(t.Context(), filepath.Join(directory, "missing", FileName)); err == nil {
		t.Fatal("a database in a missing directory opened")
	}
	garbage := filepath.Join(directory, "garbage.db")
	if err := os.WriteFile(garbage, []byte("this file holds no SQLite database, only text"), privateMode); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(t.Context(), garbage); err == nil {
		t.Fatal("a file that is no database opened")
	}

	newerPath := storePath(t)
	newer := openStore(t, newerPath)
	breakStore(t, newer, `PRAGMA user_version = 2`)
	if _, err := Open(t.Context(), newerPath); !errors.Is(err, errNewerSchema) {
		t.Fatalf("a database of a newer schema: %v", err)
	}

	damagedPath := storePath(t)
	damaged := openStore(t, damagedPath)
	breakStore(t, damaged, `DROP TABLE passkeys`)
	if _, err := Open(t.Context(), damagedPath); err == nil {
		t.Fatal("a database without its passkeys opened")
	}

	blockedPath := storePath(t)
	blocked := openStore(t, blockedPath)
	blocked.Close()
	journal := blockedPath + journalSuffix
	if err := os.Remove(journal); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(journal, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(t.Context(), blockedPath); err == nil {
		t.Fatal("a database whose journal cannot be overwritten opened")
	}
}

func TestOpenWithoutAJournalCreatesNone(t *testing.T) {
	t.Parallel()
	path := storePath(t)
	store := openStore(t, path)
	store.Close()
	if err := os.Remove(path + journalSuffix); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, statErr := os.Stat(path + journalSuffix); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("opening a stored database left a journal: %v", statErr)
	}
}

func TestAStoredChangeReportsAJournalItCouldNotOverwrite(t *testing.T) {
	t.Parallel()
	store := openStore(t, storePath(t))
	store.journal = t.TempDir()
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	token := HashToken([]byte("token"))
	if err := store.OpenEnrollment(t.Context(), token, now.Add(time.Minute)); !errors.Is(err, ErrJournalNotErased) {
		t.Fatalf("an enrollment with a journal that could not be overwritten: %v", err)
	}
	if open, err := store.EnrollmentOpen(t.Context(), token, now); err != nil || !open {
		t.Fatalf("the enrollment was not stored: %v, %v", open, err)
	}
	carried := []Count{{Start: now, Metric: "created", Value: 3}}
	if err := store.Carry(t.Context(), carried); !errors.Is(err, ErrJournalNotErased) {
		t.Fatalf("carrying with a journal that could not be overwritten: %v", err)
	}
	taken, err := store.TakeCarried(t.Context())
	if !errors.Is(err, ErrJournalNotErased) || len(taken) != 1 || taken[0].Value != 3 {
		t.Fatalf("carried counts were not handed back with the journal error: %v, %v", taken, err)
	}
}

func TestCountsReportAStoreThatFails(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	counts := []Count{{Start: now, Metric: "created", Value: 1}}
	cases := map[string]struct {
		statements []string
		operation  func(store *Store) error
	}{
		"adding without a table": {
			[]string{`DROP TABLE counts`},
			func(store *Store) error { return store.AddCounts(t.Context(), counts) },
		},
		"adding a refused row": {
			[]string{`CREATE TRIGGER refuse BEFORE INSERT ON counts ` + refuseStatement},
			func(store *Store) error { return store.AddCounts(t.Context(), counts) },
		},
		"reading without a table": {
			[]string{`DROP TABLE counts`},
			func(store *Store) error {
				_, err := store.Counts(t.Context(), Hourly, now.Add(-time.Hour), now.Add(time.Hour))
				return err
			},
		},
		"reading a damaged row": {
			[]string{`INSERT INTO counts VALUES (3600, ` + unix(Hourly.Start(now)) + `, 'created', 'many')`},
			func(store *Store) error {
				_, err := store.Counts(t.Context(), Hourly, now.Add(-time.Hour), now.Add(time.Hour))
				return err
			},
		},
		"carrying a refused row": {
			[]string{`CREATE TRIGGER refuse BEFORE INSERT ON carried ` + refuseStatement},
			func(store *Store) error { return store.Carry(t.Context(), counts) },
		},
		"taking back without a table": {
			[]string{`DROP TABLE carried`},
			func(store *Store) error {
				_, err := store.TakeCarried(t.Context())
				return err
			},
		},
		"taking back a damaged row": {
			[]string{`INSERT INTO carried VALUES (0, 'created', 'many')`},
			func(store *Store) error {
				_, err := store.TakeCarried(t.Context())
				return err
			},
		},
		"taking back a row that fails to compute": {
			[]string{
				`DROP TABLE carried`,
				`CREATE VIEW carried AS SELECT 0 AS period_start, 'created' AS metric, 1 AS value
					UNION ALL SELECT 1, 'opened', ` + overflowingValue,
			},
			func(store *Store) error {
				_, err := store.TakeCarried(t.Context())
				return err
			},
		},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := openStore(t, storePath(t))
			breakStore(t, store, test.statements...)
			if err := test.operation(store); err == nil || errors.Is(err, ErrJournalNotErased) {
				t.Fatalf("the failure was not reported: %v", err)
			}
		})
	}
}

func TestPasskeysReportAStoreThatFails(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	token := HashToken([]byte("token"))
	credential := &passkey.Credential{
		ID:        []byte("id"),
		PublicKey: []byte("key"),
		Algorithm: passkey.ES256,
		SignCount: 1,
	}
	openEnrollment := `INSERT INTO enrollment VALUES (1, x'` + hex.EncodeToString(
		token[:],
	) + `', ` + unix(
		now.Add(time.Minute),
	) + `)`
	enrolled := `INSERT INTO passkeys VALUES (x'6964', x'6b6579', -7, 0, 0, 0, ` + unix(now) + `, 0)`
	enroll := func(store *Store) error { return store.Enroll(t.Context(), token, credential, now) }
	cases := map[string]struct {
		statements []string
		operation  func(store *Store) error
	}{
		"checking an enrollment without a table": {
			[]string{`DROP TABLE enrollment`},
			func(store *Store) error {
				_, err := store.EnrollmentOpen(t.Context(), token, now)
				return err
			},
		},
		"enrolling without an enrollment table": {[]string{`DROP TABLE enrollment`}, enroll},
		"enrolling without a passkeys table":    {[]string{openEnrollment, `DROP TABLE passkeys`}, enroll},
		"enrolling against passkeys that fail to compute": {
			[]string{
				openEnrollment,
				`DROP TABLE passkeys`,
				`CREATE VIEW passkeys AS SELECT ` + overflowingValue + ` AS credential_id`,
			},
			enroll,
		},
		"enrolling when the enrollment cannot close": {
			[]string{openEnrollment, `CREATE TRIGGER refuse BEFORE DELETE ON enrollment ` + refuseStatement},
			enroll,
		},
		"listing without a table": {
			[]string{`DROP TABLE passkeys`},
			func(store *Store) error {
				_, err := store.Passkeys(t.Context())
				return err
			},
		},
		"listing a damaged row": {
			[]string{`INSERT INTO passkeys VALUES (x'6964', x'6b6579', 'strong', 0, 0, 0, 0, 0)`},
			func(store *Store) error {
				_, err := store.Passkeys(t.Context())
				return err
			},
		},
		"recording a refused use": {
			[]string{enrolled, `CREATE TRIGGER refuse BEFORE UPDATE ON passkeys ` + refuseStatement},
			func(store *Store) error {
				_, err := store.RecordUse(t.Context(), credential, now)
				return err
			},
		},
		"removing when the enrollment cannot close": {
			[]string{`CREATE TRIGGER refuse BEFORE DELETE ON enrollment ` + refuseStatement, openEnrollment},
			func(store *Store) error {
				_, err := store.RemovePasskeys(t.Context())
				return err
			},
		},
		"removing refused passkeys": {
			[]string{enrolled, `CREATE TRIGGER refuse BEFORE DELETE ON passkeys ` + refuseStatement},
			func(store *Store) error {
				_, err := store.RemovePasskeys(t.Context())
				return err
			},
		},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := openStore(t, storePath(t))
			breakStore(t, store, test.statements...)
			if err := test.operation(store); err == nil || errors.Is(err, ErrJournalNotErased) {
				t.Fatalf("the failure was not reported: %v", err)
			}
		})
	}
}

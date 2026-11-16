package adminstore_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/server/adminstore"
	"github.com/misaon/nylzo/internal/server/passkey"
)

// An enrollment enrolls one passkey, only with its own token and only before it runs out; a newer enrollment
// replaces it, and removing the passkeys closes it.
func TestEnrollmentTokenWorksOnceAndInTime(t *testing.T) {
	store, err := adminstore.Open(t.Context(), filepath.Join(t.TempDir(), adminstore.FileName))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	first, second := adminstore.HashToken([]byte("first")), adminstore.HashToken([]byte("second"))
	credential := func(id string) *passkey.Credential {
		return &passkey.Credential{ID: []byte(id), PublicKey: []byte("key"), Algorithm: passkey.ES256}
	}
	closed := func(token adminstore.TokenHash, at time.Time, id string) bool {
		return errors.Is(store.Enroll(t.Context(), token, credential(id), at), adminstore.ErrEnrollmentClosed)
	}
	if !closed(first, now, "a") {
		t.Fatal("a token nobody issued enrolled a passkey")
	}
	if err = store.OpenEnrollment(t.Context(), first, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if !closed(first, now.Add(time.Minute), "a") {
		t.Fatal("a token that ran out enrolled a passkey")
	}
	if err = store.OpenEnrollment(t.Context(), second, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if !closed(first, now, "a") {
		t.Fatal("a replaced token enrolled a passkey")
	}
	if err = store.Enroll(t.Context(), second, credential("a"), now); err != nil {
		t.Fatal(err)
	}
	if !closed(second, now, "b") {
		t.Fatal("a used token enrolled a second passkey")
	}
	if err = store.OpenEnrollment(t.Context(), first, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if removed, removeErr := store.RemovePasskeys(t.Context()); removeErr != nil || removed != 1 {
		t.Fatalf("removed %d, %v", removed, removeErr)
	}
	if !closed(first, now, "c") {
		t.Fatal("an enrollment survived the reset")
	}
}

// The store holds eight passkeys and no more and moves the signature count of a passkey only forward.
func TestPasskeysAreBoundedAndTheirCountOnlyGrows(t *testing.T) {
	path := filepath.Join(t.TempDir(), adminstore.FileName)
	store, err := adminstore.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	enroll := func(name string) error {
		token := adminstore.HashToken([]byte(name))
		if openErr := store.OpenEnrollment(t.Context(), token, now.Add(time.Minute)); openErr != nil {
			t.Fatal(openErr)
		}
		credential := &passkey.Credential{ID: []byte(name), PublicKey: []byte("key"), Algorithm: passkey.ES256}
		return store.Enroll(t.Context(), token, credential, now)
	}
	for held := range adminstore.MaxPasskeys {
		if err = enroll(string(rune('a' + held))); err != nil {
			t.Fatal(err)
		}
	}
	if err = enroll("ninth"); !errors.Is(err, adminstore.ErrTooManyPasskeys) {
		t.Fatalf("a ninth passkey: %v", err)
	}
	if held, countErr := store.CountPasskeys(t.Context()); countErr != nil || held != adminstore.MaxPasskeys {
		t.Fatalf("%d passkeys, %v", held, countErr)
	}
	use := func(id string, count uint32) bool {
		recorded, useErr := store.RecordUse(t.Context(), &passkey.Credential{ID: []byte(id), SignCount: count}, now)
		if useErr != nil {
			t.Fatal(useErr)
		}
		return recorded
	}
	for range 2 {
		if !use("a", 0) {
			t.Fatal("a passkey that keeps no count could not sign in twice")
		}
	}
	if !use("a", 2) || use("a", 2) || use("a", 1) {
		t.Fatal("a signature count that did not grow was recorded")
	}
	if use("gone", 5) {
		t.Fatal("a passkey the store does not hold was recorded")
	}
}

// What a write replaced stays nowhere on disk: the store has no write-ahead log, its journal is zeros after
// every write, and the database holds no removed row.
func TestRemovedRowsLeaveNoCopyBehind(t *testing.T) {
	path := filepath.Join(t.TempDir(), adminstore.FileName)
	store, err := adminstore.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	const removedKey, removedMetric = "public-key-of-a-removed-passkey", "page.removed-after-its-retention"
	token := adminstore.HashToken([]byte("token"))
	if err = store.OpenEnrollment(t.Context(), token, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	credential := &passkey.Credential{ID: []byte("id"), PublicKey: []byte(removedKey), Algorithm: passkey.ES256}
	if err = store.Enroll(t.Context(), token, credential, now); err != nil {
		t.Fatal(err)
	}
	counts := []adminstore.Count{{Start: now, Metric: removedMetric, Value: 7}}
	if err = errors.Join(store.AddCounts(t.Context(), counts), store.Carry(t.Context(), counts)); err != nil {
		t.Fatal(err)
	}
	if _, err = store.RemovePasskeys(t.Context()); err != nil {
		t.Fatal(err)
	}
	carried, err := store.TakeCarried(t.Context())
	if err != nil || len(carried) != 1 || carried[0].Value != 7 {
		t.Fatalf("carried counts: %v, %v", carried, err)
	}
	if again, takeErr := store.TakeCarried(t.Context()); takeErr != nil || len(again) != 0 {
		t.Fatalf("carried counts were handed back twice: %v, %v", again, takeErr)
	}
	if err = store.ForgetCounts(t.Context(), now.Add(time.Hour), now.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, file := range adminstore.Files(path) {
		content, readErr := os.ReadFile(file)
		if readErr != nil {
			t.Fatal(readErr)
		}
		for _, needle := range []string{removedKey, removedMetric} {
			if bytes.Contains(content, []byte(needle)) {
				t.Fatalf("%s still holds %q", filepath.Base(file), needle)
			}
		}
	}
	journal, err := os.ReadFile(path + "-journal")
	if err != nil || len(journal) == 0 || len(bytes.Trim(journal, "\x00")) != 0 {
		t.Fatalf("the journal holds %d bytes that are not all zero, %v", len(journal), err)
	}
	for _, file := range []string{path + "-wal", path + "-shm"} {
		if _, statErr := os.Stat(file); statErr == nil {
			t.Fatalf("%s exists", filepath.Base(file))
		}
	}
}

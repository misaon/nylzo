package server

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/sha256"
	"errors"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/signing"
	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/store"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

var pepperC = []byte("a third test pepper of 32 bytes!")

// A public key is no secret from whoever records signed requests, so a copy of the database together with it must
// still open nothing without the pepper.
func TestADatabaseCopyAndARecordedPublicKeyOpenNoRecord(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFNone, "", 1, 3600, "metadata of a recorded request")
		harness.mustCreate(message)
		if response, _ := harness.metadata(message); response.StatusCode != http.StatusOK {
			t.Fatalf("metadata: %d", response.StatusCode)
		}
		row, err := harness.store.Load(t.Context(), seal.LocatorOf(message.id[:]))
		if err != nil {
			t.Fatal(err)
		}
		readKey, manageKey := signing.PublicKey(&message.link.R), signing.PublicKey(message.manage)
		for name, attempt := range map[string]struct {
			pepperKey seal.Key
			publicKey []byte
			wrapped   []byte
			purpose   seal.Purpose
		}{
			"pk_R without the pepper":    {seal.Key{}, readKey[:], row.ReadWrap, seal.Read},
			"pk_M without the pepper":    {seal.Key{}, manageKey[:], row.ManageWrap, seal.Manage},
			"pk_R under another pepper":  {derivedRecordsKey(t, pepperB), readKey[:], row.ReadWrap, seal.Read},
			"pk_M under another pepper":  {derivedRecordsKey(t, pepperB), manageKey[:], row.ManageWrap, seal.Manage},
			"the pepper without the key": {derivedRecordsKey(t, pepperA), core.NewSecret()[:], row.ReadWrap, seal.Read},
		} {
			if _, unwrapErr := seal.UnwrapRecordKey(
				attempt.pepperKey,
				attempt.publicKey,
				attempt.wrapped,
				row.Locator,
				attempt.purpose,
			); unwrapErr == nil {
				t.Fatalf("%s opened the record", name)
			}
		}
		if unwrapErr := legacyUnwrap(row.ReadWrap, readKey[:], row.Locator); unwrapErr == nil {
			t.Fatal("the stored read wrap opens the way a wrap of an earlier release did, with pk_R alone")
		}
		if _, err = seal.UnwrapRecordKey(
			derivedRecordsKey(t, pepperA),
			readKey[:],
			row.ReadWrap,
			row.Locator,
			seal.Read,
		); err != nil {
			t.Fatalf("pk_R and the pepper must open the record: %v", err)
		}
	})
}

func TestServeSealsTheRowsOfAnEarlierRelease(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		if strings.Contains(harness.logs.String(), `"msg":"rows-sealed"`) {
			t.Fatalf("a fresh store reported sealed rows: %s", harness.logs.String())
		}
		message := newMessage(t, core.KDFNone, "", 2, 3600, "stored by nylzo 0.13")
		harness.mustCreate(message)
		record := harness.mustRecord(message)
		readKey, manageKey := signing.PublicKey(&message.link.R), signing.PublicKey(message.manage)
		err := harness.store.Update(t.Context(), record.Row.Locator, func(row *store.Row) error {
			row.ReadWrap = legacyWrap(t, readKey[:], record.Key, row.Locator, seal.Read)
			row.ManageWrap = legacyWrap(t, manageKey[:], record.Key, row.Locator, seal.Manage)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		before := len(harness.logs.String())
		harness.reopen(&harness.settings)
		if !strings.Contains(harness.logs.String()[before:], `"msg":"rows-sealed"`) {
			t.Fatalf("the start did not report the sealed rows: %s", harness.logs.String()[before:])
		}
		if response, metadata := harness.metadata(message); response.StatusCode != http.StatusOK ||
			metadata["state"] != "available" {
			t.Fatalf("metadata of a row of an earlier release: %d %v", response.StatusCode, metadata)
		}
		if response, _ := harness.status(message); response.StatusCode != http.StatusOK {
			t.Fatalf("status of a row of an earlier release: %d", response.StatusCode)
		}
		row, err := harness.store.Load(t.Context(), record.Row.Locator)
		if err != nil {
			t.Fatal(err)
		}
		if len(row.ReadWrap) != seal.SealedWrapLen || len(row.ManageWrap) != seal.SealedWrapLen {
			t.Fatalf("the wraps were not sealed: %d and %d bytes", len(row.ReadWrap), len(row.ManageWrap))
		}
		if err = legacyUnwrap(row.ReadWrap, readKey[:], row.Locator); err == nil {
			t.Fatal("pk_R alone still opens the sealed wrap")
		}
	})
}

func TestARotationReSealsEveryRow(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFNone, "", 2, 3600, "sealed under pepper A")
		harness.mustCreate(message)
		harness.reopen(baseSettings(pepperC, pepperA))
		if _, ok := harness.server.ring.PreviousRecordsKey(); ok {
			t.Fatal("the records key of the previous pepper outlived the start")
		}
		harness.reopen(baseSettings(pepperC, nil))
		if response, _ := harness.metadata(message); response.StatusCode != http.StatusOK {
			t.Fatalf("a row resealed under the current pepper: %d", response.StatusCode)
		}
		row, err := harness.store.Load(t.Context(), seal.LocatorOf(message.id[:]))
		if err != nil {
			t.Fatal(err)
		}
		readKey := signing.PublicKey(&message.link.R)
		if _, err = seal.UnwrapRecordKey(
			derivedRecordsKey(t, pepperA),
			readKey[:],
			row.ReadWrap,
			row.Locator,
			seal.Read,
		); err == nil {
			t.Fatal("the previous pepper still opens a resealed row")
		}
	})
}

func TestAMissingPreviousPepperClosesTheRowsUntilItIsConfigured(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFNone, "", 1, 3600, "sealed under pepper A")
		harness.mustCreate(message)
		before := len(harness.logs.String())
		harness.reopen(baseSettings(pepperC, nil))
		if !strings.Contains(harness.logs.String()[before:], `"msg":"rows-unopenable"`) {
			t.Fatalf("the start did not report the closed rows: %s", harness.logs.String()[before:])
		}
		if response, _ := harness.metadata(message); response.StatusCode != http.StatusNotFound {
			t.Fatalf("a row that no configured pepper opens: %d", response.StatusCode)
		}
		harness.reopen(baseSettings(pepperC, pepperA))
		if response, _ := harness.metadata(message); response.StatusCode != http.StatusOK {
			t.Fatalf("the row once its pepper is the previous one: %d", response.StatusCode)
		}
	})
}

func TestARowThatNoPepperOpensStaysAsItIs(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		harness.mustCreate(newMessage(t, core.KDFNone, "", 1, 3600, "sealed under pepper A"))
		id := core.NewID()
		foreign := store.NewRecord(
			derivedRecordsKey(t, pepperC),
			seal.LocatorOf(id[:]),
			&store.Message{
				Version:        2,
				OpensMax:       1,
				OpensRemaining: 1,
				CreatedAt:      time.Now(),
				ExpiresAt:      time.Now().Add(time.Hour),
			},
			core.NewSecret()[:],
			core.NewSecret()[:],
			"",
			new(seal.NewBlobKey()),
		)
		if err := harness.store.Create(t.Context(), &foreign.Row); err != nil {
			t.Fatal(err)
		}
		before := len(harness.logs.String())
		harness.reopen(baseSettings(pepperA, nil))
		if !strings.Contains(harness.logs.String()[before:], `"msg":"rows-unopenable"`) {
			t.Fatalf("the start did not report the closed row: %s", harness.logs.String()[before:])
		}
		row, err := harness.store.Load(t.Context(), foreign.Row.Locator)
		if err != nil || !bytes.Equal(row.ReadWrap, foreign.Row.ReadWrap) {
			t.Fatalf("a row that no pepper opens changed: %v", err)
		}
	})
}

// A wrap built by hand from spec §5.1 and §5.2 opens with the server's code, so the layer cannot drift from the spec.
func TestTheWrapLayersFollowTheSpec(t *testing.T) {
	t.Parallel()
	recordKey, locator, publicKey := seal.NewKey(), seal.LocatorOf([]byte("message")), []byte("pk_R")
	aead := func(ikm, salt []byte, info string) cipher.AEAD {
		key, err := hkdf.Key(sha256.New, ikm, salt, info, seal.KeyLen)
		if err != nil {
			t.Fatal(err)
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			t.Fatal(err)
		}
		gcm, err := cipher.NewGCMWithRandomNonce(block)
		if err != nil {
			t.Fatal(err)
		}
		return gcm
	}
	aad := func(label string) []byte { return append(locator[:len(locator):len(locator)], label...) }
	kRecords, err := hkdf.Key(sha256.New, pepperA, nil, "nylzo/v1/records", seal.KeyLen)
	if err != nil {
		t.Fatal(err)
	}
	inner := aead(publicKey, locator[:], "nylzo/v1/record/read").Seal(nil, nil, recordKey[:], aad("read"))
	wrapped := aead(kRecords, locator[:], "nylzo/v2/record/pepper").Seal(nil, nil, inner, aad("pepper-read"))
	opened, err := seal.UnwrapRecordKey(derivedRecordsKey(t, pepperA), publicKey, wrapped, locator, seal.Read)
	if err != nil || opened != recordKey {
		t.Fatalf("a wrap built from the spec does not open: %v", err)
	}
	salt, registration := seal.NewEraseSalt(), bytes.Repeat([]byte{7}, seal.RegistrationLen)
	registrationWrap := aead(salt[:], locator[:], "nylzo/v3/record/registration").
		Seal(nil, nil, registration, aad("registration"))
	unwrapped, err := seal.UnwrapRegistration(&salt, locator, registrationWrap)
	if err != nil || !bytes.Equal(unwrapped, registration) {
		t.Fatalf("a registration wrap built from the spec does not open: %v", err)
	}
	otherSalt := seal.NewEraseSalt()
	if _, err = seal.UnwrapRegistration(&otherSalt, locator, registrationWrap); err == nil {
		t.Fatal("another erase salt opened the registration")
	}
}

// failingRebuild seals rows as the store does and then fails, as a rebuild that a reader blocks would.
type failingRebuild struct {
	store.Store
}

func (f failingRebuild) ResealWraps(ctx context.Context, reseal func(row *store.Row) bool) error {
	return errors.Join(f.Store.ResealWraps(ctx, reseal), sqlite.ErrCheckpointBusy)
}

func TestAFailedRebuildSaysThatRowsWereSealed(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFNone, "", 1, 3600, "stored by nylzo 0.13")
		harness.mustCreate(message)
		record := harness.mustRecord(message)
		readKey, manageKey := signing.PublicKey(&message.link.R), signing.PublicKey(message.manage)
		err := harness.store.Update(t.Context(), record.Row.Locator, func(row *store.Row) error {
			row.ReadWrap = legacyWrap(t, readKey[:], record.Key, row.Locator, seal.Read)
			row.ManageWrap = legacyWrap(t, manageKey[:], record.Key, row.Locator, seal.Manage)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		settings := baseSettings(pepperA, nil)
		settings.DataDirectory = harness.directory
		restarted, err := New(settings, failingRebuild{harness.store}, harness.blobs)
		if err != nil {
			t.Fatal(err)
		}
		defer restarted.Close()
		if err = restarted.SealRows(t.Context()); !errors.Is(err, errSealedBeforeFailure) {
			t.Fatalf("a failed start after sealing must say that it changed the store: %v", err)
		}
	})
}

func derivedRecordsKey(t *testing.T, secret []byte) seal.Key {
	t.Helper()
	ring, err := pepper.NewRing(secret, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ring.RecordsKey()
}

// legacyWrap builds a wrap as nylzo 0.13 and earlier stored it (spec §5.2 of that release).
func legacyWrap(t *testing.T, publicKey []byte, recordKey seal.Key, locator seal.Locator, purpose seal.Purpose) []byte {
	t.Helper()
	aead, err := legacyAEAD(publicKey, locator, purpose)
	if err != nil {
		t.Fatal(err)
	}
	return aead.Seal(nil, nil, recordKey[:], append(locator[:len(locator):len(locator)], purpose...))
}

func legacyUnwrap(wrapped, publicKey []byte, locator seal.Locator) error {
	aead, err := legacyAEAD(publicKey, locator, seal.Read)
	if err != nil {
		return err
	}
	_, err = aead.Open(nil, nil, wrapped, append(locator[:len(locator):len(locator)], seal.Read...))
	return err
}

func legacyAEAD(publicKey []byte, locator seal.Locator, purpose seal.Purpose) (cipher.AEAD, error) {
	key, err := hkdf.Key(sha256.New, publicKey, locator[:], "nylzo/v1/record/"+string(purpose), seal.KeyLen)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCMWithRandomNonce(block)
}

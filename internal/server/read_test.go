package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/core/envelope"
	"github.com/misaon/nylzo/internal/core/opaque"
	"github.com/misaon/nylzo/internal/core/signing"
	"github.com/misaon/nylzo/internal/epoch"
	"github.com/misaon/nylzo/internal/erase"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/server/config"
	"github.com/misaon/nylzo/internal/server/ratelimit"
	"github.com/misaon/nylzo/internal/store"
)

// F13: a share goes at the deadline of its message. When another process whose clock runs a little ahead has moved
// the ratchet past it first, a claim answers the state the message has at its deadline and ends nothing early; at the
// deadline the message is expired, not deleted.
func TestAShareGoneAtTheDeadlineExpiresTheMessage(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := harness.issueMessage(core.KDFNone, "", 2, 600, "until the deadline")
		harness.mustCreate(message)
		deadline := epoch.Deadline(message.epoch)
		synctest.Sleep(time.Until(deadline) - 20*time.Second)
		if err := harness.server.advanceRatchet(deadline.Add(10 * time.Second)); err != nil {
			t.Fatal(err)
		}
		if response, body := harness.claim(message, message.passphrase, ""); response.StatusCode != http.StatusGone ||
			decodeJSON(t, body)["state"] != "expired" {
			t.Fatalf("a claim whose share went with a peer's clock: %d %s", response.StatusCode, body)
		}
		if _, metadata := harness.metadata(message); metadata["state"] != "available" || !harness.blobExists(message) {
			t.Fatalf("the claim ended the message before its deadline: %v", metadata)
		}
		synctest.Sleep(time.Until(deadline))
		if response, body := harness.claim(message, message.passphrase, ""); response.StatusCode != http.StatusGone ||
			decodeJSON(t, body)["state"] != "expired" {
			t.Fatalf("a claim at the deadline: %d %s", response.StatusCode, body)
		}
		if _, metadata := harness.metadata(message); metadata["state"] != "expired" || harness.blobExists(message) {
			t.Fatalf("the message must have expired at its deadline: %v", metadata)
		}
	})
}

// F13: a reader of a release before Nylzo 0.14.0 cannot open an envelope that seals its deadline, so the server refuses
// its claim and download before anything changes; a message of an earlier release, which seals none, it still serves,
// with a policy that names no deadline.
func TestAnOldReaderSpendsNoOpenOnADeadlineMessage(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := harness.issueMessage(core.KDFNone, "", 1, 3600, "sealed deadline")
		harness.mustCreate(message)
		claim, _, _ := harness.claimRequestFor(message, message.passphrase, "")
		claim.oldReader = true
		if response, body := harness.do(claim); response.StatusCode != http.StatusBadRequest ||
			decodeJSON(t, body)["error"] != api.ErrorClientOutdated {
			t.Fatalf("an old reader's claim: %d %s", response.StatusCode, body)
		}
		for _, malformed := range []string{"", "02", "256", "two"} {
			claim, _, _ = harness.claimRequestFor(message, message.passphrase, "")
			claim.headers = map[string]string{api.FlagsHeaderName: malformed}
			if response, body := harness.do(claim); response.StatusCode != http.StatusBadRequest ||
				decodeJSON(t, body)["error"] != "bad_request" {
				t.Fatalf("a claim with X-Nylzo-Flags %q: %d %s", malformed, response.StatusCode, body)
			}
		}
		if _, metadata := harness.metadata(message); metadata["opens_remaining"] != float64(1) {
			t.Fatalf("a refused claim spent an open: %v", metadata)
		}
		response, _ := harness.claim(message, message.passphrase, "")
		if response.StatusCode != http.StatusOK {
			t.Fatalf("a current reader's claim: %d", response.StatusCode)
		}
		download := &testRequest{
			method:    http.MethodGet,
			path:      "/api/v1/messages/" + message.id.String() + "/blob",
			token:     message.readToken,
			headers:   map[string]string{api.LeaseHeaderName: response.Header.Get(api.LeaseHeaderName)},
			oldReader: true,
		}
		if got, body := harness.do(download); got.StatusCode != http.StatusBadRequest ||
			decodeJSON(t, body)["error"] != api.ErrorClientOutdated {
			t.Fatalf("an old reader's download: %d %s", got.StatusCode, body)
		}
		legacy := harness.issueMessage(core.KDFNone, "", 1, 3600, "an earlier release")
		harness.mustCreate(legacy)
		harness.rewrite(legacy, func(stored *store.Message) { stored.Flags = 0 })
		claim, _, _ = harness.claimRequestFor(legacy, legacy.passphrase, "")
		claim.oldReader = true
		served, _ := harness.do(claim)
		policy, err := base64.RawURLEncoding.DecodeString(served.Header.Get(api.PolicyHeaderName))
		if served.StatusCode != http.StatusOK || err != nil || strings.Contains(string(policy), "deadline") {
			t.Fatalf("a message of an earlier release: %d %s", served.StatusCode, policy)
		}
	})
}

// A message whose open is spent goes like an unspent one: a download whose share went with a peer's clock answers the
// state at the deadline and changes nothing, and one whose seed is lost ends the message as lost.
func TestASpentMessageLosesItsShareLikeALiveOne(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		early := harness.issueMessage(core.KDFNone, "", 1, 600, "spent before the deadline")
		harness.mustCreate(early)
		deadline := epoch.Deadline(early.epoch)
		synctest.Sleep(time.Until(deadline) - 50*time.Second)
		response, _ := harness.claim(early, early.passphrase, "")
		if response.StatusCode != http.StatusOK {
			t.Fatalf("claim: %d", response.StatusCode)
		}
		lease := response.Header.Get(api.LeaseHeaderName)
		synctest.Sleep(30 * time.Second)
		if err := harness.server.advanceRatchet(deadline.Add(10 * time.Second)); err != nil {
			t.Fatal(err)
		}
		if got, body := harness.blob(early, lease); got.StatusCode != http.StatusGone ||
			decodeJSON(t, body)["state"] != "opened" || !harness.blobExists(early) {
			t.Fatalf("a download whose share went with a peer's clock: %d %s", got.StatusCode, body)
		}
		lost := harness.issueMessage(core.KDFNone, "", 1, 3600, "spent before the loss")
		harness.mustCreate(lost)
		response, _ = harness.claim(lost, lost.passphrase, "")
		if response.StatusCode != http.StatusOK {
			t.Fatalf("claim: %d", response.StatusCode)
		}
		if err := os.Remove(filepath.Join(harness.settings.EpochDirectory(), epoch.FileName)); err != nil {
			t.Fatal(err)
		}
		harness.reopen(&harness.settings)
		if got, body := harness.blob(
			lost,
			response.Header.Get(api.LeaseHeaderName),
		); got.StatusCode != http.StatusGone ||
			decodeJSON(t, body)["state"] != "lost" ||
			harness.blobExists(lost) {
			t.Fatalf("a download whose seed is lost: %d %s", got.StatusCode, body)
		}
	})
}

// A lost generation whose epoch another process has moved the shared seed file past went with the deadline: metadata
// and status, which read that file before they decide, end nothing, and a claim answers the state at the deadline, as
// for a generation still held.
func TestALostShareThatWentWithItsDeadlineIsNotLost(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := harness.issueMessage(core.KDFNone, "", 1, 600, "passed before the loss shows")
		harness.mustCreate(message)
		directory := harness.settings.EpochDirectory()
		if err := os.Remove(filepath.Join(directory, epoch.FileName)); err != nil {
			t.Fatal(err)
		}
		harness.reopen(&harness.settings)
		peer, _, err := epoch.Open(directory, epoch.Deadline(message.epoch).Add(10*time.Second), epoch.Options{})
		if err != nil || peer == nil {
			t.Fatalf("open the shared seed file as another process: %v", err)
		}
		peer.Close()
		if response, metadata := harness.metadata(message); response.StatusCode != http.StatusOK ||
			metadata["state"] != "available" || !harness.blobExists(message) {
			t.Fatalf("metadata of a share that went with its deadline: %d %v", response.StatusCode, metadata)
		}
		if response, status := harness.status(message); response.StatusCode != http.StatusOK ||
			status["state"] != "available" || !harness.blobExists(message) {
			t.Fatalf("status of a share that went with its deadline: %d %v", response.StatusCode, status)
		}
		if response, body := harness.claim(message, message.passphrase, ""); response.StatusCode != http.StatusGone ||
			decodeJSON(t, body)["state"] != "expired" || !harness.blobExists(message) {
			t.Fatalf("a claim of a share that went with its deadline: %d %s", response.StatusCode, body)
		}
	})
}

// A seed file damaged while the server runs ends nothing as lost: metadata and status cannot tell a lost generation
// from an unreadable file, so they report the stored state; a cancel and a report, which need no share, go ahead as
// before, and a burn, whose handshake needs the share, answers 500 and keeps the message.
func TestAnUnreadableSeedEndsNoMessage(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := harness.issueMessage(core.KDFNone, "", 1, 3600, "damaged while serving")
		harness.mustCreate(message)
		cancelled := harness.issueMessage(core.KDFNone, "", 1, 3600, "cancelled while damaged")
		harness.mustCreate(cancelled)
		burned := harness.issueMessage(core.KDFNone, "", 1, 3600, "deleted while damaged")
		harness.mustCreate(burned)
		reported := harness.issueMessage(core.KDFNone, "", 1, 3600, "reported while damaged")
		harness.mustCreate(reported)
		seedPath := filepath.Join(harness.settings.EpochDirectory(), epoch.FileName)
		if err := os.WriteFile(seedPath, []byte("damaged"), 0o600); err != nil {
			t.Fatal(err)
		}
		synctest.Sleep(time.Minute)
		if response, metadata := harness.metadata(message); response.StatusCode != http.StatusOK ||
			metadata["state"] != "available" || !harness.blobExists(message) {
			t.Fatalf("metadata with an unreadable seed: %d %v", response.StatusCode, metadata)
		}
		if response, status := harness.status(message); response.StatusCode != http.StatusOK ||
			status["state"] != "available" || !harness.blobExists(message) {
			t.Fatalf("status with an unreadable seed: %d %v", response.StatusCode, status)
		}
		if response, _ := harness.post(cancelled, "cancel", cancelled.manageToken); response.StatusCode !=
			http.StatusNoContent || harness.blobExists(cancelled) {
			t.Fatalf("a cancel with an unreadable seed: %d", response.StatusCode)
		}
		if response, _ := harness.post(burned, "burn", burned.readToken); response.StatusCode !=
			http.StatusInternalServerError || !harness.blobExists(burned) {
			t.Fatalf("a burn with an unreadable seed: %d", response.StatusCode)
		}
		abuseReport(t, harness, reported, "203.0.113.70")
		if lines := abuseLines(t, harness, reported.id); lines != 1 {
			t.Fatalf("a report with an unreadable seed was stored %d times", lines)
		}
	})
}

// A clock that stepped back behind the ratchet, which never goes back, gets no id until it catches up.
func TestAnInstanceBehindTheRatchetIssuesNoID(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		if err := harness.server.advanceRatchet(time.Now().Add(3 * time.Minute)); err != nil {
			t.Fatal(err)
		}
		request := &testRequest{method: http.MethodGet, path: "/api/v1/instance", headers: map[string]string{
			api.TTLHeaderName:          "3600",
			api.ShareKeyHeaderName:     testShareKeyHeader,
			api.RegistrationHeaderName: testRegistrationRequest,
		}}
		if response, body := harness.do(request); response.StatusCode != http.StatusServiceUnavailable ||
			response.Header.Get("Retry-After") != "60" {
			t.Fatalf("an instance behind the ratchet: %d %s", response.StatusCode, body)
		}
	})
}

// L3: a lost seed ends the live messages as lost at the first request that finds them, metadata and status included,
// and the server goes on with a new seed; a damaged seed or a missing directory, which may be a volume that failed to
// mount, stops it (F21).
func TestLostSeedEndsLiveMessagesAndRecovers(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		lost := harness.issueMessage(core.KDFNone, "", 2, 3600, "before the loss")
		harness.mustCreate(lost)
		tracked := harness.issueMessage(core.KDFNone, "", 1, 3600, "tracked by its sender")
		harness.mustCreate(tracked)
		cancelled := harness.issueMessage(core.KDFNone, "", 1, 3600, "cancelled by its sender")
		harness.mustCreate(cancelled)
		burned := harness.issueMessage(core.KDFNone, "", 1, 3600, "deleted by its reader")
		harness.mustCreate(burned)
		claimed := harness.issueMessage(core.KDFNone, "", 1, 3600, "claimed first")
		harness.mustCreate(claimed)
		reported := harness.issueMessage(core.KDFNone, "", 1, 3600, "reported after the loss")
		harness.mustCreate(reported)
		seedPath := filepath.Join(harness.settings.EpochDirectory(), epoch.FileName)
		if err := os.Remove(seedPath); err != nil {
			t.Fatal(err)
		}
		harness.reopen(&harness.settings)
		if !strings.Contains(harness.logs.String(), "epoch-seed-created") {
			t.Fatal("the new ratchet was not logged")
		}
		if _, metadata := harness.metadata(lost); metadata["state"] != "lost" || metadata["ended_at"] == nil ||
			harness.blobExists(lost) {
			t.Fatalf("metadata must end the message whose share is lost: %v", metadata)
		}
		if response, body := harness.claim(lost, lost.passphrase, ""); response.StatusCode != http.StatusGone ||
			decodeJSON(t, body)["state"] != "lost" {
			t.Fatalf("a claim whose share is lost: %d %s", response.StatusCode, body)
		}
		if _, status := harness.status(tracked); status["state"] != "lost" || status["ended_at"] == nil ||
			harness.blobExists(tracked) {
			t.Fatalf("status must end the message whose share is lost: %v", status)
		}
		for _, ending := range []struct {
			message       *testMessage
			action, token string
		}{{cancelled, "cancel", cancelled.manageToken}, {burned, "burn", burned.readToken}} {
			if response, body := harness.post(ending.message, ending.action, ending.token); response.StatusCode !=
				http.StatusGone || decodeJSON(t, body)["state"] != "lost" || harness.blobExists(ending.message) {
				t.Fatalf("a %s of a message whose share is lost: %d %s", ending.action, response.StatusCode, body)
			}
		}
		if response, body := harness.claim(claimed, claimed.passphrase, ""); response.StatusCode != http.StatusGone ||
			decodeJSON(t, body)["state"] != "lost" || harness.blobExists(claimed) {
			t.Fatalf("a first claim whose share is lost: %d %s", response.StatusCode, body)
		}
		abuseReport(t, harness, reported, "203.0.113.70")
		if lines := abuseLines(t, harness, reported.id); lines != 0 || harness.blobExists(reported) {
			t.Fatalf("a report about a message whose share is lost was stored %d times", lines)
		}
		synctest.Sleep(time.Until(epoch.Deadline(tracked.epoch)) + time.Minute)
		if _, status := harness.status(tracked); status["state"] != "lost" {
			t.Fatalf("a lost message must stay lost after its deadline: %v", status)
		}
		fresh := harness.issueMessage(core.KDFNone, "", 1, 3600, "after the loss")
		harness.mustCreate(fresh)
		response, object := harness.claim(fresh, fresh.passphrase, "")
		if response.StatusCode != http.StatusOK {
			t.Fatalf("a message after the loss: %d", response.StatusCode)
		}
		if _, err := harness.openObject(fresh, object, response.Header.Get(api.PolicyHeaderName)); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(seedPath, []byte("damaged"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := New(&harness.settings, harness.store, harness.blobs); !errors.Is(err, epoch.ErrCorrupt) {
			t.Fatalf("a damaged seed must stop the server: %v", err)
		}
		harness.settings.EpochDirectoryOverride = filepath.Join(t.TempDir(), "unmounted")
		if _, err := New(&harness.settings, harness.store, harness.blobs); !errors.Is(err, epoch.ErrNoDirectory) {
			t.Fatalf("a missing epoch directory must stop the server: %v", err)
		}
	})
}

// F18: once NYLZO_EPOCH_DIR names another directory, the epoch directory of the data directory stops the server for as
// long as it exists, because every backup of the data directory keeps whatever seed it holds, under any name.
func TestAnEpochDirectoryLeftInTheDataDirectoryStopsTheServer(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		quiet := WithLogger(slog.New(slog.DiscardHandler))
		moved := harness.settings
		moved.EpochDirectoryOverride = t.TempDir()
		movedSeed := filepath.Join(moved.EpochDirectoryOverride, epoch.FileName)
		refused := func(situation string, offersCopy bool) {
			t.Helper()
			_, err := New(&moved, harness.store, harness.blobs, quiet)
			advice := strings.Contains(fmt.Sprint(err), "copy its seed")
			if !errors.Is(err, ErrEpochInDataDirectory) || advice != offersCopy {
				t.Fatalf("%s: %v", situation, err)
			}
		}
		refused("a seed left in the data directory", true)
		if _, err := os.Lstat(movedSeed); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("the refused start wrote a seed: %v", err)
		}
		named := harness.settings
		named.EpochDirectoryOverride = named.DataEpochDirectory()
		server, err := New(&named, harness.store, harness.blobs, quiet)
		if err != nil {
			t.Fatalf("the epoch directory of the data directory, named: %v", err)
		}
		server.Close()
		if err = os.WriteFile(movedSeed, []byte("the seed in use"), 0o600); err != nil {
			t.Fatal(err)
		}
		refused("a seed in both directories, which a copy would overwrite", false)
		if err = os.Remove(movedSeed); err != nil {
			t.Fatal(err)
		}
		left := harness.settings.DataEpochDirectory()
		if err = os.Rename(filepath.Join(left, epoch.FileName), filepath.Join(left, "seed.old")); err != nil {
			t.Fatal(err)
		}
		refused("a seed under another name", false)
		if err = os.RemoveAll(left); err != nil {
			t.Fatal(err)
		}
		if server, err = New(&moved, harness.store, harness.blobs, quiet); err != nil {
			t.Fatalf("a data directory without its epoch directory: %v", err)
		}
		server.Close()
	})
}

func TestASeedOffTmpfsIsLoggedAtStart(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		onTmpfs, err := erase.OnTmpfs(harness.settings.EpochDirectory())
		if err != nil {
			t.Fatal(err)
		}
		if logged := strings.Contains(harness.logs.String(), "epoch-seed-on-disk"); logged == onTmpfs {
			t.Fatalf("seed on a tmpfs: %v, epoch-seed-on-disk logged: %v", onTmpfs, logged)
		}
	})
}

func TestFlowCreateMetadataClaimBlobAck(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFNone, "", 1, 86400, "hello")
		harness.mustCreate(message)
		for _, userAgent := range []string{"", "Slackbot-LinkExpanding 1.0"} {
			harness.do(
				&testRequest{
					method:  http.MethodGet,
					path:    "/m/" + message.id.String(),
					headers: map[string]string{"User-Agent": userAgent},
				},
			)
		}
		for range 5 {
			response, metadata := harness.metadata(message)
			if response.StatusCode != http.StatusOK || metadata["opens_remaining"].(float64) != 1 ||
				metadata["state"] != "available" ||
				metadata["passphrase_required"] != nil ||
				metadata["kdf_id"].(float64) != 0 {
				t.Fatalf("metadata %d: %v", response.StatusCode, metadata)
			}
		}
		response, object := harness.claim(message, "", "")
		if response.StatusCode != http.StatusOK {
			t.Fatalf("claim: %d %s", response.StatusCode, object)
		}
		lease := response.Header.Get("X-Nylzo-Lease")
		policy := response.Header.Get("X-Nylzo-Policy")
		if lease == "" || policy == "" || response.Header.Get("Content-Type") != "application/octet-stream" {
			t.Fatal("claim response headers")
		}
		payload, err := harness.openObject(message, object, policy)
		if err != nil || payload.Meta.Text == nil || *payload.Meta.Text != "hello" {
			t.Fatalf("decrypt: %v", err)
		}
		decoded, err := decodePolicy(policy)
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range []envelope.Policy{
			{OpensMax: decoded.OpensMax + 1, TTLSeconds: decoded.TTLSeconds},
			{OpensMax: decoded.OpensMax, TTLSeconds: decoded.TTLSeconds - 1},
		} {
			hash := envelope.ObjectHash(object)
			expected := envelope.Expected{
				ID:         message.id,
				Policy:     bad,
				KDF:        message.kdf,
				ObjectHash: &hash,
			}
			if _, _, openErr := envelope.Open(
				&message.content,
				&expected,
				object,
			); !errors.Is(
				openErr,
				envelope.ErrIntegrity,
			) {
				t.Fatalf("changed policy %+v: want ErrIntegrity, got %v", bad, openErr)
			}
		}
		_, metadata := harness.metadata(message)
		if metadata["state"] != "opened" || metadata["opens_remaining"].(float64) != 0 ||
			metadata["first_opened_at"] == nil {
			t.Fatalf("metadata after claim: %v", metadata)
		}
		for i := range 3 {
			got, again := harness.blob(message, lease)
			if got.StatusCode != http.StatusOK || string(again) != string(object) {
				t.Fatalf("blob %d: %d", i, got.StatusCode)
			}
		}
		if got, _ := harness.blob(message, lease); got.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("fourth download: %d", got.StatusCode)
		}
		if got, _ := harness.blob(message, core.Encode(make([]byte, 32))); got.StatusCode != http.StatusGone {
			t.Fatalf("bogus lease: %d", got.StatusCode)
		}
		if got := harness.ack(message, lease); got.StatusCode != http.StatusNoContent {
			t.Fatalf("ack: %d", got.StatusCode)
		}
		if harness.blobExists(message) {
			t.Fatal("blob survived the last ack")
		}
		if got, _ := harness.blob(message, lease); got.StatusCode != http.StatusGone {
			t.Fatalf("blob after ack: %d", got.StatusCode)
		}
		response, metadata = harness.metadata(message)
		if response.StatusCode != http.StatusOK || metadata["state"] != "opened" || metadata["ended_at"] == nil {
			t.Fatalf("tombstone metadata: %v", metadata)
		}
		if got, _ := harness.claim(message, "", ""); got.StatusCode != http.StatusGone {
			t.Fatalf("claim after end: %d", got.StatusCode)
		}
		response, status := harness.status(message)
		if response.StatusCode != http.StatusOK || status["state"] != "opened" {
			t.Fatalf("status: %v", status)
		}
		synctest.Sleep(store.LeaseDuration + store.LeaseTimerSlack)
		harness.server.flushCounts(t.Context())
		if strings.Contains(harness.logs.String(), "lease-timer") {
			t.Fatalf("the lease timer of an acknowledged message failed: %s", harness.logs.String())
		}
	})
}

func TestAK4ConcurrentClaimsHTTP(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		settings := harness.settings
		settings.Rates.ReadPerHour = 100000
		settings.Rates.CreatePerHour = 100000
		settings.Rates.CreatePerDay = 100000
		settings.Rates.BucketPerSecond = 100000
		settings.Rates.BucketBurst = 100000
		harness.reopen(&settings)
		for i := range 100 {
			message := newMessage(t, core.KDFNone, "", 1, 3600, "race")
			harness.mustCreate(message)
			var wait sync.WaitGroup
			codes := make([]int, 2)
			for j := range codes {
				wait.Go(func() {
					response, _, err := harness.tryClaim(message, "", "")
					if err != nil {
						t.Error(err)
					}
					codes[j] = response.StatusCode
				})
			}
			wait.Wait()
			if !slices.Equal(slices.Sorted(slices.Values(codes)), []int{200, 410}) {
				t.Fatalf("iteration %d: %v", i, codes)
			}
		}
	})
}

func TestAK5PassphrasePolicyHTTP(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		settings := harness.settings
		settings.Rates.ReadPerHour = 100000
		harness.reopen(&settings)
		message := newMessage(t, core.KDFScrypt, "correct", 2, 604800, "guarded")
		message.address = "203.0.113.10"
		harness.mustCreate(message)
		wrong := core.Encode(make([]byte, 32))
		expect := func(ip string, retry, left float64) {
			t.Helper()
			response, body := harness.claim(message, wrong, ip)
			fields := decodeJSON(t, body)
			if response.StatusCode != http.StatusForbidden || fields["error"] != "passphrase" ||
				fields["retry_after"].(float64) != retry ||
				fields["attempts_left"].(float64) != left {
				t.Fatalf("got %d %s, want retry %v left %v", response.StatusCode, body, retry, left)
			}
		}
		for i := 1; i <= 5; i++ {
			expect("203.0.113.10", 0, float64(5-i))
		}
		expect("203.0.113.10", 60, 0)
		for range 4 {
			synctest.Sleep(time.Minute)
			expect("203.0.113.10", 60, 0)
		}
		_, metadata := harness.metadata(message)
		if metadata["opens_remaining"].(float64) != 2 {
			t.Fatalf("wrong passphrase consumed an open: %v", metadata)
		}
		if response, _ := harness.claim(
			message,
			message.passphrase,
			"203.0.113.99",
		); response.StatusCode != http.StatusForbidden {
			t.Fatalf("the backoff belongs to the message, so another address waits too: %d", response.StatusCode)
		}
		synctest.Sleep(time.Minute)
		response, _ := harness.claim(message, message.passphrase, "203.0.113.99")
		if response.StatusCode != http.StatusOK {
			t.Fatalf("correct passphrase after the wait: %d", response.StatusCode)
		}
		_, status := harness.status(message)
		if status["failed_passphrase_attempts"].(float64) != 10 || status["opens_used"].(float64) != 1 {
			t.Fatalf("status: %v", status)
		}
		for i := range 90 {
			got, body := harness.claim(message, wrong, "198.51.100."+strconv.Itoa(i%250))
			if got.StatusCode != http.StatusForbidden {
				t.Fatalf("flood attempt %d: %d", i, got.StatusCode)
			}
			retryAfter := decodeJSON(t, body)["retry_after"].(float64)
			synctest.Sleep(time.Duration(retryAfter * float64(time.Second)))
		}
		response, body := harness.claim(message, message.passphrase, "192.0.2.77")
		fields := decodeJSON(t, body)
		if response.StatusCode != http.StatusForbidden || fields["retry_after"].(float64) <= 0 ||
			fields["attempts_left"].(float64) != 0 {
			t.Fatalf("100 failed attempts must lock the message for every address: %d %s", response.StatusCode, body)
		}
		synctest.Sleep(24 * time.Hour)
		if got, _ := harness.claim(
			message,
			message.passphrase,
			"192.0.2.78",
		); got.StatusCode != http.StatusForbidden {
			t.Fatalf("the lock must outlast the UTC day: %d", got.StatusCode)
		}
		_, status = harness.status(message)
		if status["failed_passphrase_attempts"].(float64) != 100 {
			t.Fatalf("status after cap: %v", status)
		}
		fresh := newMessage(t, core.KDFScrypt, "pw", 1, 3600, "x")
		harness.mustCreate(fresh)
		response, body = harness.claim(fresh, "", "192.0.2.78")
		fields = decodeJSON(t, body)
		if response.StatusCode != http.StatusForbidden || fields["attempts_left"].(float64) != 4 ||
			fields["retry_after"].(float64) != 0 {
			t.Fatalf("an empty passphrase: %d %s", response.StatusCode, body)
		}
		if _, statusNow := harness.status(fresh); statusNow["failed_passphrase_attempts"].(float64) != 1 {
			t.Fatalf("status after an empty passphrase: %v", statusNow)
		}
	})
}

func TestAK6LeaseExpiryWithoutAck(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		settings := harness.settings
		settings.Rates.ReadPerHour = 100000
		harness.reopen(&settings)
		message := newMessage(t, core.KDFNone, "", 3, 86400, "three")
		harness.mustCreate(message)
		var leases []string
		var wait sync.WaitGroup
		var mutex sync.Mutex
		for i := range 3 {
			wait.Go(func() {
				response, _, err := harness.tryClaim(message, "", fmt.Sprintf("198.51.100.%d", 20+i))
				if err != nil {
					t.Error(err)
					return
				}
				if response.StatusCode != http.StatusOK {
					t.Errorf("claim: %d", response.StatusCode)
					return
				}
				mutex.Lock()
				leases = append(leases, response.Header.Get("X-Nylzo-Lease"))
				mutex.Unlock()
			})
		}
		wait.Wait()
		distinct := map[string]bool{}
		for _, lease := range leases {
			distinct[lease] = true
		}
		if len(distinct) != 3 {
			t.Fatalf("leases not distinct: %v", leases)
		}
		stored := harness.mustRecord(message).Message.Leases
		first := slices.MinFunc(stored, func(a, b store.Lease) int { return a.ExpiresAt.Compare(b.ExpiresAt) })
		last := slices.MaxFunc(stored, func(a, b store.Lease) int { return a.ExpiresAt.Compare(b.ExpiresAt) })
		synctest.Sleep(time.Until(first.ExpiresAt) - time.Second)
		for _, lease := range leases {
			if response, _ := harness.blob(message, lease); response.StatusCode != http.StatusOK {
				t.Fatalf("blob within lease: %d", response.StatusCode)
			}
		}
		synctest.Sleep(time.Until(last.ExpiresAt))
		if !harness.blobExists(message) {
			t.Fatal("blob deleted before any request, timer or sweep")
		}
		synctest.Sleep(store.LeaseTimerSlack)
		if harness.blobExists(message) {
			t.Fatal("timer did not delete the blob")
		}
		for _, lease := range leases {
			if response, _ := harness.blob(message, lease); response.StatusCode != http.StatusGone {
				t.Fatalf("blob after lease expiry: %d", response.StatusCode)
			}
		}
		_, metadata := harness.metadata(message)
		if metadata["state"] != "opened" {
			t.Fatalf("metadata: %v", metadata)
		}
	})
}

func TestAK6RequestAfterLeaseExpiryReleasesBlob(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFNone, "", 1, 86400, "one")
		harness.mustCreate(message)
		response, _ := harness.claim(message, "", "")
		if response.StatusCode != http.StatusOK {
			t.Fatal("claim")
		}
		lease := response.Header.Get("X-Nylzo-Lease")
		synctest.Sleep(store.LeaseDuration)
		if got, _ := harness.blob(message, lease); got.StatusCode != http.StatusGone {
			t.Fatalf("blob at expiry: %d", got.StatusCode)
		}
		if harness.blobExists(message) {
			t.Fatal("request after the last lease expired must release the blob")
		}
	})
}

func TestAK6SweepAfterRestart(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFNone, "", 1, 86400, "restart")
		harness.mustCreate(message)
		if response, _ := harness.claim(message, "", ""); response.StatusCode != http.StatusOK {
			t.Fatal("claim")
		}
		harness.reopen(&harness.settings)
		synctest.Sleep(store.LeaseDuration + time.Second)
		if !harness.blobExists(message) {
			t.Fatal("blob gone before sweep")
		}
		synctest.Sleep(time.Until(store.CeilMinute(time.Now()).Truncate(store.BlobPurgeStep).Add(store.BlobPurgeStep)))
		report, err := harness.server.sweepOnce(t.Context())
		if err != nil || report.Blobs != 1 {
			t.Fatalf("sweep: %+v %v", report, err)
		}
		if harness.blobExists(message) {
			t.Fatal("sweep left the blob")
		}
	})
}

func TestAK7ExpiryLazyAndSweep(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFNone, "", 1, 60, "short")
		harness.mustCreate(message)
		synctest.Sleep(61 * time.Second)
		response, metadata := harness.metadata(message)
		if response.StatusCode != http.StatusOK || metadata["state"] != "expired" {
			t.Fatalf("lazy expiry: %d %v", response.StatusCode, metadata)
		}
		if harness.blobExists(message) {
			t.Fatal("expired blob kept")
		}
		if got, _ := harness.claim(message, "", ""); got.StatusCode != http.StatusGone {
			t.Fatalf("claim expired: %d", got.StatusCode)
		}
		secondMessage := newMessage(t, core.KDFNone, "", 1, 60, "swept")
		harness.mustCreate(secondMessage)
		synctest.Sleep(store.BlobPurgeStep)
		report, err := harness.server.sweepOnce(t.Context())
		if err != nil || report.Blobs != 1 || harness.blobExists(secondMessage) {
			t.Fatalf("sweep: %+v %v", report, err)
		}
		synctest.Sleep(store.TombstoneTTL + store.RowPurgeStep)
		if _, sweepOnceErr := harness.server.sweepOnce(t.Context()); sweepOnceErr != nil {
			t.Fatal(sweepOnceErr)
		}
		if got, _ := harness.metadata(message); got.StatusCode != http.StatusNotFound {
			t.Fatalf("after the row purge: %d", got.StatusCode)
		}
		if got, body := createWithToken(
			t,
			harness,
			message,
			nil,
		); got.StatusCode != http.StatusForbidden ||
			decodeJSON(t, body)["error"] != "id_token" {
			t.Fatalf(
				"an ended id must not be registered again without a token the server issued: %d %s",
				got.StatusCode,
				body,
			)
		}
	})
}

func TestAckRefusesALeaseThatIsNotActive(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFNone, "", 1, 3600, "ack")
		harness.mustCreate(message)
		response, _ := harness.claim(message, "", "")
		lease := response.Header.Get("X-Nylzo-Lease")
		if response.StatusCode != http.StatusOK || lease == "" {
			t.Fatalf("claim: %d", response.StatusCode)
		}
		if code := harness.ack(message, core.Encode(core.NewSecret()[:])).StatusCode; code != http.StatusGone {
			t.Fatalf("an ack with a lease the server never issued: %d", code)
		}
		if !harness.blobExists(message) {
			t.Fatal("an ack with a foreign lease deleted the blob")
		}
		synctest.Sleep(store.LeaseDuration + time.Minute)
		response, body := harness.do(
			&testRequest{
				method:  http.MethodPost,
				path:    "/api/v1/messages/" + message.id.String() + "/ack",
				address: message.address,
				token:   message.readToken,
				headers: map[string]string{"X-Nylzo-Lease": lease},
			},
		)
		if response.StatusCode != http.StatusGone || decodeJSON(t, body)["state"] != "opened" {
			t.Fatalf("an ack after the lease ended: %d %s", response.StatusCode, body)
		}
	})
}

func TestClaimBodyVariants(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFScrypt, "pw", 1, 3600, "x")
		harness.mustCreate(message)
		path := "/api/v1/messages/" + message.id.String() + "/claim"
		for name, request := range map[string]*testRequest{
			"malformed-json": {method: http.MethodPost, path: path, body: []byte(`{"verifier":`), token: message.readToken},
			"short-ke3": {
				method:  http.MethodPost,
				path:    path,
				token:   message.readToken,
				headers: map[string]string{api.KE3HeaderName: core.Encode(make([]byte, 63))},
			},
		} {
			response, _ := harness.do(request)
			if response.StatusCode != http.StatusBadRequest {
				t.Fatalf("%s: %d", name, response.StatusCode)
			}
		}
		if _, status := harness.status(
			message,
		); status["failed_passphrase_attempts"].(float64) != 0 ||
			status["opens_used"].(float64) != 0 {
			t.Fatalf("malformed claim bodies changed the message: %v", status)
		}
		unfinished, sender := io.Pipe()
		defer sender.Close()
		r, _ := http.NewRequest(http.MethodPost, harness.httpServer.URL+path, unfinished)
		r.Header.Set(
			"Authorization",
			signing.SignRequest(&message.link.R, signing.PurposeRead, "POST", path, harness.challenge(), nil, signing.Components{}).
				Authorization(),
		)
		r.Header.Set("X-Forwarded-For", "203.0.113.4")
		r.Header.Set(api.ShareKeyHeaderName, testShareKeyHeader)
		response, err := harness.client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest || r.ContentLength != 0 {
			t.Fatalf("claim with a chunked body: %d (content length %d)", response.StatusCode, r.ContentLength)
		}
		if _, status := harness.status(message); status["opens_used"].(float64) != 0 {
			t.Fatalf("a chunked claim body spent an open: %v", status)
		}
	})
}

func TestDownloadAfterExpiryWithAStuckDeleteIsGone(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		faulty := harness.withFaultyBlobs()
		message := newMessage(t, core.KDFNone, "", 2, 60, "expires under an active lease")
		harness.mustCreate(message)
		response, _ := harness.claim(message, "", "")
		lease := response.Header.Get("X-Nylzo-Lease")
		if response.StatusCode != http.StatusOK || lease == "" {
			t.Fatalf("claim: %d", response.StatusCode)
		}
		faulty.FailDelete(harness.blobReference(message), errInjected)
		synctest.Sleep(61 * time.Second)
		if got, body := harness.blob(
			message,
			lease,
		); got.StatusCode != http.StatusGone ||
			decodeJSON(t, body)["state"] != "expired" {
			t.Fatalf("a download after the expiry while the blob cannot be deleted: %d %s", got.StatusCode, body)
		}
		if code := harness.ack(message, lease).StatusCode; code != http.StatusGone {
			t.Fatalf("an ack after the expiry while the blob cannot be deleted: %d", code)
		}
	})
}

func TestFirstOpenedAtKeepsTheFirstClaim(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFNone, "", 2, 3600, "opened twice")
		harness.mustCreate(message)
		if response, _ := harness.claim(message, "", ""); response.StatusCode != http.StatusOK {
			t.Fatalf("first claim: %d", response.StatusCode)
		}
		_, first := harness.status(message)
		synctest.Sleep(2 * time.Minute)
		response, _ := harness.claim(message, "", "")
		if response.StatusCode != http.StatusOK {
			t.Fatalf("second claim: %d", response.StatusCode)
		}
		_, second := harness.status(message)
		if first["first_opened_at"] == nil || second["first_opened_at"] != first["first_opened_at"] {
			t.Fatalf("first_opened_at moved from %v to %v", first["first_opened_at"], second["first_opened_at"])
		}
		if code := harness.ack(message, response.Header.Get("X-Nylzo-Lease")).StatusCode; code != http.StatusNoContent {
			t.Fatalf("ack: %d", code)
		}
		if _, ended := harness.status(message); ended["first_opened_at"] != first["first_opened_at"] {
			t.Fatalf(
				"the tombstone moved first_opened_at from %v to %v",
				first["first_opened_at"],
				ended["first_opened_at"],
			)
		}
	})
}

func TestBlobCarriesThePolicyOfTheClaim(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFNone, "", 1, 3600, "resume")
		harness.mustCreate(message)
		response, _ := harness.claim(message, "", "")
		policy := response.Header.Get("X-Nylzo-Policy")
		if response.StatusCode != http.StatusOK || policy == "" {
			t.Fatalf("claim: %d policy=%q", response.StatusCode, policy)
		}
		again, object := harness.blob(message, response.Header.Get("X-Nylzo-Lease"))
		if again.StatusCode != http.StatusOK || again.Header.Get("X-Nylzo-Policy") != policy {
			t.Fatalf("blob: %d policy=%q, want %q", again.StatusCode, again.Header.Get("X-Nylzo-Policy"), policy)
		}
		if _, err := harness.openObject(message, object, again.Header.Get("X-Nylzo-Policy")); err != nil {
			t.Fatalf("the blob does not open with its own policy: %v", err)
		}
	})
}

func TestAckRefusesAnExpiredLeaseWhileOpensRemain(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFNone, "", 2, 3600, "ack")
		harness.mustCreate(message)
		response, _ := harness.claim(message, "", "")
		lease := response.Header.Get("X-Nylzo-Lease")
		if response.StatusCode != http.StatusOK || lease == "" {
			t.Fatalf("claim: %d", response.StatusCode)
		}
		synctest.Sleep(store.LeaseDuration + time.Minute)
		if code := harness.ack(message, lease).StatusCode; code != http.StatusGone {
			t.Fatalf("an ack after the lease ended: %d", code)
		}
		if got, _ := harness.blob(message, lease); got.StatusCode != http.StatusGone {
			t.Fatalf("a download after the lease ended: %d", got.StatusCode)
		}
	})
}

type lifecycleKind struct {
	name   string
	opens  uint16
	ttl    uint32
	ending store.State
	after  time.Duration
	expect [11]string
}

var lifecycleActions = [11]string{
	"burn",
	"cancel",
	"expiry by request",
	"expiry by sweep",
	"release",
	"resume by request",
	"resume by sweep",
	"claim",
	"lease download",
	"blob vanished",
	"ack",
}

func recordedRow(state store.State) [11]string {
	stateName := string(state)
	return [11]string{
		"410 " + stateName + " tomb:" + stateName,
		"410 " + stateName + " tomb:" + stateName,
		"200 " + stateName + " tomb:" + stateName,
		"- - tomb:" + stateName,
		"- - tomb:" + stateName,
		"200 " + stateName + " tomb:" + stateName,
		"- - tomb:" + stateName,
		"410 " + stateName + " tomb:" + stateName,
		"410 " + stateName + " tomb:" + stateName,
		"410 " + stateName + " tomb:" + stateName,
		"410 " + stateName + " tomb:" + stateName,
	}
}

// expiredRow and openedRow wait for the sweep until blob_purge_at, which is rounded up to the quarter hour.
func expiredRow(state store.State) [11]string {
	row := recordedRow(state)
	row[6] = "- - live:"
	return row
}

func openedRow() [11]string {
	return expiredRow(store.StateOpened)
}

var lifecycleKinds = []lifecycleKind{
	{name: "unspent", opens: 2, ttl: 3600, expect: [11]string{
		"204 - tomb:deleted", "204 - tomb:cancelled", "200 expired tomb:expired", "- - tomb:expired", "- - live:",
		"200 available live:", "- - live:", "200 - live:", "200 - live:", "500 - live:", "204 - live:",
	}},
	{name: "spent during the lease", opens: 1, ttl: 3600, expect: [11]string{
		"204 - tomb:deleted", "204 - tomb:cancelled", "200 opened tomb:opened", "- - tomb:opened", "- - live:",
		"200 opened live:", "- - live:", "410 opened live:", "200 - live:", "500 - live:", "204 - tomb:opened",
	}},
	{
		name:   "spent after the lease",
		opens:  1,
		ttl:    3600,
		after:  store.LeaseDuration + store.LeaseTimerSlack,
		expect: openedRow(),
	},
	{name: "unspent and expired", opens: 2, ttl: 60, after: 61 * time.Second, expect: expiredRow(store.StateExpired)},
	{name: "spent and expired", opens: 1, ttl: 60, after: 61 * time.Second, expect: expiredRow(store.StateOpened)},
	{
		name:   "recorded deleted",
		opens:  2,
		ttl:    3600,
		ending: store.StateDeleted,
		expect: recordedRow(store.StateDeleted),
	},
	{
		name:   "recorded cancelled",
		opens:  2,
		ttl:    3600,
		ending: store.StateCancelled,
		expect: recordedRow(store.StateCancelled),
	},
}

func TestLifecycleMatrix(t *testing.T) {
	t.Parallel()
	for _, kind := range lifecycleKinds {
		for i, action := range lifecycleActions {
			t.Run(kind.name+"/"+action, func(t *testing.T) {
				t.Parallel()
				synctest.Test(t, func(t *testing.T) {
					harness := newHarness(t, baseSettings(pepperA, nil))
					setup := setupLifecycleKind(t, harness, &kind)
					response, body := runLifecycleAction(t, harness, setup.message, setup.lease, action)
					checkLifecycleCell(t, harness, setup.message, action, kind.expect[i], response, body)
				})
			})
		}
	}
}

type lifecycleSetup struct {
	message *testMessage
	lease   string
}

func setupLifecycleKind(t *testing.T, harness *testHarness, kind *lifecycleKind) lifecycleSetup {
	t.Helper()
	message := newMessage(t, core.KDFNone, "", kind.opens, kind.ttl, "x")
	harness.mustCreate(message)
	response, _ := harness.claim(message, "", "")
	lease := response.Header.Get("X-Nylzo-Lease")
	if response.StatusCode != http.StatusOK || lease == "" {
		t.Fatalf("claim: %d", response.StatusCode)
	}
	harness.reopen(&harness.settings)
	synctest.Sleep(kind.after)
	if kind.ending != "" {
		harness.markEnding(message, kind.ending)
	}
	return lifecycleSetup{message: message, lease: lease}
}

func runLifecycleAction(
	t *testing.T,
	harness *testHarness,
	message *testMessage,
	lease, action string,
) (testResponse, responseBody) {
	t.Helper()
	ctx := t.Context()
	switch action {
	case "burn":
		return harness.post(message, "burn", message.readToken)
	case "cancel":
		return harness.post(message, "cancel", message.manageToken)
	case "expiry by request":
		synctest.Sleep(2 * time.Hour)
		return harness.do(
			&testRequest{
				method:  http.MethodGet,
				path:    "/api/v1/messages/" + message.id.String(),
				address: message.address,
				token:   message.readToken,
			},
		)
	case "expiry by sweep":
		synctest.Sleep(2 * time.Hour)
		if _, err := harness.server.sweepOnce(ctx); err != nil {
			t.Fatal(err)
		}
	case "release":
		if _, err := harness.finish(message, time.Now()); err != nil {
			t.Fatal(err)
		}
	case "resume by request":
		return harness.do(
			&testRequest{
				method:  http.MethodGet,
				path:    "/api/v1/messages/" + message.id.String() + "/status",
				address: message.address,
				token:   message.manageToken,
			},
		)
	case "resume by sweep":
		if _, err := harness.server.sweepOnce(ctx); err != nil {
			t.Fatal(err)
		}
	case "claim":
		return harness.claim(message, "", "")
	case "lease download":
		return harness.blob(message, lease)
	case "blob vanished":
		if err := harness.blobs.Delete(ctx, harness.blobReference(message)); err != nil {
			t.Fatal(err)
		}
		return harness.claim(message, "", "")
	case "ack":
		return harness.do(
			&testRequest{
				method:  http.MethodPost,
				path:    "/api/v1/messages/" + message.id.String() + "/ack",
				address: message.address,
				token:   message.readToken,
				headers: map[string]string{"X-Nylzo-Lease": lease},
			},
		)
	default:
		t.Fatalf("unknown action %q", action)
	}
	return testResponse{}, nil
}

func checkLifecycleCell(
	t *testing.T,
	harness *testHarness,
	message *testMessage,
	action, expect string,
	response testResponse,
	body []byte,
) {
	t.Helper()
	parts := strings.Fields(expect)
	code, state, final := parts[0], parts[1], parts[2]
	if code != "-" && response.StatusCode == 0 {
		t.Fatalf("no response, want %s", code)
	}
	if code != "-" && strconv.Itoa(response.StatusCode) != code {
		t.Fatalf("response %d, want %s (%s)", response.StatusCode, code, body)
	}
	if state != "-" && decodeJSON(t, body)["state"] != state {
		t.Fatalf("state %s, want %s", body, state)
	}
	if code == "410" || code == "500" {
		if lease := response.Header.Get("X-Nylzo-Lease"); lease != "" && action == "claim" {
			t.Fatal("a refused claim handed out a lease")
		}
	}
	record, err := harness.record(message)
	if err != nil {
		t.Fatal(err)
	}
	kind, want, _ := strings.Cut(final, ":")
	switch kind {
	case "tomb":
		if record.HasBlob() || string(record.State(time.Now())) != want {
			t.Fatalf("record %+v, want an ended %s without its blob", record.Message, want)
		}
		if harness.blobExists(message) {
			t.Fatal("an ended message kept its blob")
		}
	case "live":
		if !record.HasBlob() || string(record.Message.Ending) != want {
			t.Fatalf("record %+v, want a blob and the ending %q", record.Message, want)
		}
		if exists := harness.blobExists(message); exists != (action != "blob vanished") {
			t.Fatalf("blob present %v on a live row", exists)
		}
	default:
		t.Fatalf("unknown final state %q", final)
	}
}

func TestVanishedBlobDuringLeaseIsAStorageFault(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFNone, "", 1, 3600, "x")
		harness.mustCreate(message)
		response, _ := harness.claim(message, "", "")
		lease := response.Header.Get("X-Nylzo-Lease")
		if err := harness.blobs.Delete(t.Context(), harness.blobReference(message)); err != nil {
			t.Fatal(err)
		}
		if got, _ := harness.blob(message, lease); got.StatusCode != http.StatusInternalServerError {
			t.Fatalf("download of a vanished blob: %d", got.StatusCode)
		}
		if record, err := harness.record(message); err != nil || !record.HasBlob() || record.Message.Ending != "" {
			t.Fatalf("the row must stay live without an ending: %+v %v", record.Message, err)
		}
	})
}

func TestExpiryRacingADownloadAnswersGone(t *testing.T) {
	t.Parallel()
	for name, finishes := range map[string]bool{"the expiry finishes": true, "the expiry cannot finish": false} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				harness := newHarness(t, baseSettings(pepperA, nil))
				faulty := harness.withFaultyBlobs()
				message := newMessage(t, core.KDFNone, "", 2, 60, "x")
				harness.mustCreate(message)
				response, _ := harness.claim(message, "", "")
				lease := response.Header.Get("X-Nylzo-Lease")
				if !finishes {
					harness.store = failingForgetStore{Store: harness.store}
					harness.reopen(&harness.settings)
				}
				var once sync.Once
				faulty.OnOpen(func(reference string) {
					once.Do(func() {
						time.Sleep(61 * time.Second)
						if err := faulty.Blobs.Delete(t.Context(), reference); err != nil {
							t.Error(err)
						}
					})
				})
				response, body := harness.blob(message, lease)
				if response.StatusCode != http.StatusGone || decodeJSON(t, body)["state"] != "expired" {
					t.Fatalf("a download racing the expiry ending: %d %s", response.StatusCode, body)
				}
				if state, gone := harness.endedState(message); finishes && (!gone || state != store.StateExpired) {
					t.Fatalf("the download must finish the expiry: %s %v", state, gone)
				}
			})
		})
	}
}

func TestClaimReadsItsBodyBeforeOpeningTheBlob(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		faulty := harness.withFaultyBlobs()
		message := newMessage(t, core.KDFNone, "", 1, 3600, "x")
		harness.mustCreate(message)
		var opens atomic.Int32
		faulty.OnOpen(func(string) { opens.Add(1) })
		response, _ := harness.do(
			&testRequest{
				method:  http.MethodPost,
				path:    "/api/v1/messages/" + message.id.String() + "/claim",
				address: message.address,
				body:    []byte("{"),
				token:   message.readToken,
				headers: map[string]string{"Content-Type": "application/json"},
			},
		)
		if response.StatusCode != http.StatusBadRequest || opens.Load() != 0 {
			t.Fatalf(
				"a malformed claim body must be refused before the blob is opened: %d, %d opens",
				response.StatusCode,
				opens.Load(),
			)
		}
	})
}

func TestTruncatedBlobSpendsNoOpen(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFNone, "", 1, 3600, "x")
		harness.mustCreate(message)
		reference := harness.blobReference(message)
		if err := harness.blobs.Delete(t.Context(), reference); err != nil {
			t.Fatal(err)
		}
		short := message.object[:len(message.object)-1]
		if _, err := harness.blobs.PutExpiring(
			t.Context(),
			reference,
			bytes.NewReader(short),
			int64(len(short)),
			time.Now().Add(time.Hour),
		); err != nil {
			t.Fatal(err)
		}
		response, _ := harness.claim(message, "", "")
		if response.StatusCode != http.StatusInternalServerError || response.Header.Get("X-Nylzo-Lease") != "" {
			t.Fatalf("a blob of the wrong size must answer 500 before an open is spent: %d", response.StatusCode)
		}
		if _, metadata := harness.metadata(message); metadata["opens_remaining"] != float64(1) {
			t.Fatalf("a truncated blob spent an open: %v", metadata)
		}
	})
}

// F14: the server checks the tag of the stored blob before a claim or a download spends anything.
func TestChangedBlobSpendsNoOpenOrDownload(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		faulty := harness.withFaultyBlobs()
		message := newMessage(t, core.KDFNone, "", 2, 3600, "x")
		harness.mustCreate(message)
		path := harness.blobFile(message)
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		writeBlob := func(content []byte) {
			if writeErr := os.WriteFile(path, content, 0o600); writeErr != nil {
				t.Fatal(writeErr)
			}
		}
		changeByte := func(offset int) func() {
			return func() {
				changed := bytes.Clone(original)
				changed[offset] ^= 1
				writeBlob(changed)
			}
		}
		restore := func() {
			faulty.FailRead(nil)
			writeBlob(original)
		}
		for name, change := range map[string]func(){
			"a bit of the object": changeByte(erase.BlockLen + len(message.object)/2),
			"a bit of the salt":   changeByte(0),
			"a zeroed salt":       func() { writeBlob(append(make([]byte, seal.EraseSaltLen), original[seal.EraseSaltLen:]...)) },
			"a byte more":         func() { writeBlob(append(bytes.Clone(original), 0)) },
			"a read fault":        func() { faulty.FailRead(errInjected) },
		} {
			change()
			response, _ := harness.claim(message, "", "")
			restore()
			if response.StatusCode != http.StatusInternalServerError || response.Header.Get("X-Nylzo-Lease") != "" {
				t.Fatalf("%s: a claim must answer 500 before it spends an open: %d", name, response.StatusCode)
			}
			if _, metadata := harness.metadata(message); metadata["opens_remaining"] != float64(2) {
				t.Fatalf("%s spent an open: %v", name, metadata)
			}
		}
		harness.server.flushCounts(t.Context())
		if !strings.Contains(harness.logs.String(), `"msg":"blob-changed","count":4`) {
			t.Fatalf("each changed blob must be logged as blob-changed: %s", harness.logs.String())
		}
		claimed, _ := harness.claim(message, "", "")
		lease := claimed.Header.Get("X-Nylzo-Lease")
		if claimed.StatusCode != http.StatusOK || lease == "" {
			t.Fatalf("claim of the restored blob: %d", claimed.StatusCode)
		}
		changeByte(len(original) - 1)()
		refused, _ := harness.blob(message, lease)
		restore()
		if refused.StatusCode != http.StatusInternalServerError {
			t.Fatalf("a download of a changed blob: %d", refused.StatusCode)
		}
		for i := range store.MaxLeaseDownload {
			if response, object := harness.blob(message, lease); response.StatusCode != http.StatusOK ||
				!bytes.Equal(object, message.object) {
				t.Fatalf("download %d after a refused one: %d", i+1, response.StatusCode)
			}
		}
		faulty.FailRead(errInjected)
		limited, _ := harness.blob(message, lease)
		faulty.FailRead(nil)
		if limited.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("a lease with no download left must answer 429 before it reads the blob: %d", limited.StatusCode)
		}
	})
}

// F14: a download is judged at the time after the blob check, so a lease that ends during the check counts nothing.
func TestALeaseThatEndsDuringTheCheckDownloadsNothing(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		faulty := harness.withFaultyBlobs()
		message := newMessage(t, core.KDFNone, "", 2, 3600, "x")
		harness.mustCreate(message)
		claimed, _ := harness.claim(message, "", "")
		lease := claimed.Header.Get("X-Nylzo-Lease")
		leaseEnd := harness.mustRecord(message).Message.Leases[0].ExpiresAt
		faulty.OnOpen(func(string) { time.Sleep(time.Until(leaseEnd) + time.Second) })
		response, body := harness.blob(message, lease)
		faulty.OnOpen(nil)
		if response.StatusCode != http.StatusGone {
			t.Fatalf("a lease that ended during the check must answer 410: %d", response.StatusCode)
		}
		if state := decodeJSON(t, body)["state"]; state != "available" {
			t.Fatalf("a lease that ended during the check answered state %v", state)
		}
		if downloads := harness.mustRecord(message).Message.Leases[0].Downloads; downloads != 0 {
			t.Fatalf("a lease that ended during the check counted %d downloads", downloads)
		}
	})
}

// F14: a download whose check outlasts the message is refused, although its lease is still active.
func TestADownloadThatWaitsPastExpiryIsRefused(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		faulty := harness.withFaultyBlobs()
		message := newMessage(t, core.KDFNone, "", 2, 60, "x")
		harness.mustCreate(message)
		claimed, _ := harness.claim(message, "", "")
		lease := claimed.Header.Get("X-Nylzo-Lease")
		record := harness.mustRecord(message)
		expiry := record.Message.ExpiresAt
		if !record.Message.Leases[0].ExpiresAt.After(expiry.Add(time.Second)) {
			t.Fatal("the lease must outlive the message for this test")
		}
		faulty.OnOpen(func(string) { time.Sleep(time.Until(expiry) + time.Second) })
		response, body := harness.blob(message, lease)
		faulty.OnOpen(nil)
		if response.StatusCode != http.StatusGone {
			t.Fatalf("a download that reached the store after expires_at must be refused: %d", response.StatusCode)
		}
		if state := decodeJSON(t, body)["state"]; state != "expired" {
			t.Fatalf("a download past expiry answered state %v", state)
		}
	})
}

// F14: a check that fails because an ending changed the blob meanwhile answers the ending, not blob-changed.
func TestACheckRacingAnEndingAnswersTheEnding(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFNone, "", 1, 3600, "x")
		harness.mustCreate(message)
		path := harness.blobFile(message)
		inner := harness.store
		harness.store = &racingStore{Store: inner, race: func() {
			record := harness.recordIn(inner, message)
			if _, err := store.MarkEnding(t.Context(), inner, &record, store.StateDeleted, time.Now()); err != nil {
				t.Error(err)
			}
			content, err := os.ReadFile(path)
			if err != nil {
				t.Error(err)
				return
			}
			content[len(content)-1] ^= 1
			if err = os.WriteFile(path, content, 0o600); err != nil {
				t.Error(err)
			}
		}}
		harness.reopen(&harness.settings)
		response, body := harness.claim(message, "", "")
		if response.StatusCode != http.StatusGone || decodeJSON(t, body)["state"] != "deleted" {
			t.Fatalf("a check that raced an ending must answer the ending: %d %s", response.StatusCode, body)
		}
		harness.server.flushCounts(t.Context())
		if strings.Contains(harness.logs.String(), `"msg":"blob-changed"`) {
			t.Fatalf("an ending was counted as a changed blob: %s", harness.logs.String())
		}
	})
}

// F14: a claim that the passphrase backoff refuses anyway reads no blob.
func TestALockedOutClaimReadsNoBlob(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		faulty := harness.withFaultyBlobs()
		message := newMessage(t, core.KDFScrypt, "correct", 1, 3600, "x")
		harness.mustCreate(message)
		wrong := core.Encode(make([]byte, core.SeedLen))
		for range store.FreeAttempts + 1 {
			if response, _ := harness.claim(message, wrong, ""); response.StatusCode != http.StatusForbidden {
				t.Fatalf("a wrong passphrase: %d", response.StatusCode)
			}
		}
		faulty.FailOpen(errInjected)
		response, body := harness.claim(message, wrong, "")
		faulty.FailOpen(nil)
		if response.StatusCode != http.StatusForbidden || decodeJSON(t, body)["retry_after"].(float64) <= 0 {
			t.Fatalf("a claim in the backoff must be refused before it opens the blob: %d %s",
				response.StatusCode, body)
		}
		_, status := harness.status(message)
		if status["failed_passphrase_attempts"].(float64) != store.FreeAttempts+1 {
			t.Fatalf("a claim refused in the backoff counted an attempt: %v", status)
		}
	})
}

// F14: a blob wrap that a release before Nylzo 0.14.0 stored has no tag, and its message keeps opening until it ends.
func TestABlobWithoutTagStillOpens(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFNone, "", 1, 3600, "stored by 0.13")
		harness.mustCreate(message)
		if record := harness.mustRecord(message); len(record.Row.BlobWrap) != seal.BlobWrapLen {
			t.Fatalf("a new blob wrap has %d bytes", len(record.Row.BlobWrap))
		}
		harness.dropBlobTag(message)
		response, object := harness.claim(message, "", "")
		if response.StatusCode != http.StatusOK || !bytes.Equal(object, message.object) {
			t.Fatalf("a message of an earlier release must still open: %d", response.StatusCode)
		}
		if downloaded, again := harness.blob(message, response.Header.Get("X-Nylzo-Lease")); downloaded.StatusCode !=
			http.StatusOK || !bytes.Equal(again, message.object) {
			t.Fatalf("its lease must still download it: %d", downloaded.StatusCode)
		}
	})
}

func TestHeadOnBlobSpendsNoDownload(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFNone, "", 2, 3600, "x")
		harness.mustCreate(message)
		response, _ := harness.claim(message, "", "")
		lease := response.Header.Get("X-Nylzo-Lease")
		head, _ := harness.do(
			&testRequest{
				method:  http.MethodHead,
				path:    "/api/v1/messages/" + message.id.String() + "/blob",
				address: message.address,
				token:   message.readToken,
				headers: map[string]string{"X-Nylzo-Lease": lease},
			},
		)
		if head.StatusCode != http.StatusNotFound {
			t.Fatalf("HEAD on /blob: %d", head.StatusCode)
		}
		for i := range store.MaxLeaseDownload {
			if got, _ := harness.blob(message, lease); got.StatusCode != http.StatusOK {
				t.Fatalf("download %d after a HEAD: %d", i+1, got.StatusCode)
			}
		}
	})
}

// burnAfterClaim burns the message right after the first write, which is the claim transaction.
// burnAfterClaim burns the message after the update that spends the open, the third of a handshake and its claim.
type burnAfterClaim struct {
	store.Store

	once    sync.Once
	updates atomic.Int32
	burn    func()
}

func (s *burnAfterClaim) Update(ctx context.Context, locator seal.Locator, change func(*store.Row) error) error {
	err := s.Store.Update(ctx, locator, change)
	if err == nil && s.updates.Add(1) == 3 {
		s.once.Do(s.burn)
	}
	return err
}

func TestClaimStreamsTheBlobItOpenedWhenABurnFollows(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFNone, "", 2, 3600, "x")
		harness.mustCreate(message)
		inner := harness.store
		harness.store = &burnAfterClaim{Store: inner, burn: func() {
			record := harness.recordIn(inner, message)
			if _, err := store.End(
				t.Context(),
				inner,
				harness.blobs,
				&record,
				store.StateDeleted,
				time.Now(),
			); err != nil {
				t.Error(err)
			}
		}}
		harness.reopen(&harness.settings)
		response, object := harness.claim(message, "", "")
		if response.StatusCode != http.StatusOK || response.Header.Get("X-Nylzo-Lease") == "" ||
			!bytes.Equal(object, message.object) {
			t.Fatalf(
				"a claim that spent an open must stream the blob it opened: %d %d bytes",
				response.StatusCode,
				len(object),
			)
		}
	})
}

func TestReleaseAfterLastLeaseKeepsUnspentMessage(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFNone, "", 2, 3600, "x")
		harness.mustCreate(message)
		response, _ := harness.claim(message, "", "")
		if ack := harness.ack(message, response.Header.Get("X-Nylzo-Lease")); ack.StatusCode != http.StatusNoContent {
			t.Fatalf("ack: %d", ack.StatusCode)
		}
		synctest.Sleep(store.LeaseDuration + store.LeaseTimerSlack)
		if released, err := harness.finish(message, time.Now()); err != nil || released {
			t.Fatalf("release: %v %v", released, err)
		}
		record, err := harness.record(message)
		if err != nil || !record.HasBlob() || record.Message.Ending != "" || record.Message.OpensRemaining != 1 ||
			!harness.blobExists(message) {
			t.Fatalf("row %+v %v", record.Message, err)
		}
	})
}

func TestAckAfterARecordedBurnFinishesIt(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFNone, "", 2, 3600, "x")
		harness.mustCreate(message)
		response, _ := harness.claim(message, "", "")
		lease := response.Header.Get("X-Nylzo-Lease")
		harness.endOnFirstLoad(message, store.StateDeleted, false)
		response, body := runLifecycleAction(t, harness, message, lease, "ack")
		if response.StatusCode != http.StatusGone || decodeJSON(t, body)["state"] != "deleted" {
			t.Fatalf("an ack after a recorded burn: %d %s", response.StatusCode, body)
		}
		if record, err := harness.record(message); err != nil || record.HasBlob() ||
			record.State(time.Now()) != store.StateDeleted || harness.blobExists(message) {
			t.Fatalf("the ack must finish the recorded burn: %+v %v", record.Message, err)
		}
	})
}

type slowWriterStore struct {
	store.Store

	wait time.Duration
}

func (s slowWriterStore) Update(ctx context.Context, locator seal.Locator, change func(*store.Row) error) error {
	time.Sleep(s.wait)
	return s.Store.Update(ctx, locator, change)
}

func TestClaimThatWaitsPastExpiryIsRefused(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFNone, "", 1, 60, "x")
		harness.mustCreate(message)
		synctest.Sleep(59 * time.Second)
		harness.store = slowWriterStore{Store: harness.store, wait: 2 * time.Second}
		harness.reopen(&harness.settings)
		response, body := harness.claim(message, "", "")
		if response.StatusCode != http.StatusGone || decodeJSON(t, body)["state"] != "expired" ||
			response.Header.Get("X-Nylzo-Lease") != "" {
			t.Fatalf(
				"a claim that reached the store after expires_at must be refused: %d %s",
				response.StatusCode,
				response.Header.Get("Content-Type"),
			)
		}
	})
}

func TestBlobLossWithoutEndingIsAStorageFault(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		for _, opens := range []uint16{1, 2} {
			message := newMessage(t, core.KDFNone, "", opens, 3600, "x")
			harness.mustCreate(message)
			if err := harness.blobs.Delete(t.Context(), harness.blobReference(message)); err != nil {
				t.Fatal(err)
			}
			response, _ := harness.claim(message, "", "")
			if response.StatusCode != http.StatusInternalServerError || response.Header.Get("X-Nylzo-Lease") != "" {
				t.Fatalf(
					"opens %d: a lost blob must answer 500 before any open is spent: %d",
					opens,
					response.StatusCode,
				)
			}
			synctest.Sleep(store.LeaseDuration + time.Minute)
			if _, err := harness.server.sweepOnce(t.Context()); err != nil {
				t.Fatal(err)
			}
			if got, metadata := harness.metadata(
				message,
			); got.StatusCode != http.StatusOK || metadata["state"] != "available" ||
				metadata["opens_remaining"] != float64(opens) {
				t.Fatalf("opens %d: a lost blob must not spend an open or end the message: %v", opens, metadata)
			}
		}
	})
}

func TestInterruptedEndingIsFinishedOnAccess(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		crash := func(storage store.Store, message *testMessage, state store.State) {
			record := harness.recordIn(storage, message)
			if _, err := store.MarkEnding(t.Context(), storage, &record, state, time.Now()); err != nil {
				t.Error(err)
			}
			if err := harness.blobs.Delete(t.Context(), harness.blobReference(message)); err != nil {
				t.Error(err)
			}
		}
		claimed := newMessage(t, core.KDFNone, "", 2, 3600, "x")
		harness.mustCreate(claimed)
		crash(harness.store, claimed, store.StateCancelled)
		response, body := harness.claim(claimed, "", "")
		if response.StatusCode != http.StatusGone || response.Header.Get("X-Nylzo-Lease") != "" ||
			decodeJSON(t, body)["state"] != "cancelled" {
			t.Fatalf("claim after an interrupted cancel: %d %s", response.StatusCode, body)
		}
		if _, status := harness.status(claimed); status["state"] != "cancelled" {
			t.Fatalf("status: %v", status)
		}
		downloaded := newMessage(t, core.KDFNone, "", 1, 3600, "x")
		harness.mustCreate(downloaded)
		response, _ = harness.claim(downloaded, "", "")
		lease := response.Header.Get("X-Nylzo-Lease")
		if response.StatusCode != http.StatusOK || lease == "" {
			t.Fatalf("claim: %d", response.StatusCode)
		}
		inner := harness.store
		harness.store = &racingStore{Store: inner, race: func() { crash(inner, downloaded, store.StateDeleted) }}
		harness.reopen(&harness.settings)
		response, body = harness.blob(downloaded, lease)
		if response.StatusCode != http.StatusGone || decodeJSON(t, body)["state"] != "deleted" {
			t.Fatalf("download racing a burn: %d %s", response.StatusCode, body)
		}
		if record, err := harness.recordFrom(inner, downloaded); err != nil || record.HasBlob() ||
			record.State(time.Now()) != store.StateDeleted {
			t.Fatalf("the download must finish the interrupted burn: %+v %v", record.Message, err)
		}
	})
}

func TestClaimKeepsLeaseWhenTheStreamFails(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		faulty := harness.withFaultyBlobs()
		message := newMessage(t, core.KDFNone, "", 1, 3600, "x")
		harness.mustCreate(message)
		login, _, _ := harness.handshake(message, "", "")
		faulty.FailStream(errInjected)
		request, err := http.NewRequest(
			http.MethodPost,
			harness.httpServer.URL+"/api/v1/messages/"+message.id.String()+"/claim",
			http.NoBody,
		)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set(api.ShareKeyHeaderName, testShareKeyHeader)
		request.Header.Set(api.KE3HeaderName, login.ke3)
		request.Header.Set(api.FlagsHeaderName, strconv.Itoa(envelope.FlagDeadline))
		harness.sign(request, message.readToken, &login.challenge, nil)
		request.Header.Set("X-Forwarded-For", message.address)
		claimed, err := harness.client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_, readErr := io.ReadAll(claimed.Body)
		claimed.Body.Close()
		lease := claimed.Header.Get("X-Nylzo-Lease")
		if claimed.StatusCode != http.StatusOK || lease == "" || readErr == nil {
			t.Fatalf(
				"a stream that fails after the claim must still carry the lease: %d %q %v",
				claimed.StatusCode,
				lease,
				readErr,
			)
		}
		harness.server.flushCounts(t.Context())
		if !strings.Contains(harness.logs.String(), `"msg":"blob-stream"`) {
			t.Fatalf("a failed stream must be logged: %s", harness.logs.String())
		}
		for line := range strings.SplitSeq(harness.logs.String(), "\n") {
			var entry map[string]any
			if json.Unmarshal([]byte(line), &entry) != nil || entry["msg"] != "blob-stream" {
				continue
			}
			delete(entry, "time")
			delete(entry, "level")
			delete(entry, "msg")
			delete(entry, "count")
			if len(entry) != 0 {
				t.Fatalf("the stream warning carries more than its name and count: %s", line)
			}
		}
		faulty.FailStream(nil)
		response, object := harness.blob(message, lease)
		if response.StatusCode != http.StatusOK || !bytes.Equal(object, message.object) {
			t.Fatalf("retry through the lease: %d", response.StatusCode)
		}
	})
}

func TestStorageCancellationIsInternalWhileClientWaits(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		faulty := harness.withFaultyBlobs()
		faulty.FailPut(context.Canceled)
		if response, _ := harness.create(
			newMessage(t, core.KDFNone, "", 1, 3600, "x"),
		); response.StatusCode != http.StatusInternalServerError {
			t.Fatalf("storage cancellation with a live client: %d", response.StatusCode)
		}
	})
}

func TestClaimFindsBlobRemovedByCommittedEnding(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		faulty := harness.withFaultyBlobs()
		message := newMessage(t, core.KDFNone, "", 1, 3600, "x")
		harness.mustCreate(message)
		inner := harness.store
		faulty.OnOpen(func(string) {
			record := harness.recordIn(inner, message)
			if _, err := store.End(
				t.Context(),
				inner,
				faulty.Blobs,
				&record,
				store.StateDeleted,
				time.Now(),
			); err != nil {
				t.Error(err)
			}
		})
		response, body := harness.claim(message, "", "")
		if response.StatusCode != http.StatusGone || response.Header.Get("X-Nylzo-Lease") != "" ||
			decodeJSON(t, body)["state"] != "deleted" {
			t.Fatalf("claim racing a committed burn: %d %s", response.StatusCode, body)
		}
	})
}

// ackRacingStore burns the message right before the first write, which is the ack transaction.
type ackRacingStore struct {
	store.Store

	once sync.Once
	race func()
}

func (s *ackRacingStore) Update(ctx context.Context, locator seal.Locator, change func(*store.Row) error) error {
	s.once.Do(s.race)
	return s.Store.Update(ctx, locator, change)
}

func TestAckAfterAConcurrentBurnAnswersGone(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFNone, "", 2, 3600, "x")
		harness.mustCreate(message)
		response, _ := harness.claim(message, "", "")
		lease := response.Header.Get("X-Nylzo-Lease")
		if response.StatusCode != http.StatusOK || lease == "" {
			t.Fatalf("claim: %d", response.StatusCode)
		}
		inner, blobs := harness.store, harness.blobs
		harness.store = &ackRacingStore{Store: inner, race: func() {
			record := harness.recordIn(inner, message)
			if _, err := store.End(
				t.Context(),
				inner,
				blobs,
				&record,
				store.StateDeleted,
				time.Now(),
			); err != nil {
				t.Error(err)
			}
		}}
		harness.reopen(&harness.settings)
		response, body := harness.do(
			&testRequest{
				method:  http.MethodPost,
				path:    "/api/v1/messages/" + message.id.String() + "/ack",
				address: message.address,
				token:   message.readToken,
				headers: map[string]string{"X-Nylzo-Lease": lease},
			},
		)
		if response.StatusCode != http.StatusGone || decodeJSON(t, body)["state"] != "deleted" {
			t.Fatalf("ack after a burn removed its lease: %d %s", response.StatusCode, body)
		}
	})
}

type countingReleaseStore struct {
	store.Store

	releases atomic.Int32
}

func (s *countingReleaseStore) Update(ctx context.Context, locator seal.Locator, change func(*store.Row) error) error {
	s.releases.Add(1)
	return s.Store.Update(ctx, locator, change)
}

func (s *countingReleaseStore) ShredBlobs(ctx context.Context, due []store.DueBlob) error {
	s.releases.Add(1)
	return s.Store.ShredBlobs(ctx, due)
}

func TestRequestDuringTheLeaseTakesNoWriteLock(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFNone, "", 1, 3600, "x")
		harness.mustCreate(message)
		if response, _ := harness.claim(message, "", ""); response.StatusCode != http.StatusOK {
			t.Fatalf("claim: %d", response.StatusCode)
		}
		counting := &countingReleaseStore{Store: harness.store}
		harness.store = counting
		harness.reopen(&harness.settings)
		wrong := *message
		wrong.readToken = core.Encode(core.NewSecret()[:])
		harness.metadata(message)
		harness.metadata(&wrong)
		if releases := counting.releases.Load(); releases != 0 {
			t.Fatalf("requests during the lease tried to release the blob %d times", releases)
		}
	})
}

func TestExpiredMessageWithStuckDeleteReportsItsEnd(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		faulty := harness.withFaultyBlobs()
		unspent := newMessage(t, core.KDFNone, "", 2, 60, "x")
		spent := newMessage(t, core.KDFNone, "", 1, 3600, "y")
		harness.mustCreate(unspent)
		harness.mustCreate(spent)
		if response, _ := harness.claim(spent, "", ""); response.StatusCode != http.StatusOK {
			t.Fatalf("claim: %d", response.StatusCode)
		}
		faulty.FailDelete(harness.blobReference(unspent), errInjected)
		faulty.FailDelete(harness.blobReference(spent), errInjected)
		synctest.Sleep(time.Hour)
		if _, metadata := harness.metadata(unspent); metadata["state"] != "expired" {
			t.Fatalf("unspent: %v", metadata)
		}
		if _, metadata := harness.metadata(spent); metadata["state"] != "opened" {
			t.Fatalf("spent: %v", metadata)
		}
		if response, body := harness.claim(
			unspent,
			"",
			"",
		); response.StatusCode != http.StatusGone ||
			decodeJSON(t, body)["state"] != "expired" {
			t.Fatalf("claim: %d %s", response.StatusCode, body)
		}
	})
}

func TestClaimOpenErrorSpendsNoOpen(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		faulty := harness.withFaultyBlobs()
		message := newMessage(t, core.KDFNone, "", 1, 3600, "x")
		harness.mustCreate(message)
		faulty.FailOpen(errInjected)
		response, _ := harness.claim(message, "", "")
		if response.StatusCode != http.StatusInternalServerError || response.Header.Get("X-Nylzo-Lease") != "" {
			t.Fatalf(
				"a blob open error before the claim: %d lease=%q",
				response.StatusCode,
				response.Header.Get("X-Nylzo-Lease"),
			)
		}
		faulty.FailOpen(nil)
		if _, metadata := harness.metadata(message); metadata["opens_remaining"] != float64(1) {
			t.Fatalf("an open error spent an open: %v", metadata)
		}
	})
}

func TestMalformedIDsDoNotSpendReadLimit(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		settings := baseSettings(pepperA, nil)
		settings.Rates.ReadPerHour = 1
		harness := newHarness(t, settings)
		message := newMessage(t, core.KDFNone, "", 1, 3600, "n")
		message.address = "203.0.113.5"
		harness.mustCreate(message)
		for _, path := range []string{"/api/v1/messages/short", "/api/v1/messages/short/claim", "/api/v1/messages/short/blob"} {
			method := "GET"
			if strings.HasSuffix(path, "/claim") {
				method = "POST"
			}
			if response, _ := harness.do(
				&testRequest{method: method, path: path, address: message.address, token: message.readToken},
			); response.StatusCode != http.StatusNotFound {
				t.Fatalf("%s %s: %d", method, path, response.StatusCode)
			}
		}
		if response, _ := harness.metadata(message); response.StatusCode != http.StatusOK {
			t.Fatalf("malformed ids spent the read limit: %d", response.StatusCode)
		}
	})
}

func TestRefusedReaderDoesNotSpendItsRange(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		settings := baseSettings(pepperA, nil)
		settings.Rates.ReadPerHour = 1
		settings.Rates.CoarseMultiplier = 2
		harness := newHarness(t, settings)
		message := newMessage(t, core.KDFNone, "", 1, 3600, "n")
		harness.mustCreate(message)
		read := func(ip string) int {
			reader := *message
			reader.address = ip
			response, _ := harness.metadata(&reader)
			return response.StatusCode
		}
		for i := range 4 {
			if want := map[bool]int{true: 200, false: 429}[i == 0]; read("198.51.100.1") != want {
				t.Fatalf("read %d from one source, want %d", i+1, want)
			}
		}
		if code := read("198.51.100.2"); code != http.StatusOK {
			t.Fatalf("a refused reader must not use up its /24: %d", code)
		}
		if code := read("198.51.100.3"); code != http.StatusTooManyRequests {
			t.Fatalf("the /24 read budget still applies: %d", code)
		}
	})
}

func TestReadRateLimitCountsMetadataHandshakeClaimAndBlob(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		settings := baseSettings(pepperA, nil)
		settings.Rates.ReadPerHour = 4
		harness := newHarness(t, settings)
		message := newMessage(t, core.KDFNone, "", 1, 3600, "n")
		message.address = "203.0.113.5"
		harness.mustCreate(message)
		for i := range 2 {
			if response, _ := harness.metadata(message); response.StatusCode != http.StatusOK {
				t.Fatalf("metadata %d: %d", i, response.StatusCode)
			}
		}
		claimed, _ := harness.claim(message, "", "")
		lease := claimed.Header.Get("X-Nylzo-Lease")
		if claimed.StatusCode != http.StatusOK || lease == "" {
			t.Fatalf("the third and fourth reads, a handshake and its claim: %d", claimed.StatusCode)
		}
		if response, _ := harness.blob(message, lease); response.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("a download over the read limit: %d", response.StatusCode)
		}
		if response, _ := harness.metadata(message); response.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("metadata over the read limit: %d", response.StatusCode)
		}
		message.address = "203.0.113.6"
		if response, _ := harness.blob(message, lease); response.StatusCode != http.StatusOK {
			t.Fatalf("another address keeps its own read budget: %d", response.StatusCode)
		}
	})
}

func TestReadBytesPerHourStopsAClaimBeforeItSpendsAnOpen(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		settings := harness.settings
		settings.Limits.MaxFileBytes = 1
		settings.Limits.MaxObjectBytes = envelope.ObjectLimit(1)
		settings.Rates.ReadBytesPerHour = settings.Limits.MaxObjectBytes
		harness.reopen(&settings)
		probe := newMessage(t, core.KDFNone, "", 1, 3600, "x")
		within := int(settings.Rates.ReadBytesPerHour / int64(len(probe.object)))
		message := newMessage(t, core.KDFNone, "", uint16(within+1), 3600, "x")
		harness.mustCreate(message)
		for range within + 1 {
			outdated, _, _ := harness.claimRequestFor(message, "", "")
			outdated.oldReader = true
			if response, _ := harness.do(outdated); response.StatusCode != http.StatusBadRequest {
				t.Fatalf("an outdated reader's claim: %d", response.StatusCode)
			}
		}
		for range within {
			if response, _ := harness.claim(message, "", ""); response.StatusCode != http.StatusOK {
				t.Fatalf("a claim within the byte budget: %d", response.StatusCode)
			}
		}
		if response, _ := harness.claim(message, "", ""); response.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("a claim past the byte budget: %d", response.StatusCode)
		}
		if _, metadata := harness.metadata(message); metadata["opens_remaining"] != float64(1) {
			t.Fatalf("a claim refused for its bytes spent an open: %v", metadata)
		}
	})
}

func TestASignedRequestWithAnEmptyChunkedBodyIsServed(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFNone, "", 1, 3600, "x")
		harness.mustCreate(message)
		path := "/api/v1/messages/" + message.id.String()
		r, _ := http.NewRequest(http.MethodGet, harness.httpServer.URL+path, strings.NewReader(""))
		r.ContentLength = -1
		r.TransferEncoding = []string{"chunked"}
		r.Header.Set(
			"Authorization",
			signing.SignRequest(&message.link.R, signing.PurposeRead, "GET", path, harness.challenge(), nil, signing.Components{}).
				Authorization(),
		)
		r.Header.Set("X-Forwarded-For", message.address)
		response, err := harness.client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("a proxy's empty chunked body on a signed request: %d", response.StatusCode)
		}
	})
}

// A message whose registration pepper the instance does not hold answers 503 and counts nothing, and opens again once
// that pepper is configured as the previous one (spec section 6.4).
func TestAMessageWhoseRegistrationPepperIsMissingWaitsForIt(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFNone, "", 1, 3600, "registered under pepper A")
		harness.mustCreate(message)
		harness.reopen(baseSettings(pepperB, pepperA))
		harness.reopen(baseSettings(pepperC, pepperB))
		_, response, body := harness.handshake(message, "", "")
		if response.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("a handshake under a pepper that is missing: %d %s", response.StatusCode, body)
		}
		if failures := harness.mustRecord(message).Message.FailCountTotal; failures != 0 {
			t.Fatalf("the handshake counted %d attempts", failures)
		}
		harness.reopen(baseSettings(pepperC, pepperA))
		if claimed, _ := harness.claim(message, "", ""); claimed.StatusCode != http.StatusOK {
			t.Fatalf("a claim once the pepper is back: %d", claimed.StatusCode)
		}
	})
}

// A handshake refused for a malformed KE1 or a lease that is not active counts no attempt (spec section 6.4).
func TestARefusedHandshakeCountsNoAttempt(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFScrypt, "correct horse battery staple", 1, 3600, "refused handshake")
		harness.mustCreate(message)
		valid, _, err := opaque.GenerateKE1([]byte("any password"))
		if err != nil {
			t.Fatal(err)
		}
		identity := valid
		clear(identity[:opaque.ElementLen])
		for name, test := range map[string]struct {
			headers map[string]string
			status  int
		}{
			"no KE1":                      {map[string]string{}, http.StatusBadRequest},
			"a short KE1":                 {map[string]string{api.KE1HeaderName: core.Encode(valid[:opaque.KE1Len-1])}, http.StatusBadRequest},
			"an identity blinded element": {map[string]string{api.KE1HeaderName: core.Encode(identity[:])}, http.StatusBadRequest},
			"a lease that is not active": {
				map[string]string{api.KE1HeaderName: core.Encode(valid[:]), api.LeaseHeaderName: core.Encode(core.NewSecret()[:])},
				http.StatusGone,
			},
		} {
			response, body := harness.do(&testRequest{
				method:  http.MethodPost,
				path:    "/api/v1/messages/" + message.id.String() + api.HandshakeAction,
				token:   message.readToken,
				headers: test.headers,
			})
			if failures := harness.mustRecord(message).Message.FailCountTotal; response.StatusCode != test.status ||
				failures != 0 {
				t.Fatalf("%s: %d %s, %d failures", name, response.StatusCode, body, failures)
			}
		}
	})
}

// A claim or burn refused for load before its KE3 is checked still takes back the attempt of its handshake, so a busy
// server never charges a reader whose passphrase is right (spec section 6.4).
func TestARequestRefusedForLoadTakesItsHandshakeBack(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		action  string
		refuse  func(settings *config.Server)
		refusal func(server *Server)
	}{
		"a claim over the read budget": {
			action: api.ClaimAction,
			refuse: func(settings *config.Server) { settings.Rates.ReadPerHour = 1 },
		},
		"a burn over the read budget": {
			action: api.BurnAction,
			refuse: func(settings *config.Server) { settings.Rates.ReadPerHour = 1 },
		},
		"a claim the instance bucket throttles": {
			action:  api.ClaimAction,
			refusal: func(server *Server) { server.instance = ratelimit.NewFair(1, 0) },
		},
	} {
		synctest.Test(t, func(t *testing.T) {
			settings := baseSettings(pepperA, nil)
			if test.refuse != nil {
				test.refuse(settings)
			}
			harness := newHarness(t, settings)
			message := newMessage(t, core.KDFScrypt, "correct horse battery staple", 1, 3600, "refused for load")
			harness.mustCreate(message)
			login, _, _ := harness.handshake(message, message.passphrase, "")
			if test.refusal != nil {
				test.refusal(harness.server)
			}
			response, _ := harness.do(login.request(message, test.action, ""))
			failures := harness.mustRecord(message).Message.FailCountTotal
			if response.StatusCode != http.StatusTooManyRequests || failures != 0 || !harness.blobExists(message) {
				t.Fatalf("%s: %d, %d failures", name, response.StatusCode, failures)
			}
		})
	}
}

// A download refused for load before its KE3 is checked takes the attempt of its handshake back (spec section 6.4).
func TestADownloadRefusedForLoadTakesItsHandshakeBack(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFScrypt, "correct horse battery staple", 1, 3600, "refused download")
		harness.mustCreate(message)
		claimed, _ := harness.claim(message, message.passphrase, "")
		login, _, _ := harness.handshake(message, message.passphrase, "")
		harness.server.streams = ratelimit.NewStreams(0)
		response, _ := harness.do(&testRequest{
			method:    http.MethodGet,
			path:      "/api/v1/messages/" + message.id.String() + api.BlobAction,
			token:     message.readToken,
			challenge: &login.challenge,
			headers: map[string]string{
				api.LeaseHeaderName: claimed.Header.Get(api.LeaseHeaderName),
				api.KE3HeaderName:   login.ke3,
			},
		})
		failures := harness.mustRecord(message).Message.FailCountTotal
		if response.StatusCode != http.StatusServiceUnavailable || failures != 0 {
			t.Fatalf("a download refused for load: %d, %d failures", response.StatusCode, failures)
		}
	})
}

// A handshake that names an active lease runs, and counts, on a message whose last open is spent (spec section 6.5).
func TestAHandshakeNamingAnActiveLeaseCounts(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFScrypt, "correct horse battery staple", 1, 3600, "resumed")
		harness.mustCreate(message)
		claimed, _ := harness.claim(message, message.passphrase, "")
		ke1, _, err := opaque.GenerateKE1([]byte("any password"))
		if err != nil {
			t.Fatal(err)
		}
		response, body := harness.do(&testRequest{
			method: http.MethodPost,
			path:   "/api/v1/messages/" + message.id.String() + api.HandshakeAction,
			token:  message.readToken,
			headers: map[string]string{
				api.KE1HeaderName:   core.Encode(ke1[:]),
				api.LeaseHeaderName: claimed.Header.Get(api.LeaseHeaderName),
			},
		})
		failures := harness.mustRecord(message).Message.FailCountTotal
		if response.StatusCode != http.StatusOK || failures != 1 {
			t.Fatalf("a handshake naming an active lease: %d %s, %d failures", response.StatusCode, body, failures)
		}
	})
}

// TestAResumedDownloadTakesItsHandshakeBack covers spec section 6.5: a reader that resumes from its lease runs a
// handshake for P, and its download carries KE3, which takes the attempt back only when it matches.
func TestAResumedDownloadTakesItsHandshakeBack(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFScrypt, "correct horse battery staple", 1, 3600, "resumed")
		harness.mustCreate(message)
		claimed, _ := harness.claim(message, message.passphrase, "")
		lease := claimed.Header.Get(api.LeaseHeaderName)
		if claimed.StatusCode != http.StatusOK || lease == "" {
			t.Fatalf("claim: %d", claimed.StatusCode)
		}
		blobPath := "/api/v1/messages/" + message.id.String() + "/blob"
		resume := func(challenge [signing.ChallengeLen]byte, ke3 string) testResponse {
			response, _ := harness.do(&testRequest{
				method:    http.MethodGet,
				path:      blobPath,
				token:     message.readToken,
				challenge: &challenge,
				headers:   map[string]string{api.LeaseHeaderName: lease, api.KE3HeaderName: ke3},
			})
			return response
		}
		failures := func() float64 {
			_, status := harness.status(message)
			return status["failed_passphrase_attempts"].(float64)
		}
		wrong, _, _ := harness.handshake(message, "a wrong passphrase", "")
		if got := resume(wrong.challenge, wrong.ke3); got.StatusCode != http.StatusForbidden || failures() != 1 {
			t.Fatalf("a download with a KE3 that does not match: %d, %v failures", got.StatusCode, failures())
		}
		right, _, _ := harness.handshake(message, message.passphrase, "")
		if got := resume(harness.challenge(), right.ke3); got.StatusCode != http.StatusNotFound || failures() != 2 {
			t.Fatalf("a KE3 over a challenge no handshake issued: %d, %v failures", got.StatusCode, failures())
		}
		again, _, _ := harness.handshake(message, message.passphrase, "")
		if got := resume(again.challenge, again.ke3); got.StatusCode != http.StatusOK || failures() != 2 {
			t.Fatalf("a matching KE3 on the resumed download: %d, %v failures", got.StatusCode, failures())
		}
	})
}

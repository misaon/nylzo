package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/server/config"
	"github.com/misaon/nylzo/internal/server/logging"
	"github.com/misaon/nylzo/internal/server/sweep"
	"github.com/misaon/nylzo/internal/store"
	"github.com/misaon/nylzo/internal/store/disk"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

const (
	sweepDirEnv  = "NYLZO_TEST_SWEEP_DIR"
	sweepNowEnv  = "NYLZO_TEST_SWEEP_NOW"
	sweptMarker  = "nylzo-test-swept"
	stressInFlow = 32
)

func TestMain(tests *testing.M) {
	if directory := os.Getenv(sweepDirEnv); directory != "" {
		os.Exit(sweepProcess(directory, os.Getenv(sweepNowEnv)))
	}
	tests.Run()
}

func sweepProcess(directory, at string) int {
	now, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		fmt.Fprintln(os.Stderr, "time:", err)
		return 1
	}
	ctx := context.Background()
	database, err := sqlite.OpenCurrent(ctx, filepath.Join(directory, "nylzo.db"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "open:", err)
		return 1
	}
	defer database.Close()
	blobs, err := disk.New(directory)
	if err != nil {
		fmt.Fprintln(os.Stderr, "blobs:", err)
		return 1
	}
	if _, runErr := sweep.Run(ctx, database, blobs, now); !onlyCheckpointBusy(runErr) {
		fmt.Fprintln(os.Stderr, "sweep:", logging.ErrorText(runErr))
		return 1
	}
	fmt.Fprintln(os.Stdout, sweptMarker)
	return 0
}

func onlyCheckpointBusy(err error) bool {
	if err == nil {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, leaf := range joined.Unwrap() {
			if !onlyCheckpointBusy(leaf) {
				return false
			}
		}
		return true
	}
	if wrapped := errors.Unwrap(err); wrapped != nil {
		return onlyCheckpointBusy(wrapped)
	}
	return errors.Is(err, sqlite.ErrCheckpointBusy)
}

type endings struct {
	claims   atomic.Int32
	goneAcks atomic.Int32
	burns    atomic.Int32
	cancels  atomic.Int32
}

func TestStressEndingsRaceSweepsInTwoProcesses(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	settings := harness.settings
	settings.Rates.ReadPerHour = 1_000_000
	settings.Rates.CreatePerHour = 1_000_000
	settings.Rates.CreatePerDay = 1_000_000
	settings.Rates.CreateBytesPerDay = 1 << 40
	settings.MaxStreams = config.MaxStreams
	harness.reopen(&settings)
	const count = 30
	messages := make([]*testMessage, count)
	got := make([]endings, count)
	for i := range messages {
		messages[i] = newMessage(t, core.KDFNone, "", uint16(1+i%3), 3600, "stress")
		harness.mustCreate(messages[i])
	}
	expect := func(what string, code int, allowed ...int) {
		if !slices.Contains(allowed, code) {
			t.Errorf("%s answered %d, want one of %v", what, code, allowed)
		}
	}
	stop := make(chan struct{})
	var background sync.WaitGroup
	background.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := harness.server.sweepOnce(t.Context()); !onlyCheckpointBusy(err) {
				t.Errorf("sweep in the server process: %v", err)
			}
		}
	})
	var overlapping atomic.Int32
	var requestsDone atomic.Bool
	background.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			during := !requestsDone.Load()
			process := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^$")
			process.Env = append(
				os.Environ(),
				sweepDirEnv+"="+harness.directory,
				sweepNowEnv+"="+time.Now().Format(time.RFC3339Nano),
			)
			out, err := process.CombinedOutput()
			if err != nil || !strings.Contains(string(out), sweptMarker) {
				t.Errorf("sweep-once in a second process: %v %s", err, out)
				return
			}
			if during {
				overlapping.Add(1)
			}
		}
	})
	slots := make(chan struct{}, stressInFlow)
	limit := func(run func()) func() {
		return func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			run()
		}
	}
	var requests sync.WaitGroup
	for i, message := range messages {
		for j := range 4 {
			ip := fmt.Sprintf("10.%d.%d.1", i, j)
			requests.Go(limit(func() {
				response, _, err := harness.tryClaim(message, "", ip)
				if err != nil {
					t.Error(err)
					return
				}
				expect("claim", response.StatusCode, http.StatusOK, http.StatusGone)
				if response.StatusCode != http.StatusOK {
					return
				}
				got[i].claims.Add(1)
				ack, _, err := harness.tryDo(
					&testRequest{
						method:  http.MethodPost,
						path:    "/api/v1/messages/" + message.id.String() + "/ack",
						address: ip,
						token:   message.readToken,
						headers: map[string]string{"X-Nylzo-Lease": response.Header.Get("X-Nylzo-Lease")},
					},
				)
				if err != nil {
					t.Error(err)
					return
				}
				expect("ack", ack.StatusCode, http.StatusNoContent, http.StatusGone)
				if ack.StatusCode == http.StatusGone {
					got[i].goneAcks.Add(1)
				}
			}))
		}
		for _, ending := range []struct {
			action  string
			every   int
			token   string
			octet   int
			counter *atomic.Int32
		}{
			{"burn", 2, message.readToken, 10, &got[i].burns},
			{"cancel", 3, message.manageToken, 11, &got[i].cancels},
		} {
			if i%ending.every != 0 {
				continue
			}
			requests.Go(limit(func() {
				address := fmt.Sprintf("10.%d.%d.1", i, ending.octet)
				var code int
				if ending.action == "burn" {
					response, _, err := harness.tryBurn(message, "", address)
					if err != nil {
						t.Error(err)
					}
					code = response.StatusCode
				} else {
					code = harness.statusFrom(&testRequest{
						method:  http.MethodPost,
						path:    "/api/v1/messages/" + message.id.String() + "/" + ending.action,
						address: address,
						token:   ending.token,
					})
				}
				expect(ending.action, code, http.StatusNoContent, http.StatusGone)
				if code == http.StatusNoContent {
					ending.counter.Add(1)
				}
			}))
		}
		requests.Go(limit(func() {
			code := harness.statusFrom(
				&testRequest{
					method:  http.MethodGet,
					path:    "/api/v1/messages/" + message.id.String() + "/status",
					address: fmt.Sprintf("10.%d.12.1", i),
					token:   message.manageToken,
				},
			)
			expect("status", code, http.StatusOK)
		}))
	}
	requests.Wait()
	requestsDone.Store(true)
	close(stop)
	background.Wait()
	if overlapping.Load() == 0 {
		t.Error("no sweep-once process ran while the requests ran")
	}
	for i, message := range messages {
		claims, goneAcks, burns, cancels := got[i].claims.Load(), got[i].goneAcks.Load(), got[i].burns.Load(), got[i].cancels.Load()
		opens := int32(message.policy.OpensMax)
		if claims > opens || burns+cancels > 1 {
			t.Errorf("message %d: %d claims of %d opens, %d burns, %d cancels", i, claims, opens, burns, cancels)
			continue
		}
		if burns+cancels == 0 && (claims != opens || goneAcks != 0) {
			t.Errorf(
				"message %d: with four readers and no deletion, %d of %d opens were claimed and %d acks were refused",
				i,
				claims,
				opens,
				goneAcks,
			)
		}
		_, status := harness.status(message)
		var want string
		switch {
		case cancels == 1:
			want = "cancelled"
		case burns == 1:
			want = "deleted"
		case claims == int32(message.policy.OpensMax):
			want = "opened"
		default:
			want = "available"
		}
		if status["state"] != want {
			t.Errorf(
				"message %d ended as %v, want %s (%d claims of %d opens, %d burns, %d cancels)",
				i,
				status["state"],
				want,
				claims,
				opens,
				burns,
				cancels,
			)
		}
		if ended := want != "available"; harness.blobExists(message) == ended {
			t.Errorf("message %d in state %s: blob present %v", i, want, !ended)
		}
	}
	_, err := sweep.Run(t.Context(), harness.store, harness.blobs, time.Now().Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for i, message := range messages {
		if harness.blobExists(message) {
			t.Errorf("message %d still has its blob after it expired and was swept", i)
		}
	}
}

func TestTakenDownMessageReadsAsDeleted(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFNone, "", 1, 3600, "taken down")
		harness.mustCreate(message)
		locator := seal.LocatorOf(message.id[:])
		if _, err := store.TakeDown(t.Context(), harness.store, harness.blobs, locator, time.Now()); err != nil {
			t.Fatal(err)
		}
		if response, metadata := harness.metadata(message); response.StatusCode != http.StatusOK ||
			metadata["state"] != "deleted" {
			t.Fatalf("metadata of a message the operator removed: %d %v", response.StatusCode, metadata)
		}
		if response, body := harness.claim(message, "", ""); response.StatusCode != http.StatusGone ||
			decodeJSON(t, body)["state"] != "deleted" {
			t.Fatalf("claim of a message the operator removed: %d %s", response.StatusCode, body)
		}
	})
}

func TestBurnAndCancelEndpoints(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		burn := newMessage(t, core.KDFNone, "", 1, 3600, "b")
		cancel := newMessage(t, core.KDFNone, "", 2, 3600, "c")
		harness.mustCreate(burn)
		harness.mustCreate(cancel)
		unknown, unknownBody := harness.do(
			&testRequest{
				method:  http.MethodPost,
				path:    "/api/v1/messages/" + core.NewID().String() + "/burn",
				token:   burn.readToken,
				headers: map[string]string{api.KE3HeaderName: core.Encode(make([]byte, 64))},
			},
		)
		if unknown.StatusCode != http.StatusNotFound {
			t.Fatalf("unknown id: %d", unknown.StatusCode)
		}
		for _, testCase := range []struct {
			message       *testMessage
			action, token string
		}{{burn, "burn", burn.manageToken}, {cancel, "cancel", cancel.readToken}} {
			response, body := harness.post(testCase.message, testCase.action, testCase.token)
			if response.StatusCode != http.StatusNotFound || string(body) != string(unknownBody) {
				t.Fatalf("%s with the token of the other purpose: %d %s", testCase.action, response.StatusCode, body)
			}
			if !harness.blobExists(testCase.message) {
				t.Fatalf("%s with a wrong token removed the blob", testCase.action)
			}
		}
		if response, _ := harness.post(
			burn,
			"burn",
			burn.readToken,
		); response.StatusCode != http.StatusNoContent ||
			harness.blobExists(burn) {
			t.Fatalf("burn: %d", response.StatusCode)
		}
		if _, metadata := harness.metadata(burn); metadata["state"] != "deleted" {
			t.Fatalf("metadata after burn: %v", metadata)
		}
		if response, _ := harness.claim(cancel, "", ""); response.StatusCode != http.StatusOK {
			t.Fatal("claim before cancel")
		}
		if response, _ := harness.post(
			cancel,
			"cancel",
			cancel.manageToken,
		); response.StatusCode != http.StatusNoContent ||
			harness.blobExists(cancel) {
			t.Fatalf("cancel: %d", response.StatusCode)
		}
		if _, status := harness.status(cancel); status["state"] != "cancelled" || status["first_opened_at"] == nil {
			t.Fatalf("status after cancel: %v", status)
		}
		response, body := harness.post(burn, "burn", burn.readToken)
		if response.StatusCode != http.StatusGone || decodeJSON(t, body)["state"] != "deleted" {
			t.Fatalf("second burn: %d %s", response.StatusCode, body)
		}
		if got, _ := harness.post(cancel, "cancel", cancel.manageToken); got.StatusCode != http.StatusGone {
			t.Fatalf("second cancel: %d", got.StatusCode)
		}
	})
}

func TestBurnNeedsAPassphraseHandshake(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFScrypt, "correct", 2, 3600, "guarded")
		harness.mustCreate(message)
		burnPath := "/api/v1/messages/" + message.id.String() + "/burn"
		expectRefusal := func(response testResponse, body responseBody, attemptsLeft float64) {
			t.Helper()
			fields := decodeJSON(t, body)
			if response.StatusCode != http.StatusForbidden || fields["error"] != "passphrase" ||
				fields["retry_after"].(float64) != 0 || fields["attempts_left"].(float64) != attemptsLeft {
				t.Fatalf("got %d %s, want a refusal with %v attempts left", response.StatusCode, body, attemptsLeft)
			}
			if !harness.blobExists(message) {
				t.Fatal("a refused burn deleted the blob")
			}
		}
		response, body := harness.do(&testRequest{method: http.MethodPost, path: burnPath, token: message.readToken})
		if response.StatusCode != http.StatusBadRequest || !harness.blobExists(message) {
			t.Fatalf("a burn without KE3 must be refused uncounted: %d %s", response.StatusCode, body)
		}
		response, body = harness.burn(message, "wrong", "")
		expectRefusal(response, body, 4)
		response, body = harness.claim(message, "wrong", "")
		expectRefusal(response, body, 3)
		login, _, _ := harness.handshake(message, message.passphrase, "")
		malformed := login.request(message, "/burn", "")
		malformed.headers[api.KE3HeaderName] = "not-a-ke3"
		if response, _ = harness.do(malformed); response.StatusCode != http.StatusBadRequest {
			t.Fatalf("a malformed KE3: %d", response.StatusCode)
		}
		if _, status := harness.status(message); status["state"] != "available" ||
			status["failed_passphrase_attempts"].(float64) != 3 {
			t.Fatalf("claims and burns share the counters, and a handshake counts until its KE3: %v", status)
		}
		path := harness.blobFile(message)
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		changedSalt := bytes.Clone(original)
		changedSalt[0] ^= 1
		if err = os.WriteFile(path, changedSalt, 0o600); err != nil {
			t.Fatal(err)
		}
		response, _ = harness.burn(message, message.passphrase, "")
		if err = os.WriteFile(path, original, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, status := harness.status(message); response.StatusCode != http.StatusInternalServerError ||
			status["failed_passphrase_attempts"].(float64) != 3 {
			t.Fatalf("a changed salt must be refused before it costs a passphrase attempt: %d %v",
				response.StatusCode, status)
		}
		changedBody := bytes.Clone(original)
		changedBody[len(changedBody)-1] ^= 1
		if err = os.WriteFile(path, changedBody, 0o600); err != nil {
			t.Fatal(err)
		}
		if response, body = harness.burn(
			message,
			message.passphrase,
			"",
		); response.StatusCode != http.StatusNoContent ||
			harness.blobExists(message) {
			t.Fatalf("burn with the passphrase: %d %s", response.StatusCode, body)
		}
		if _, metadata := harness.metadata(message); metadata["state"] != "deleted" {
			t.Fatalf("metadata after the burn: %v", metadata)
		}
	})
}

type racingStore struct {
	store.Store

	once sync.Once
	skip atomic.Int32
	race func()
}

func (s *racingStore) Load(ctx context.Context, locator seal.Locator) (store.Row, error) {
	row, err := s.Store.Load(ctx, locator)
	if err == nil {
		s.raceOnce()
	}
	return row, err
}

func (s *racingStore) Lookup(ctx context.Context, locator seal.Locator) (store.Row, bool, error) {
	row, known, err := s.Store.Lookup(ctx, locator)
	if err == nil && known {
		s.raceOnce()
	}
	return row, known, err
}

func (s *racingStore) raceOnce() {
	if s.skip.Add(-1) < 0 {
		s.once.Do(s.race)
	}
}

func TestBurnRacingACancelReportsTheCancel(t *testing.T) {
	t.Parallel()
	for name, finished := range map[string]bool{"recorded cancel": false, "finished cancel": true} {
		for _, kdf := range []core.KDF{core.KDFNone, core.KDFScrypt} {
			t.Run(fmt.Sprintf("%s, kdf %d", name, kdf), func(t *testing.T) {
				t.Parallel()
				synctest.Test(t, func(t *testing.T) {
					harness := newHarness(t, baseSettings(pepperA, nil))
					message := newMessage(t, kdf, "correct", 1, 3600, "x")
					harness.mustCreate(message)
					harness.endOnLoad(message, store.StateCancelled, finished, 1)
					login, _, _ := harness.handshake(message, message.passphrase, "")
					response, body := harness.do(login.request(message, "/burn", ""))
					if response.StatusCode != http.StatusGone || decodeJSON(t, body)["state"] != "cancelled" {
						t.Fatalf("burn that lost the race: %d %s", response.StatusCode, body)
					}
					if _, status := harness.status(message); status["state"] != "cancelled" {
						t.Fatalf("status: %v", status)
					}
				})
			})
		}
	}
}

type failingForgetStore struct {
	store.Store
}

func (failingForgetStore) ForgetBlobs(context.Context, []store.DueBlob) error {
	return errInjected
}

func TestUnfinishableEndingReportsRecordedState(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFNone, "", 2, 3600, "x")
		harness.mustCreate(message)
		inner := harness.store
		harness.store = &racingStore{Store: failingForgetStore{Store: inner}, race: func() {
			record := harness.recordIn(inner, message)
			if _, err := store.MarkEnding(
				t.Context(),
				inner,
				&record,
				store.StateCancelled,
				time.Now(),
			); err != nil {
				t.Error(err)
			}
			if err := harness.blobs.Delete(t.Context(), harness.blobReference(message)); err != nil {
				t.Error(err)
			}
		}}
		harness.reopen(&harness.settings)
		response, body := harness.claim(message, "", "")
		if response.StatusCode != http.StatusGone || decodeJSON(t, body)["state"] != "cancelled" {
			t.Fatalf(
				"an ending that cannot finish must still report its recorded state: %d %s",
				response.StatusCode,
				body,
			)
		}
	})
}

// vanishingStore lets the handshake count and the burn take its attempt back, then loses the row.
type vanishingStore struct {
	store.Store

	armed   atomic.Bool
	updates atomic.Int32
}

func (s *vanishingStore) Update(ctx context.Context, locator seal.Locator, change func(*store.Row) error) error {
	if s.updates.Add(1) <= 2 {
		return s.Store.Update(ctx, locator, change)
	}
	s.armed.Store(true)
	return store.ErrNotFound
}

func (s *vanishingStore) Load(ctx context.Context, locator seal.Locator) (store.Row, error) {
	if s.armed.Load() {
		return store.Row{}, errInjected
	}
	return s.Store.Load(ctx, locator)
}

func TestBurnWithUnreadableEndAnswers500(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFNone, "", 1, 3600, "x")
		harness.mustCreate(message)
		harness.store = &vanishingStore{Store: harness.store}
		harness.reopen(&harness.settings)
		if response, body := harness.post(
			message,
			"burn",
			message.readToken,
		); response.StatusCode != http.StatusInternalServerError {
			t.Fatalf("a burn that cannot read the final state must not guess it: %d %s", response.StatusCode, body)
		}
	})
}

type deleteHookBlobs struct {
	store.Blobs

	once sync.Once
	hook func()
}

func (b *deleteHookBlobs) Delete(ctx context.Context, reference string) error {
	b.once.Do(b.hook)
	return b.Blobs.Delete(ctx, reference)
}

func TestBurnWinsWhenARaceFinishesItsEnding(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFNone, "", 1, 3600, "x")
		harness.mustCreate(message)
		if response, _ := harness.claim(message, "", ""); response.StatusCode != http.StatusOK {
			t.Fatalf("claim: %d", response.StatusCode)
		}
		raw := harness.blobs
		harness.blobs = &deleteHookBlobs{Blobs: raw, hook: func() {
			synctest.Sleep(store.LeaseDuration)
			record := harness.mustRecord(message)
			if _, err := store.Finish(t.Context(), harness.store, raw, &record, time.Now()); err != nil {
				t.Error(err)
			}
		}}
		harness.reopen(&harness.settings)
		if response, body := harness.post(
			message,
			"burn",
			message.readToken,
		); response.StatusCode != http.StatusNoContent {
			t.Fatalf(
				"the burn that recorded the first ending must answer 204 when a release finishes it: %d %s",
				response.StatusCode,
				body,
			)
		}
		if state, gone := harness.endedState(message); !gone || state != store.StateDeleted {
			t.Fatalf("after the burn: %s %v", state, gone)
		}
	})
}

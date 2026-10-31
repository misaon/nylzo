package server

import (
	"context"
	"encoding/json"
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
	if _, runErr := sweep.Run(ctx, database, blobs, directory, now); !onlyCheckpointBusy(runErr) {
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
				response, _, err := harness.tryDo(claimRequestFor(message, "", ip))
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
				code := harness.statusFrom(
					&testRequest{
						method:  http.MethodPost,
						path:    "/api/v1/messages/" + message.id.String() + "/" + ending.action,
						address: fmt.Sprintf("10.%d.%d.1", i, ending.octet),
						token:   ending.token,
					},
				)
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
	_, err := sweep.Run(t.Context(), harness.store, harness.blobs, harness.directory, time.Now().Add(2*time.Hour))
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
		message := newMessage(t, core.KDFNone, "", 1, 3600, "reported")
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
				method: http.MethodPost,
				path:   "/api/v1/messages/" + core.NewID().String() + "/burn",
				token:  burn.readToken,
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

type racingStore struct {
	store.Store

	once sync.Once
	race func()
}

func (s *racingStore) Load(ctx context.Context, locator seal.Locator) (store.Row, error) {
	row, err := s.Store.Load(ctx, locator)
	if err == nil {
		s.once.Do(s.race)
	}
	return row, err
}

func TestBurnRacingACancelReportsTheCancel(t *testing.T) {
	t.Parallel()
	for name, finished := range map[string]bool{"recorded cancel": false, "finished cancel": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				harness := newHarness(t, baseSettings(pepperA, nil))
				message := newMessage(t, core.KDFNone, "", 1, 3600, "x")
				harness.mustCreate(message)
				harness.endOnFirstLoad(message, store.StateCancelled, finished)
				response, body := harness.post(message, "burn", message.readToken)
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
			if err := harness.blobs.Delete(t.Context(), harness.blobRef(message)); err != nil {
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

type vanishingStore struct {
	store.Store

	armed atomic.Bool
}

func (s *vanishingStore) Update(context.Context, seal.Locator, func(*store.Row) error) error {
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

func (b *deleteHookBlobs) Delete(ctx context.Context, ref string) error {
	b.once.Do(b.hook)
	return b.Blobs.Delete(ctx, ref)
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

func abuseReport(t *testing.T, harness *testHarness, message *testMessage, address string) {
	t.Helper()
	report := map[string]any{"id": message.id.String(), "reason": "spam"}
	if got := reportAbuse(t, harness, message.readToken, address, report); !strings.HasPrefix(got, "202 ") {
		t.Fatalf("report: %s", got)
	}
}

func TestReportsAboutEndedMessagesAreNotStored(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		faulty := harness.withFaultyBlobs()
		burned := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
		expired := newMessage(t, core.KDFNone, "", 1, 60, "abuse")
		opened := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
		ending := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
		live := newMessage(t, core.KDFNone, "", 2, 3600, "abuse")
		for _, message := range []*testMessage{burned, expired, opened, ending, live} {
			harness.mustCreate(message)
		}
		harness.markEnding(ending, store.StateCancelled)
		faulty.FailDelete(harness.blobRef(ending), errInjected)
		if response, _ := harness.post(burned, "burn", burned.readToken); response.StatusCode != http.StatusNoContent {
			t.Fatalf("burn: %d", response.StatusCode)
		}
		response, _ := harness.claim(opened, "", "")
		if response.StatusCode != http.StatusOK ||
			harness.ack(opened, response.Header.Get("X-Nylzo-Lease")).StatusCode != http.StatusNoContent {
			t.Fatalf("open: %d", response.StatusCode)
		}
		synctest.Sleep(2 * time.Minute)
		for i, message := range []*testMessage{burned, expired, opened, ending} {
			abuseReport(t, harness, message, fmt.Sprintf("203.0.113.%d", 50+i))
		}
		if data, err := harness.abuseReports(); (err != nil && !errors.Is(err, os.ErrNotExist)) || len(data) != 0 {
			t.Fatalf("reports about ended messages were stored: %q %v", data, err)
		}
		abuseReport(t, harness, live, "203.0.113.60")
		if data, err := harness.abuseReports(); err != nil || !strings.Contains(string(data), live.id.String()) {
			t.Fatalf("the report about a live message was not stored: %q %v", data, err)
		}
	})
}

func TestMalformedAbuseReportsSpendNoRateLimit(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		settings := baseSettings(pepperA, nil)
		settings.Rates.AbusePerHour = 1
		harness := newHarness(t, settings)
		message := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
		harness.mustCreate(message)
		report := func(id string) int {
			body, _ := json.Marshal(map[string]string{"id": id, "reason": "spam"})
			response, _ := harness.do(
				&testRequest{
					method:  http.MethodPost,
					path:    "/api/v1/abuse",
					address: "203.0.113.44",
					body:    body,
					token:   message.readToken,
					headers: map[string]string{"Content-Type": "application/json"},
				},
			)
			return response.StatusCode
		}
		for range 3 {
			if code := report("not-an-id"); code != http.StatusAccepted {
				t.Fatalf("malformed id: %d", code)
			}
		}
		if code := report(message.id.String()); code != http.StatusAccepted {
			t.Fatalf("a valid report after malformed ones was rate limited: %d", code)
		}
		if data, err := harness.abuseReports(); err != nil || !strings.Contains(string(data), message.id.String()) {
			t.Fatalf("the valid report was not stored: %v", err)
		}
	})
}

func TestAbuseReportsRecordOnlyValidReports(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
		harness.mustCreate(message)
		send := func(token, body string) int {
			response, _ := harness.do(
				&testRequest{
					method:  http.MethodPost,
					path:    "/api/v1/abuse",
					address: "203.0.113.70",
					body:    []byte(body),
					token:   token,
					headers: map[string]string{"Content-Type": "application/json"},
				},
			)
			return response.StatusCode
		}
		long := strings.Repeat("r", 3000)
		if send(message.readToken, fmt.Sprintf(`{"id":%q,"reason":%q}`, message.id.String(), long)) != 202 ||
			send(message.readToken, fmt.Sprintf(`{"id":%q}`, core.NewID().String())) != 202 ||
			send(core.Encode(make([]byte, 32)), fmt.Sprintf(`{"id":%q}`, message.id.String())) != 202 {
			t.Fatal("reports must answer 202")
		}
		if send("", `{"id":`) != 400 {
			t.Fatal("malformed report body")
		}
		data, err := harness.abuseReports()
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) != 1 || !strings.Contains(lines[0], message.id.String()) ||
			strings.Contains(lines[0], strings.Repeat("r", 1001)) {
			t.Fatalf("recorded reports: %d lines", len(lines))
		}
		if response, metadata := harness.metadata(
			message,
		); response.StatusCode != http.StatusOK ||
			metadata["state"] != "available" {
			t.Fatal("abuse report changed the message")
		}
	})
}

func TestAbuseReportHardening(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
		harness.mustCreate(message)
		link := core.FormatLink("https://nylzo.example", message.id, message.kdf, message.secret)
		_, fragment, _ := strings.Cut(link, "#")
		body := fmt.Sprintf(`{"id":%q,"reason":%q}`, message.id.String(), "malware at "+link+" and again "+fragment)
		for _, contentType := range []string{"", "text/plain", "application/x-www-form-urlencoded"} {
			response, _ := harness.do(
				&testRequest{
					method:  http.MethodPost,
					path:    "/api/v1/abuse",
					address: "203.0.113.80",
					body:    []byte(body),
					token:   message.readToken,
					headers: map[string]string{"Content-Type": contentType},
				},
			)
			if response.StatusCode != http.StatusBadRequest {
				t.Fatalf("content type %q: %d", contentType, response.StatusCode)
			}
		}
		response, _ := harness.do(
			&testRequest{
				method:  http.MethodPost,
				path:    "/api/v1/abuse",
				address: "203.0.113.81",
				body:    []byte(body),
				token:   message.readToken,
				headers: map[string]string{"Content-Type": "application/json; charset=utf-8"},
			},
		)
		if response.StatusCode != http.StatusAccepted {
			t.Fatalf("json report: %d", response.StatusCode)
		}
		path := harness.abuseFile(time.Now())
		if truncateErr := os.Truncate(path, 64<<20); truncateErr != nil {
			t.Fatal(truncateErr)
		}
		response, _ = harness.do(
			&testRequest{
				method:  http.MethodPost,
				path:    "/api/v1/abuse",
				address: "203.0.113.82",
				body:    []byte(body),
				token:   message.readToken,
				headers: map[string]string{"Content-Type": "application/json"},
			},
		)
		info, err := os.Stat(path)
		if response.StatusCode != http.StatusServiceUnavailable || err != nil || info.Size() != 64<<20 ||
			!strings.Contains(harness.logs.String(), "abuse-file-full") {
			t.Fatalf(
				"a full report file must answer 503, log an error and not grow: %d %v %d",
				response.StatusCode,
				err,
				info.Size(),
			)
		}
	})
}

func TestAbuseReports(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		harness := newHarness(t, baseSettings(pepperA, nil))
		message := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
		harness.mustCreate(message)
		report := func(id, token string) int {
			response, _ := harness.do(
				&testRequest{
					method:  http.MethodPost,
					path:    "/api/v1/abuse",
					address: "203.0.113.77",
					body:    []byte(fmt.Sprintf(`{"id":%q,"reason":"spam"}`, id)),
					token:   token,
					headers: map[string]string{"Content-Type": "application/json"},
				},
			)
			return response.StatusCode
		}
		if report(message.id.String(), message.readToken) != http.StatusAccepted {
			t.Fatal("valid report")
		}
		for range 4 {
			report(message.id.String(), core.Encode(make([]byte, 32)))
		}
		if report(message.id.String(), message.readToken) != http.StatusTooManyRequests {
			t.Fatal("sixth report per hour must be limited")
		}
	})
}

func reportAbuse(t *testing.T, harness *testHarness, token, address string, report map[string]any) string {
	t.Helper()
	body, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	response, answer := harness.do(
		&testRequest{
			method:  http.MethodPost,
			path:    "/api/v1/abuse",
			address: address,
			body:    body,
			token:   token,
			headers: map[string]string{"Content-Type": "application/json"},
		},
	)
	var fields struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if decodeErr := json.Unmarshal(answer, &fields); decodeErr != nil {
		t.Fatalf("abuse answer %d %q: %v", response.StatusCode, answer, decodeErr)
	}
	return fmt.Sprintf("%d %s%s", response.StatusCode, fields.Status, fields.Error)
}

func TestAbuseReportsRefuseAnImplausibleContactAndStoreAValidOneAsGiven(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		settings := baseSettings(pepperA, nil)
		settings.Rates.AbusePerHour = 1
		harness := newHarness(t, settings)
		message := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
		harness.mustCreate(message)
		link := core.FormatLink("https://nylzo.example", message.id, message.kdf, message.secret)
		for _, id := range []string{message.id.String(), core.NewID().String(), "not-an-id"} {
			for _, contact := range []string{"jana", "jana@example", "ja na@example.cz", link + "@example.cz"} {
				report := map[string]any{"id": id, "reason": "spam", "contact": contact, "good_faith": true}
				if got := reportAbuse(t, harness, message.readToken, "203.0.113.90", report); got != "400 contact" {
					t.Fatalf("contact %q about %s: %s", contact, id, got)
				}
			}
		}
		contact := "abcdefghijklmnopqrstuvwxyz@example.cz"
		report := map[string]any{
			"id":         message.id.String(),
			"reason":     "spam",
			"contact":    " " + contact + "\n",
			"good_faith": true,
		}
		if got := reportAbuse(t, harness, message.readToken, "203.0.113.90", report); got != "202 accepted" {
			t.Fatalf("a refused contact spent the rate limit or the valid report failed: %s", got)
		}
		data, err := harness.abuseReports()
		var stored map[string]any
		if err != nil || json.Unmarshal(data, &stored) != nil || stored["contact"] != contact ||
			stored["good_faith"] != true {
			t.Fatalf("the report must keep the trimmed contact without redaction and the good faith: %s %v", data, err)
		}
	})
}

func TestAbuseReportsAnswerTheLimitOnlyForALiveMessage(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		settings := baseSettings(pepperA, nil)
		settings.Rates.AbusePerHour = 0
		harness := newHarness(t, settings)
		live := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
		burned := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
		harness.mustCreate(live)
		harness.mustCreate(burned)
		for _, message := range []*testMessage{live, burned} {
			for range 3 {
				if got := reportAbuse(
					t,
					harness,
					message.readToken,
					"203.0.113.90",
					map[string]any{"id": message.id.String()},
				); got != "202 accepted" {
					t.Fatalf("a report within the daily budget: %s", got)
				}
			}
		}
		if got := reportAbuse(
			t,
			harness,
			live.readToken,
			"203.0.113.90",
			map[string]any{"id": live.id.String()},
		); got != "202 limit" {
			t.Fatalf("a fourth report about a live message must not read as received: %s", got)
		}
		if response, _ := harness.post(burned, "burn", burned.readToken); response.StatusCode != http.StatusNoContent {
			t.Fatalf("burn: %d", response.StatusCode)
		}
		wrongKey := core.Encode(make([]byte, 32))
		for name, request := range map[string]struct {
			token string
			id    string
		}{
			"a wrong key":    {wrongKey, live.id.String()},
			"an ended id":    {burned.readToken, burned.id.String()},
			"an unknown id":  {live.readToken, core.NewID().String()},
			"a malformed id": {live.readToken, "not-an-id"},
		} {
			if got := reportAbuse(
				t,
				harness,
				request.token,
				"203.0.113.90",
				map[string]any{"id": request.id},
			); got != "202 accepted" {
				t.Errorf("%s must keep the uniform answer: %s", name, got)
			}
		}
		data, err := harness.abuseReports()
		if err != nil || strings.Count(string(data), "\n") != 6 {
			t.Fatalf("stored reports: %q %v", data, err)
		}
	})
}

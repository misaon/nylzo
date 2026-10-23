package server_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/store"
)

func TestConcurrentCreateWithSameID(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	for i := range 20 {
		m := newMessage(t, core.KDFNone, "", 1, 3600, "race")
		m.ip = fmt.Sprintf("198.51.100.%d", i+1)
		var wg sync.WaitGroup
		codes := make([]int, 2)
		for j := range codes {
			wg.Go(func() { codes[j] = h.statusFrom(createRequest(m)) })
		}
		wg.Wait()
		if !slices.Equal(slices.Sorted(slices.Values(codes)), []int{201, 409}) {
			t.Fatalf("iteration %d: %v", i, codes)
		}
		resp, object := h.claim(m, "", "")
		if resp.StatusCode != 200 {
			t.Fatalf("iteration %d: claim after the race: %d", i, resp.StatusCode)
		}
		if _, err := h.openObject(m, object, resp.Header.Get("X-Nylzo-Policy")); err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
	}
}

func TestTruncatedCreateStoresNothing(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 3600, "short")
	conn, err := net.Dial("tcp", h.listen())
	if err != nil {
		t.Fatal(err)
	}
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprintf(conn, "POST /api/v1/messages HTTP/1.1\r\nHost: nylzo\r\nContent-Length: %d\r\nX-Nylzo-Create: %s\r\n\r\n", len(m.object), h.withIDToken(m.createHeader(0)))
	conn.Write(m.object[:100])
	conn.(*net.TCPConn).CloseWrite()
	line, _ := bufio.NewReader(conn).ReadString('\n')
	conn.Close()
	if !strings.Contains(line, " 400 ") {
		t.Fatalf("status line %q", line)
	}
	if h.blobExists(m.id) {
		t.Fatal("truncated body stored")
	}
	if resp, _ := h.metadata(m); resp.StatusCode != 404 {
		t.Fatal("truncated body created a row")
	}
}

func TestBlobLossWithoutEndingIsAStorageFault(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	for _, opens := range []uint16{1, 2} {
		m := newMessage(t, core.KDFNone, "", opens, 3600, "x")
		h.mustCreate(m)
		if err := h.blobs.Delete(t.Context(), m.id.String()); err != nil {
			t.Fatal(err)
		}
		resp, _ := h.claim(m, "", "")
		if resp.StatusCode != 500 || resp.Header.Get("X-Nylzo-Lease") != "" {
			t.Fatalf("opens %d: a lost blob must answer 500 before any open is spent: %d", opens, resp.StatusCode)
		}
		h.clock.Advance(store.LeaseDuration + time.Minute)
		if _, err := h.srv.SweepOnce(t.Context()); err != nil {
			t.Fatal(err)
		}
		if resp, meta := h.metadata(m); resp.StatusCode != 200 || meta["state"] != "available" || meta["opens_remaining"] != float64(opens) {
			t.Fatalf("opens %d: a lost blob must not spend an open or end the message: %v", opens, meta)
		}
	}
}

func TestInterruptedEndingIsFinishedOnAccess(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	crash := func(m *message, state store.State) {
		if _, _, _, err := h.store.MarkEnding(context.Background(), m.id.String(), state, h.clock.Now()); err != nil {
			t.Error(err)
		}
		if err := h.blobs.Delete(context.Background(), m.id.String()); err != nil {
			t.Error(err)
		}
	}
	claimed := newMessage(t, core.KDFNone, "", 2, 3600, "x")
	h.mustCreate(claimed)
	crash(claimed, store.StateCancelled)
	resp, body := h.claim(claimed, "", "")
	if resp.StatusCode != 410 || resp.Header.Get("X-Nylzo-Lease") != "" || decodeJSON(t, body)["state"] != "cancelled" {
		t.Fatalf("claim after an interrupted cancel: %d %s", resp.StatusCode, body)
	}
	if _, st := h.status(claimed); st["state"] != "cancelled" {
		t.Fatalf("status: %v", st)
	}
	downloaded := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	h.mustCreate(downloaded)
	resp, _ = h.claim(downloaded, "", "")
	lease := resp.Header.Get("X-Nylzo-Lease")
	if resp.StatusCode != 200 || lease == "" {
		t.Fatalf("claim: %d", resp.StatusCode)
	}
	inner := h.store
	h.store = &racingStore{Store: inner, race: func(id string) {
		if id == downloaded.id.String() {
			crash(downloaded, store.StateDeleted)
		}
	}}
	h.reopen(h.cfg)
	resp, body = h.blob(downloaded, lease)
	if resp.StatusCode != 410 || decodeJSON(t, body)["state"] != "deleted" {
		t.Fatalf("download racing a burn: %d %s", resp.StatusCode, body)
	}
	if rec, err := inner.Get(context.Background(), downloaded.id.String()); err != nil || rec.Tombstone == nil || rec.Tombstone.State != store.StateDeleted {
		t.Fatalf("the download must finish the interrupted burn: %+v %v", rec, err)
	}
}

func TestClaimKeepsLeaseWhenTheStreamFails(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	fb := h.withFaultyBlobs()
	m := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	h.mustCreate(m)
	fb.set(func(b *faultyBlobs) { b.failRead = errInjected })
	req, err := http.NewRequest("POST", h.ts.URL+"/api/v1/messages/"+m.id.String()+"/claim", nil)
	if err != nil {
		t.Fatal(err)
	}
	h.sign(req, m.readToken, "", nil)
	req.Header.Set("X-Forwarded-For", m.ip)
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	lease := resp.Header.Get("X-Nylzo-Lease")
	if resp.StatusCode != 200 || lease == "" || readErr == nil {
		t.Fatalf("a stream that fails after the claim must still carry the lease: %d %q %v", resp.StatusCode, lease, readErr)
	}
	if !strings.Contains(h.logBuf.String(), `"msg":"blob-stream"`) {
		t.Fatalf("a failed stream must be logged: %s", h.logBuf.String())
	}
	for line := range strings.SplitSeq(h.logBuf.String(), "\n") {
		var entry map[string]any
		if json.Unmarshal([]byte(line), &entry) != nil || entry["msg"] != "blob-stream" {
			continue
		}
		delete(entry, "time")
		delete(entry, "level")
		delete(entry, "msg")
		if len(entry) != 0 {
			t.Fatalf("the stream warning carries more than its name: %s", line)
		}
	}
	fb.set(func(b *faultyBlobs) { b.failRead = nil })
	resp, object := h.blob(m, lease)
	if resp.StatusCode != 200 || !bytes.Equal(object, m.object) {
		t.Fatalf("retry through the lease: %d", resp.StatusCode)
	}
}

func TestCreateStorageFailureAnswers500(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	fb := h.withFaultyBlobs()
	fb.set(func(b *faultyBlobs) { b.failPut = errInjected })
	m := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	if resp, _ := h.create(m); resp.StatusCode != 500 {
		t.Fatalf("storage failure: %d", resp.StatusCode)
	}
	if resp, _ := h.metadata(m); resp.StatusCode != 404 {
		t.Fatal("a failed Put created a row")
	}
}

type racingStore struct {
	store.Store
	once sync.Once
	race func(id string)
}

func (s *racingStore) Get(ctx context.Context, id string) (store.Record, error) {
	rec, err := s.Store.Get(ctx, id)
	if err == nil && rec.Message != nil {
		s.once.Do(func() { s.race(id) })
	}
	return rec, err
}

func TestBurnRacingCancelReportsTheRealState(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	h.mustCreate(m)
	inner := h.store
	h.store = &racingStore{Store: inner, race: func(id string) {
		if err := inner.End(context.Background(), id, store.StateCancelled, h.clock.Now()); err != nil {
			t.Error(err)
		}
	}}
	h.reopen(h.cfg)
	resp, body := h.post(m, "burn", m.readToken)
	if resp.StatusCode != 410 || decodeJSON(t, body)["state"] != "cancelled" {
		t.Fatalf("burn that lost the race: %d %s", resp.StatusCode, body)
	}
}

func TestStorageConflictMatchesEarlyConflict(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	existing := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	h.mustCreate(existing)
	early, earlyBody := h.create(existing)
	fb := h.withFaultyBlobs()
	fb.set(func(b *faultyBlobs) { b.failPut = store.ErrConflict })
	late, lateBody := h.create(newMessage(t, core.KDFNone, "", 1, 3600, "x"))
	if early.StatusCode != 409 || late.StatusCode != 409 || late.Close != early.Close || !bytes.Equal(earlyBody, lateBody) {
		t.Fatalf("early %d close=%v, late %d close=%v", early.StatusCode, early.Close, late.StatusCode, late.Close)
	}
}

func TestStorageCancellationIsInternalWhileClientWaits(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	fb := h.withFaultyBlobs()
	fb.set(func(b *faultyBlobs) { b.failPut = context.Canceled })
	if resp, _ := h.create(newMessage(t, core.KDFNone, "", 1, 3600, "x")); resp.StatusCode != 500 {
		t.Fatalf("storage cancellation with a live client: %d", resp.StatusCode)
	}
}

func TestBurnFinishesRecordedCancel(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	h.mustCreate(m)
	inner := h.store
	h.store = &racingStore{Store: inner, race: func(id string) {
		if _, _, _, err := inner.MarkEnding(context.Background(), id, store.StateCancelled, h.clock.Now()); err != nil {
			t.Error(err)
		}
	}}
	h.reopen(h.cfg)
	resp, body := h.post(m, "burn", m.readToken)
	if resp.StatusCode != 410 || decodeJSON(t, body)["state"] != "cancelled" {
		t.Fatalf("burn after a recorded cancel: %d %s", resp.StatusCode, body)
	}
	if _, st := h.status(m); st["state"] != "cancelled" {
		t.Fatalf("status: %v", st)
	}
}

func TestStorageFaultKeepsUniformNotFound(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	fb := h.withFaultyBlobs()
	m := newMessage(t, core.KDFNone, "", 2, 3600, "x")
	h.mustCreate(m)
	if _, _, _, err := h.store.MarkEnding(t.Context(), m.id.String(), store.StateCancelled, h.clock.Now()); err != nil {
		t.Fatal(err)
	}
	fb.set(func(b *faultyBlobs) { b.failDelete[m.id.String()] = true })
	unknown, unknownBody := h.do(request{method: "GET", path: "/api/v1/messages/" + core.NewID().String(), token: m.readToken})
	wrong, wrongBody := h.do(request{method: "GET", path: "/api/v1/messages/" + m.id.String(), token: core.Encode(make([]byte, 32))})
	if unknown.StatusCode != 404 || wrong.StatusCode != 404 || !bytes.Equal(unknownBody, wrongBody) {
		t.Fatalf("a storage fault must not change the 404 for a wrong token: %d %d", unknown.StatusCode, wrong.StatusCode)
	}
	resp, body := h.claim(m, "", "")
	if resp.StatusCode != 410 || decodeJSON(t, body)["state"] != "cancelled" {
		t.Fatalf("claim on a recorded cancel whose delete keeps failing: %d %s", resp.StatusCode, body)
	}
	if _, meta := h.metadata(m); meta["state"] != "cancelled" {
		t.Fatalf("metadata: %v", meta)
	}
}

func TestClaimFindsBlobRemovedByCommittedEnding(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	fb := h.withFaultyBlobs()
	m := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	h.mustCreate(m)
	inner := h.store
	fb.set(func(b *faultyBlobs) {
		b.onOpen = func(ref string) {
			if _, err := store.EndMessage(context.Background(), inner, b.Blobs, ref, store.StateDeleted, h.clock.Now()); err != nil {
				t.Error(err)
			}
		}
	})
	resp, body := h.claim(m, "", "")
	if resp.StatusCode != 410 || resp.Header.Get("X-Nylzo-Lease") != "" || decodeJSON(t, body)["state"] != "deleted" {
		t.Fatalf("claim racing a committed burn: %d %s", resp.StatusCode, body)
	}
}

type ackRacingStore struct {
	store.Store
	race func(id string)
}

func (s *ackRacingStore) Ack(ctx context.Context, l store.Lease, now time.Time) (string, error) {
	s.race(l.ID)
	return s.Store.Ack(ctx, l, now)
}

func TestAckAfterAConcurrentBurnAnswersGone(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 2, 3600, "x")
	h.mustCreate(m)
	resp, _ := h.claim(m, "", "")
	lease := resp.Header.Get("X-Nylzo-Lease")
	if resp.StatusCode != 200 || lease == "" {
		t.Fatalf("claim: %d", resp.StatusCode)
	}
	inner, blobs := h.store, h.blobs
	h.store = &ackRacingStore{Store: inner, race: func(id string) {
		if _, err := store.EndMessage(context.Background(), inner, blobs, id, store.StateDeleted, h.clock.Now()); err != nil {
			t.Error(err)
		}
	}}
	h.reopen(h.cfg)
	resp, body := h.do(request{method: "POST", path: "/api/v1/messages/" + m.id.String() + "/ack", ip: m.ip, token: m.readToken, headers: map[string]string{"X-Nylzo-Lease": lease}})
	if resp.StatusCode != 410 || decodeJSON(t, body)["state"] != "deleted" {
		t.Fatalf("ack after a burn removed its lease: %d %s", resp.StatusCode, body)
	}
}

type failingEndStore struct {
	store.Store
}

func (s failingEndStore) End(ctx context.Context, id string, state store.State, now time.Time) error {
	return errInjected
}

func TestUnfinishableEndingReportsRecordedState(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 2, 3600, "x")
	h.mustCreate(m)
	inner := h.store
	h.store = &racingStore{Store: failingEndStore{Store: inner}, race: func(id string) {
		if _, _, _, err := inner.MarkEnding(context.Background(), id, store.StateCancelled, h.clock.Now()); err != nil {
			t.Error(err)
		}
		if err := h.blobs.Delete(context.Background(), id); err != nil {
			t.Error(err)
		}
	}}
	h.reopen(h.cfg)
	resp, body := h.claim(m, "", "")
	if resp.StatusCode != 410 || decodeJSON(t, body)["state"] != "cancelled" {
		t.Fatalf("an ending that cannot finish must still report its recorded state: %d %s", resp.StatusCode, body)
	}
}

type endingThenFailingGet struct {
	store.Store
	armed atomic.Bool
}

func (s *endingThenFailingGet) MarkEnding(ctx context.Context, id string, state store.State, now time.Time) (store.State, string, bool, error) {
	if err := s.End(ctx, id, store.StateCancelled, now); err != nil {
		return "", "", false, err
	}
	s.armed.Store(true)
	return s.Store.MarkEnding(ctx, id, state, now)
}

func (s *endingThenFailingGet) Get(ctx context.Context, id string) (store.Record, error) {
	if s.armed.Load() {
		return store.Record{}, errInjected
	}
	return s.Store.Get(ctx, id)
}

func TestBurnWithUnreadableEndAnswers500(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	h.mustCreate(m)
	h.store = &endingThenFailingGet{Store: h.store}
	h.reopen(h.cfg)
	if resp, body := h.post(m, "burn", m.readToken); resp.StatusCode != 500 {
		t.Fatalf("a burn that cannot read the final state must not guess it: %d %s", resp.StatusCode, body)
	}
}

type countingReleaseStore struct {
	store.Store
	releases atomic.Int32
}

func (s *countingReleaseStore) ReleaseIfDone(ctx context.Context, id string, now time.Time) (string, error) {
	s.releases.Add(1)
	return s.Store.ReleaseIfDone(ctx, id, now)
}

func TestRequestDuringTheLeaseTakesNoWriteLock(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	h.mustCreate(m)
	if resp, _ := h.claim(m, "", ""); resp.StatusCode != 200 {
		t.Fatalf("claim: %d", resp.StatusCode)
	}
	counting := &countingReleaseStore{Store: h.store}
	h.store = counting
	h.reopen(h.cfg)
	wrong := *m
	wrong.readToken = core.Encode(core.NewSecret()[:])
	h.metadata(m)
	h.metadata(&wrong)
	if n := counting.releases.Load(); n != 0 {
		t.Fatalf("requests during the lease tried to release the blob %d times", n)
	}
}

func TestExpiredMessageWithStuckDeleteReportsItsEnd(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	fb := h.withFaultyBlobs()
	unspent := newMessage(t, core.KDFNone, "", 2, 60, "x")
	spent := newMessage(t, core.KDFNone, "", 1, 3600, "y")
	h.mustCreate(unspent)
	h.mustCreate(spent)
	if resp, _ := h.claim(spent, "", ""); resp.StatusCode != 200 {
		t.Fatalf("claim: %d", resp.StatusCode)
	}
	fb.set(func(b *faultyBlobs) {
		b.failDelete[unspent.id.String()] = true
		b.failDelete[spent.id.String()] = true
	})
	h.clock.Advance(time.Hour)
	if _, meta := h.metadata(unspent); meta["state"] != "expired" {
		t.Fatalf("unspent: %v", meta)
	}
	if _, meta := h.metadata(spent); meta["state"] != "opened" {
		t.Fatalf("spent: %v", meta)
	}
	if resp, body := h.claim(unspent, "", ""); resp.StatusCode != 410 || decodeJSON(t, body)["state"] != "expired" {
		t.Fatalf("claim: %d %s", resp.StatusCode, body)
	}
}

func TestClaimOpenErrorSpendsNoOpen(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	fb := h.withFaultyBlobs()
	m := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	h.mustCreate(m)
	fb.set(func(b *faultyBlobs) { b.failOpen = errInjected })
	resp, _ := h.claim(m, "", "")
	if resp.StatusCode != 500 || resp.Header.Get("X-Nylzo-Lease") != "" {
		t.Fatalf("a blob open error before the claim: %d lease=%q", resp.StatusCode, resp.Header.Get("X-Nylzo-Lease"))
	}
	fb.set(func(b *faultyBlobs) { b.failOpen = nil })
	if _, meta := h.metadata(m); meta["opens_remaining"] != float64(1) {
		t.Fatalf("an open error spent an open: %v", meta)
	}
}

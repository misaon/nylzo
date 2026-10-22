package server_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/store"
)

type lifecycleKind struct {
	name   string
	opens  uint16
	ttl    uint32
	ending store.State
	after  time.Duration
	opened bool
	expect [11]string
}

var lifecycleActions = [11]string{"burn", "cancel", "expiry by request", "expiry by sweep", "release", "resume by request", "resume by sweep", "claim", "lease download", "blob vanished", "ack"}

func recordedRow(state store.State) [11]string {
	s := string(state)
	return [11]string{"410 " + s + " tomb:" + s, "410 " + s + " tomb:" + s, "200 " + s + " tomb:" + s, "- - tomb:" + s, "- - live:" + s,
		"200 " + s + " tomb:" + s, "- - tomb:" + s, "410 " + s + " tomb:" + s, "410 " + s + " tomb:" + s, "410 " + s + " tomb:" + s, "410 " + s + " tomb:" + s}
}

func expiredRow(state store.State) [11]string {
	row := recordedRow(state)
	row[4] = "- - live:"
	return row
}

func openedRow() [11]string {
	row := recordedRow(store.StateOpened)
	row[4] = "- - tomb:opened"
	return row
}

var lifecycleKinds = []lifecycleKind{
	{name: "unspent", opens: 2, ttl: 3600, expect: [11]string{"204 - tomb:deleted", "204 - tomb:cancelled", "200 expired tomb:expired", "- - tomb:expired", "- - live:",
		"200 available live:", "- - live:", "200 - live:", "200 - live:", "500 - live:", "204 - live:"}},
	{name: "spent during the lease", opens: 1, ttl: 3600, expect: [11]string{"204 - tomb:deleted", "204 - tomb:cancelled", "200 opened tomb:opened", "- - tomb:opened", "- - live:",
		"200 opened live:", "- - live:", "410 opened live:", "200 - live:", "500 - live:", "204 - tomb:opened"}},
	{name: "spent after the lease", opens: 1, ttl: 3600, after: store.LeaseDuration + store.LeaseTimerSlack, expect: openedRow()},
	{name: "unspent and expired", opens: 2, ttl: 60, after: 61 * time.Second, expect: expiredRow(store.StateExpired)},
	{name: "spent and expired", opens: 1, ttl: 60, after: 61 * time.Second, expect: expiredRow(store.StateOpened)},
	{name: "recorded deleted", opens: 2, ttl: 3600, ending: store.StateDeleted, expect: recordedRow(store.StateDeleted)},
	{name: "recorded cancelled", opens: 2, ttl: 3600, ending: store.StateCancelled, expect: recordedRow(store.StateCancelled)},
	{name: "recorded opened", opens: 1, ttl: 3600, after: store.LeaseDuration + store.LeaseTimerSlack, opened: true, expect: openedRow()},
	{name: "recorded expired", opens: 2, ttl: 3600, ending: store.StateExpired, expect: recordedRow(store.StateExpired)},
}

func TestLifecycleMatrix(t *testing.T) {
	for _, kind := range lifecycleKinds {
		for i, action := range lifecycleActions {
			t.Run(kind.name+"/"+action, func(t *testing.T) {
				t.Parallel()
				h := newHarness(t, baseConfig(pepperA, nil))
				m, lease := setupLifecycleKind(t, h, kind)
				resp, body := runLifecycleAction(t, h, m, lease, action)
				checkLifecycleCell(t, h, m, action, kind.expect[i], resp, body)
			})
		}
	}
}

func setupLifecycleKind(t *testing.T, h *harness, kind lifecycleKind) (*message, string) {
	t.Helper()
	m := newMessage(t, core.KDFNone, "", kind.opens, kind.ttl, "x")
	h.mustCreate(m)
	resp, _ := h.claim(m, "", "")
	lease := resp.Header.Get("X-Nylzo-Lease")
	if resp.StatusCode != http.StatusOK || lease == "" {
		t.Fatalf("claim: %d", resp.StatusCode)
	}
	h.clock.Advance(kind.after)
	ctx := t.Context()
	if kind.ending != "" {
		if _, _, _, err := h.store.MarkEnding(ctx, m.id.String(), kind.ending, h.clock.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if kind.opened {
		if ref, err := h.store.ReleaseIfDone(ctx, m.id.String(), h.clock.Now()); err != nil || ref == "" {
			t.Fatalf("record opened: %q %v", ref, err)
		}
	}
	return m, lease
}

func runLifecycleAction(t *testing.T, h *harness, m *message, lease, action string) (*http.Response, []byte) {
	t.Helper()
	ctx := t.Context()
	switch action {
	case "burn":
		return h.post(m, "burn", m.readToken)
	case "cancel":
		return h.post(m, "cancel", m.manageTok)
	case "expiry by request":
		h.clock.Advance(2 * time.Hour)
		return h.do(request{method: "GET", path: "/api/v1/messages/" + m.id.String(), ip: m.ip, headers: map[string]string{"Authorization": "Bearer " + m.readToken}})
	case "expiry by sweep":
		h.clock.Advance(2 * time.Hour)
		if _, err := h.srv.SweepOnce(ctx); err != nil {
			t.Fatal(err)
		}
	case "release":
		if _, err := store.Release(ctx, h.store, h.blobs, m.id.String(), h.clock.Now()); err != nil {
			t.Fatal(err)
		}
	case "resume by request":
		return h.do(request{method: "GET", path: "/api/v1/messages/" + m.id.String() + "/status", ip: m.ip, headers: map[string]string{"Authorization": "Bearer " + m.manageTok}})
	case "resume by sweep":
		if _, err := h.srv.SweepOnce(ctx); err != nil {
			t.Fatal(err)
		}
	case "claim":
		return h.claim(m, "", "")
	case "lease download":
		return h.blob(m, lease)
	case "blob vanished":
		if err := h.blobs.Delete(ctx, m.id.String()); err != nil {
			t.Fatal(err)
		}
		return h.claim(m, "", "")
	case "ack":
		return h.do(request{method: "POST", path: "/api/v1/messages/" + m.id.String() + "/ack", ip: m.ip, headers: map[string]string{"Authorization": "Bearer " + m.readToken, "X-Nylzo-Lease": lease}})
	}
	return nil, nil
}

func checkLifecycleCell(t *testing.T, h *harness, m *message, action, expect string, resp *http.Response, body []byte) {
	t.Helper()
	parts := strings.Fields(expect)
	code, state, final := parts[0], parts[1], parts[2]
	if code != "-" && (resp == nil || strconv.Itoa(resp.StatusCode) != code) {
		t.Fatalf("response %v, want %s (%s)", resp.StatusCode, code, body)
	}
	if state != "-" && decodeJSON(t, body)["state"] != state {
		t.Fatalf("state %s, want %s", body, state)
	}
	if code == "410" || code == "500" {
		if lease := resp.Header.Get("X-Nylzo-Lease"); lease != "" && action == "claim" {
			t.Fatalf("a refused claim handed out a lease")
		}
	}
	rec, err := h.store.Get(t.Context(), m.id.String())
	if err != nil {
		t.Fatal(err)
	}
	kind, want, _ := strings.Cut(final, ":")
	switch kind {
	case "tomb":
		if rec.Tombstone == nil || string(rec.Tombstone.State) != want {
			t.Fatalf("row %+v %+v, want tombstone %s", rec.Message, rec.Tombstone, want)
		}
		if h.blobExists(m.id) {
			t.Fatal("an ended message kept its blob")
		}
	case "live":
		if rec.Message == nil || string(rec.Message.Ending) != want {
			t.Fatalf("row %+v %+v, want a live row with ending %q", rec.Message, rec.Tombstone, want)
		}
		if exists := h.blobExists(m.id); exists != (action != "blob vanished") {
			t.Fatalf("blob present %v on a live row", exists)
		}
	}
}

func TestVanishedBlobDuringLeaseIsAStorageFault(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	h.mustCreate(m)
	resp, _ := h.claim(m, "", "")
	lease := resp.Header.Get("X-Nylzo-Lease")
	if err := h.blobs.Delete(t.Context(), m.id.String()); err != nil {
		t.Fatal(err)
	}
	if resp, _ := h.blob(m, lease); resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("download of a vanished blob: %d", resp.StatusCode)
	}
	if rec, err := h.store.Get(t.Context(), m.id.String()); err != nil || rec.Message == nil || rec.Message.Ending != "" {
		t.Fatalf("the row must stay live without an ending: %+v %v", rec, err)
	}
}

func TestExpiryRacingADownloadAnswersGone(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	fb := h.withFaultyBlobs()
	m := newMessage(t, core.KDFNone, "", 2, 60, "x")
	h.mustCreate(m)
	resp, _ := h.claim(m, "", "")
	lease := resp.Header.Get("X-Nylzo-Lease")
	var once sync.Once
	fb.set(func(b *faultyBlobs) {
		b.onOpen = func(ref string) {
			once.Do(func() {
				h.clock.Advance(61 * time.Second)
				if err := b.Blobs.Delete(context.Background(), ref); err != nil {
					t.Error(err)
				}
			})
		}
	})
	resp, body := h.blob(m, lease)
	if resp.StatusCode != http.StatusGone || decodeJSON(t, body)["state"] != "expired" {
		t.Fatalf("a download racing the expiry ending: %d %s", resp.StatusCode, body)
	}
	if rec, err := h.store.Get(t.Context(), m.id.String()); err != nil || rec.Tombstone == nil || rec.Tombstone.State != store.StateExpired {
		t.Fatalf("the download must finish the expiry: %+v %v", rec, err)
	}
}

type slowPutBlobs struct {
	store.Blobs
	clock *fakeClock
	delay time.Duration
}

func (b slowPutBlobs) Put(ctx context.Context, ref string, r io.Reader, size int64) error {
	b.clock.Advance(b.delay)
	return b.Blobs.Put(ctx, ref, r, size)
}

func TestExpiryCountsFromTheStoredUpload(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	h.blobs = slowPutBlobs{Blobs: h.blobs, clock: h.clock, delay: 90 * time.Second}
	h.reopen(h.cfg)
	m := newMessage(t, core.KDFNone, "", 1, 60, "x")
	h.mustCreate(m)
	if resp, meta := h.metadata(m); resp.StatusCode != 200 || meta["state"] != "available" {
		t.Fatalf("a slow upload must not use up the TTL: %d %v", resp.StatusCode, meta)
	}
}

func TestClaimReadsItsBodyBeforeOpeningTheBlob(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	fb := h.withFaultyBlobs()
	m := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	h.mustCreate(m)
	var opens atomic.Int32
	fb.set(func(b *faultyBlobs) { b.onOpen = func(string) { opens.Add(1) } })
	resp, _ := h.do(request{method: "POST", path: "/api/v1/messages/" + m.id.String() + "/claim", ip: m.ip, body: []byte("{"), headers: map[string]string{"Authorization": "Bearer " + m.readToken, "Content-Type": "application/json"}})
	if resp.StatusCode != http.StatusBadRequest || opens.Load() != 0 {
		t.Fatalf("a malformed claim body must be refused before the blob is opened: %d, %d opens", resp.StatusCode, opens.Load())
	}
}

func TestTruncatedBlobSpendsNoOpen(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	h.mustCreate(m)
	short := m.object[:len(m.object)-1]
	if err := h.blobs.Delete(t.Context(), m.id.String()); err != nil {
		t.Fatal(err)
	}
	if err := h.blobs.Put(t.Context(), m.id.String(), bytes.NewReader(short), int64(len(short))); err != nil {
		t.Fatal(err)
	}
	resp, _ := h.claim(m, "", "")
	if resp.StatusCode != http.StatusInternalServerError || resp.Header.Get("X-Nylzo-Lease") != "" {
		t.Fatalf("a blob of the wrong size must answer 500 before an open is spent: %d", resp.StatusCode)
	}
	if _, meta := h.metadata(m); meta["opens_remaining"] != float64(1) {
		t.Fatalf("a truncated blob spent an open: %v", meta)
	}
}

func TestHeadOnBlobSpendsNoDownload(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 2, 3600, "x")
	h.mustCreate(m)
	resp, _ := h.claim(m, "", "")
	lease := resp.Header.Get("X-Nylzo-Lease")
	head, _ := h.do(request{method: "HEAD", path: "/api/v1/messages/" + m.id.String() + "/blob", ip: m.ip, headers: map[string]string{"Authorization": "Bearer " + m.readToken, "X-Nylzo-Lease": lease}})
	if head.StatusCode != http.StatusNotFound {
		t.Fatalf("HEAD on /blob: %d", head.StatusCode)
	}
	for i := range store.MaxLeaseDownload {
		if resp, _ := h.blob(m, lease); resp.StatusCode != http.StatusOK {
			t.Fatalf("download %d after a HEAD: %d", i+1, resp.StatusCode)
		}
	}
}

func TestMalformedAbuseReportsSpendNoRateLimit(t *testing.T) {
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.AbusePerHour = 1
	h := newHarness(t, cfg)
	m := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
	h.mustCreate(m)
	report := func(id string) int {
		body, _ := json.Marshal(map[string]string{"id": id, "read_token": m.readToken, "reason": "spam"})
		resp, _ := h.do(request{method: "POST", path: "/api/v1/abuse", ip: "203.0.113.44", body: body, headers: map[string]string{"Content-Type": "application/json"}})
		return resp.StatusCode
	}
	for range 3 {
		if code := report("not-an-id"); code != http.StatusAccepted {
			t.Fatalf("malformed id: %d", code)
		}
	}
	if code := report(m.id.String()); code != http.StatusAccepted {
		t.Fatalf("a valid report after malformed ones was rate limited: %d", code)
	}
	if data, err := os.ReadFile(filepath.Join(h.dir, "abuse-reports.jsonl")); err != nil || !strings.Contains(string(data), m.id.String()) {
		t.Fatalf("the valid report was not stored: %v", err)
	}
}

type leaseCount struct {
	store.Store
	n *atomic.Int32
}

func (s leaseCount) Leases(ctx context.Context, id string) ([]store.Lease, error) {
	s.n.Add(1)
	return s.Store.Leases(ctx, id)
}

func TestDownloadReadsLeasesOnce(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	n := &atomic.Int32{}
	h.store = leaseCount{h.store, n}
	h.reopen(h.cfg)
	m := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	h.mustCreate(m)
	resp, _ := h.claim(m, "", "")
	n.Store(0)
	if resp, _ := h.blob(m, resp.Header.Get("X-Nylzo-Lease")); resp.StatusCode != http.StatusOK {
		t.Fatalf("download: %d", resp.StatusCode)
	}
	if got := n.Load(); got != 1 {
		t.Fatalf("a download read the leases %d times", got)
	}
}

type burnAfterClaim struct {
	store.Store
	blobs store.Blobs
}

func (s burnAfterClaim) Claim(ctx context.Context, p store.ClaimParams) (store.ClaimResult, error) {
	res, err := s.Store.Claim(ctx, p)
	if err == nil {
		if _, err := store.EndMessage(ctx, s.Store, s.blobs, p.ID, store.StateDeleted, p.Clock()); err != nil {
			return res, err
		}
	}
	return res, err
}

func TestClaimStreamsTheBlobItOpenedWhenABurnFollows(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 2, 3600, "x")
	h.mustCreate(m)
	h.store = burnAfterClaim{Store: h.store, blobs: h.blobs}
	h.reopen(h.cfg)
	resp, object := h.claim(m, "", "")
	if resp.StatusCode != http.StatusOK || resp.Header.Get("X-Nylzo-Lease") == "" || !bytes.Equal(object, m.object) {
		t.Fatalf("a claim that spent an open must stream the blob it opened: %d %d bytes", resp.StatusCode, len(object))
	}
}

func TestExpiryRacingADownloadWithAStuckEndAnswersExpired(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	fb := h.withFaultyBlobs()
	m := newMessage(t, core.KDFNone, "", 2, 60, "x")
	h.mustCreate(m)
	resp, _ := h.claim(m, "", "")
	lease := resp.Header.Get("X-Nylzo-Lease")
	h.store = failingEndStore{Store: h.store}
	h.reopen(h.cfg)
	var once sync.Once
	fb.set(func(b *faultyBlobs) {
		b.onOpen = func(ref string) {
			once.Do(func() {
				h.clock.Advance(61 * time.Second)
				if err := b.Blobs.Delete(context.Background(), ref); err != nil {
					t.Error(err)
				}
			})
		}
	})
	resp, body := h.blob(m, lease)
	if resp.StatusCode != http.StatusGone || decodeJSON(t, body)["state"] != "expired" {
		t.Fatalf("an expiry that cannot finish must still report expired: %d %s", resp.StatusCode, body)
	}
}

func TestReleaseAfterLastLeaseKeepsUnspentMessage(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 2, 3600, "x")
	h.mustCreate(m)
	resp, _ := h.claim(m, "", "")
	if ack := h.ack(m, resp.Header.Get("X-Nylzo-Lease")); ack.StatusCode != http.StatusNoContent {
		t.Fatalf("ack: %d", ack.StatusCode)
	}
	h.clock.Advance(store.LeaseDuration + store.LeaseTimerSlack)
	if released, err := store.Release(t.Context(), h.store, h.blobs, m.id.String(), h.clock.Now()); err != nil || released {
		t.Fatalf("release: %v %v", released, err)
	}
	rec, err := h.store.Get(t.Context(), m.id.String())
	if err != nil || rec.Message == nil || rec.Message.Ending != "" || rec.Message.OpensRemaining != 1 || !h.blobExists(m.id) {
		t.Fatalf("row %+v %v", rec.Message, err)
	}
}

func TestReleaseAfterLastLeaseKeepsRecordedExpiry(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 2, 3600, "x")
	h.mustCreate(m)
	if resp, _ := h.claim(m, "", ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("claim: %d", resp.StatusCode)
	}
	if _, _, _, err := h.store.MarkEnding(t.Context(), m.id.String(), store.StateExpired, h.clock.Now()); err != nil {
		t.Fatal(err)
	}
	h.clock.Advance(store.LeaseDuration + store.LeaseTimerSlack)
	if released, err := store.Release(t.Context(), h.store, h.blobs, m.id.String(), h.clock.Now()); err != nil || released {
		t.Fatalf("release: %v %v", released, err)
	}
	rec, err := h.store.Get(t.Context(), m.id.String())
	if err != nil || rec.Message == nil || rec.Message.Ending != store.StateExpired || rec.Message.OpensRemaining != 1 || !h.blobExists(m.id) {
		t.Fatalf("row %+v %v", rec.Message, err)
	}
}

func TestDownloadAfterAFinishedConcurrentBurnReportsDeleted(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 2, 3600, "x")
	h.mustCreate(m)
	resp, _ := h.claim(m, "", "")
	lease := resp.Header.Get("X-Nylzo-Lease")
	inner := h.store
	h.store = &racingStore{Store: inner, race: func(id string) {
		if _, err := store.EndMessage(context.Background(), inner, h.blobs, id, store.StateDeleted, h.clock.Now()); err != nil {
			t.Error(err)
		}
	}}
	h.reopen(h.cfg)
	for _, action := range []string{"lease download", "ack"} {
		resp, body := runLifecycleAction(t, h, m, lease, action)
		if resp.StatusCode != http.StatusGone || decodeJSON(t, body)["state"] != "deleted" {
			t.Fatalf("%s after a finished concurrent burn: %d %s", action, resp.StatusCode, body)
		}
	}
}

func TestAckAfterARecordedBurnFinishesIt(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 2, 3600, "x")
	h.mustCreate(m)
	resp, _ := h.claim(m, "", "")
	lease := resp.Header.Get("X-Nylzo-Lease")
	inner := h.store
	h.store = &racingStore{Store: inner, race: func(id string) {
		if _, _, _, err := inner.MarkEnding(context.Background(), id, store.StateDeleted, h.clock.Now()); err != nil {
			t.Error(err)
		}
	}}
	h.reopen(h.cfg)
	resp, body := runLifecycleAction(t, h, m, lease, "ack")
	if resp.StatusCode != http.StatusGone || decodeJSON(t, body)["state"] != "deleted" {
		t.Fatalf("an ack after a recorded burn: %d %s", resp.StatusCode, body)
	}
	if rec, err := inner.Get(t.Context(), m.id.String()); err != nil || rec.Tombstone == nil || rec.Tombstone.State != store.StateDeleted || h.blobExists(m.id) {
		t.Fatalf("the ack must finish the recorded burn: %+v %v", rec, err)
	}
}

type slowWriterStore struct {
	store.Store
	clock *fakeClock
	wait  time.Duration
}

func (s slowWriterStore) Claim(ctx context.Context, p store.ClaimParams) (store.ClaimResult, error) {
	s.clock.Advance(s.wait)
	return s.Store.Claim(ctx, p)
}

func TestClaimThatWaitsPastExpiryIsRefused(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 60, "x")
	h.mustCreate(m)
	h.clock.Advance(59 * time.Second)
	h.store = slowWriterStore{Store: h.store, clock: h.clock, wait: 2 * time.Second}
	h.reopen(h.cfg)
	resp, body := h.claim(m, "", "")
	if resp.StatusCode != http.StatusGone || decodeJSON(t, body)["state"] != "expired" || resp.Header.Get("X-Nylzo-Lease") != "" {
		t.Fatalf("a claim that reached the store after expires_at must be refused: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
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
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	h.mustCreate(m)
	if resp, _ := h.claim(m, "", ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("claim: %d", resp.StatusCode)
	}
	raw := h.blobs
	h.blobs = &deleteHookBlobs{Blobs: raw, hook: func() {
		h.clock.Advance(store.LeaseDuration)
		if _, err := store.Release(context.Background(), h.store, raw, m.id.String(), h.clock.Now()); err != nil {
			t.Error(err)
		}
	}}
	h.reopen(h.cfg)
	if resp, body := h.post(m, "burn", m.readToken); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("the burn that recorded the first ending must answer 204 when a release finishes it: %d %s", resp.StatusCode, body)
	}
	if rec, err := h.store.Get(t.Context(), m.id.String()); err != nil || rec.Tombstone == nil || rec.Tombstone.State != store.StateDeleted {
		t.Fatalf("tombstone: %+v %v", rec, err)
	}
}

func abuseReport(t *testing.T, h *harness, m *message, ip string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"id": m.id.String(), "read_token": m.readToken, "reason": "spam"})
	if resp, _ := h.do(request{method: "POST", path: "/api/v1/abuse", ip: ip, body: body, headers: map[string]string{"Content-Type": "application/json"}}); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("report: %d", resp.StatusCode)
	}
}

func TestAbuseReportsKeepOnlyTheDay(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
	h.mustCreate(m)
	h.clock.Advance(3*time.Hour + 17*time.Minute + 5*time.Second)
	abuseReport(t, h, m, "203.0.113.44")
	data, err := os.ReadFile(filepath.Join(h.dir, "abuse-reports.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var line map[string]string
	if err := json.Unmarshal(bytes.TrimSpace(data), &line); err != nil || line["date"] != "2026-10-22" || line["id"] != m.id.String() || len(line) != 3 {
		t.Fatalf("a stored report must carry only the day, the id and the reason: %s %v", data, err)
	}
}

func TestOldAbuseReportsAreDropped(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
	h.mustCreate(m)
	path := filepath.Join(h.dir, "abuse-reports.jsonl")
	old := `{"date":"2026-10-01","id":"AAAAAAAAAAAAAAAAAAAAAA","reason":"old"}` + "\n" + `{"id":"AAAAAAAAAAAAAAAAAAAAAQ","reason":"older format","time":"2026-10-02T10:00:00Z"}` + "\n"
	recent := `{"date":"2026-10-21","id":"AAAAAAAAAAAAAAAAAAAAAg","reason":"recent"}` + "\n"
	if err := os.WriteFile(path, []byte(old+recent), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := h.srv.SweepOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != recent {
		t.Fatalf("the sweep must drop reports older than 14 days: %q %v", data, err)
	}
	if err := os.WriteFile(path, []byte(old+recent), 0o600); err != nil {
		t.Fatal(err)
	}
	abuseReport(t, h, m, "203.0.113.45")
	data, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(data), "old") || !strings.HasPrefix(string(data), recent) {
		t.Fatalf("a new report must drop reports older than 14 days: %q %v", data, err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("report file mode: %v %v", info, err)
	}
}

func TestStoredTimesAreWholeMinutes(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	h.clock.Advance(17*time.Second + 123*time.Millisecond)
	m := newMessage(t, core.KDFNone, "", 2, 3600, "minute")
	resp, body := h.create(m)
	if resp.StatusCode != http.StatusCreated || decodeJSON(t, body)["expires_at"] != "2026-10-22T11:01:00Z" {
		t.Fatalf("expires_at must be rounded up to the full minute: %d %s", resp.StatusCode, body)
	}
	if resp, _ := h.claim(m, "", ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("claim: %d", resp.StatusCode)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(h.dir, "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var created, expires, lease int64
	if err := db.QueryRow(`SELECT m.created_at, m.expires_at, l.lease_expires_at FROM messages m JOIN leases l ON l.id = m.id WHERE m.id = ?`, m.id.String()).Scan(&created, &expires, &lease); err != nil {
		t.Fatal(err)
	}
	minute := time.Minute.Milliseconds()
	if created%minute != 0 || expires%minute != 0 || lease%minute != 0 {
		t.Fatalf("stored times must be whole minutes: created %d, expires %d, lease %d", created, expires, lease)
	}
	if created != time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC).UnixMilli() || lease < h.clock.Now().Add(store.LeaseDuration).UnixMilli() {
		t.Fatalf("created_at rounds down, the lease rounds up: created %d, lease %d", created, lease)
	}
}

package server_test

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
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
	expect [10]string
}

var lifecycleActions = [10]string{"burn", "cancel", "expiry by request", "expiry by sweep", "release", "resume by request", "resume by sweep", "claim", "lease download", "blob vanished"}

func recordedRow(state store.State) [10]string {
	s := string(state)
	return [10]string{"410 " + s + " tomb:" + s, "410 " + s + " tomb:" + s, "200 " + s + " tomb:" + s, "- - tomb:" + s, "- - live:" + s,
		"200 " + s + " tomb:" + s, "- - tomb:" + s, "410 " + s + " tomb:" + s, "410 " + s + " tomb:" + s, "410 " + s + " tomb:" + s}
}

func expiredRow(state store.State) [10]string {
	row := recordedRow(state)
	row[4] = "- - live:"
	return row
}

func openedRow() [10]string {
	row := recordedRow(store.StateOpened)
	row[4] = "- - tomb:opened"
	return row
}

var lifecycleKinds = []lifecycleKind{
	{name: "unspent", opens: 2, ttl: 3600, expect: [10]string{"204 - tomb:deleted", "204 - tomb:cancelled", "200 expired tomb:expired", "- - tomb:expired", "- - live:",
		"200 available live:", "- - live:", "200 - live:", "200 - live:", "500 - live:"}},
	{name: "spent during the lease", opens: 1, ttl: 3600, expect: [10]string{"204 - tomb:deleted", "204 - tomb:cancelled", "200 opened tomb:opened", "- - tomb:opened", "- - live:",
		"200 opened live:", "- - live:", "410 opened live:", "200 - live:", "500 - live:"}},
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
		if _, _, err := h.store.MarkEnding(ctx, m.id.String(), kind.ending, h.clock.Now()); err != nil {
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

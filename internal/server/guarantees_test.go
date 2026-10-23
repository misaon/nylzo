package server_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/store"
)

func TestReplayAfterRestartIsRefused(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 2, 3600, "replay after restart")
	h.mustCreate(m)
	path := "/api/v1/messages/" + m.id.String()
	claim := signedRequest(&m.link.R, core.PurposeRead, "POST", path+"/claim", h.srv.ChallengeForTest(), nil)
	status := signedRequest(m.manage, core.PurposeManage, "GET", path+"/status", h.srv.ChallengeForTest(), nil)
	for _, req := range []request{claim, status} {
		if resp, _ := h.do(req); resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: %d", req.path, resp.StatusCode)
		}
	}
	h.reopen(h.cfg)
	for _, req := range []request{claim, status} {
		if resp, _ := h.do(req); resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s replayed against a fresh process: %d", req.path, resp.StatusCode)
		}
	}
	if _, st := h.status(m); st["opens_used"] != 1.0 {
		t.Fatalf("the replay spent an open: %v", st)
	}
}

func TestDownloadAfterExpiryWithAStuckDeleteIsGone(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	fb := h.withFaultyBlobs()
	m := newMessage(t, core.KDFNone, "", 2, 60, "expires under an active lease")
	h.mustCreate(m)
	resp, _ := h.claim(m, "", "")
	lease := resp.Header.Get("X-Nylzo-Lease")
	if resp.StatusCode != http.StatusOK || lease == "" {
		t.Fatalf("claim: %d", resp.StatusCode)
	}
	fb.set(func(b *faultyBlobs) { b.failDelete[m.id.String()] = true })
	h.clock.Advance(61 * time.Second)
	if resp, body := h.blob(m, lease); resp.StatusCode != http.StatusGone || decodeJSON(t, body)["state"] != "expired" {
		t.Fatalf("a download after the expiry while the blob cannot be deleted: %d %s", resp.StatusCode, body)
	}
	if code := h.ack(m, lease).StatusCode; code != http.StatusGone {
		t.Fatalf("an ack after the expiry while the blob cannot be deleted: %d", code)
	}
}

func TestFirstOpenedAtKeepsTheFirstClaim(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 2, 3600, "opened twice")
	h.mustCreate(m)
	if resp, _ := h.claim(m, "", ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("first claim: %d", resp.StatusCode)
	}
	_, first := h.status(m)
	h.clock.Advance(2 * time.Minute)
	resp, _ := h.claim(m, "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("second claim: %d", resp.StatusCode)
	}
	_, second := h.status(m)
	if first["first_opened_at"] == nil || second["first_opened_at"] != first["first_opened_at"] {
		t.Fatalf("first_opened_at moved from %v to %v", first["first_opened_at"], second["first_opened_at"])
	}
	if code := h.ack(m, resp.Header.Get("X-Nylzo-Lease")).StatusCode; code != http.StatusNoContent {
		t.Fatalf("ack: %d", code)
	}
	if _, ended := h.status(m); ended["first_opened_at"] != first["first_opened_at"] {
		t.Fatalf("the tombstone moved first_opened_at from %v to %v", first["first_opened_at"], ended["first_opened_at"])
	}
}

func TestBlobCarriesThePolicyOfTheClaim(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 3600, "resume")
	h.mustCreate(m)
	resp, _ := h.claim(m, "", "")
	policy := resp.Header.Get("X-Nylzo-Policy")
	if resp.StatusCode != http.StatusOK || policy == "" {
		t.Fatalf("claim: %d policy=%q", resp.StatusCode, policy)
	}
	again, object := h.blob(m, resp.Header.Get("X-Nylzo-Lease"))
	if again.StatusCode != http.StatusOK || again.Header.Get("X-Nylzo-Policy") != policy {
		t.Fatalf("blob: %d policy=%q, want %q", again.StatusCode, again.Header.Get("X-Nylzo-Policy"), policy)
	}
	if _, err := h.openObject(m, object, again.Header.Get("X-Nylzo-Policy")); err != nil {
		t.Fatalf("the blob does not open with its own policy: %v", err)
	}
}

type failingCreateStore struct{ store.Store }

func (failingCreateStore) Create(context.Context, *store.Message, ...store.RateHit) error {
	return errInjected
}

func TestFailedRowWriteRemovesTheUploadedBlob(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	h.store = failingCreateStore{Store: h.store}
	h.reopen(h.cfg)
	m := newMessage(t, core.KDFNone, "", 1, 3600, "orphan")
	if resp, _ := h.create(m); resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("create with a failing row write: %d", resp.StatusCode)
	}
	if h.blobExists(m.id) {
		t.Fatal("the ciphertext of a create that stored no row stays in the blob store")
	}
}

func TestAckRefusesAnExpiredLeaseWhileOpensRemain(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 2, 3600, "ack")
	h.mustCreate(m)
	resp, _ := h.claim(m, "", "")
	lease := resp.Header.Get("X-Nylzo-Lease")
	if resp.StatusCode != http.StatusOK || lease == "" {
		t.Fatalf("claim: %d", resp.StatusCode)
	}
	h.clock.Advance(store.LeaseDuration + time.Minute)
	if code := h.ack(m, lease).StatusCode; code != http.StatusGone {
		t.Fatalf("an ack after the lease ended: %d", code)
	}
	if resp, _ := h.blob(m, lease); resp.StatusCode != http.StatusGone {
		t.Fatalf("a download after the lease ended: %d", resp.StatusCode)
	}
}

func TestDailyCreateLimits(t *testing.T) {
	t.Parallel()
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.CreatePerHour = 1000
	cfg.Rates.CreatePerDay = 2
	h := newHarness(t, cfg)
	create := func(ip string) int {
		m := newMessage(t, core.KDFNone, "", 1, 3600, "n")
		m.ip = ip
		resp, _ := h.create(m)
		return resp.StatusCode
	}
	for range 2 {
		if code := create("203.0.113.1"); code != http.StatusCreated {
			t.Fatalf("create within the daily limit: %d", code)
		}
	}
	if code := create("203.0.113.1"); code != http.StatusTooManyRequests {
		t.Fatalf("a third create in one day from one /32: %d", code)
	}
	for i := 2; i <= 5; i++ {
		for range 2 {
			if code := create(fmt.Sprintf("203.0.113.%d", i)); code != http.StatusCreated {
				t.Fatalf("create from 203.0.113.%d: %d", i, code)
			}
		}
	}
	if code := create("203.0.113.6"); code != http.StatusTooManyRequests {
		t.Fatalf("a create past the daily count of its /24: %d", code)
	}
	if code := create("198.51.100.1"); code != http.StatusCreated {
		t.Fatalf("another /24 must keep its own budget: %d", code)
	}
}

func TestRangeDailyBytesLimit(t *testing.T) {
	t.Parallel()
	cfg := baseConfig(pepperA, nil)
	h := newHarness(t, cfg)
	probe := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	cfg.Rates.CreateBytesPerDay = int64(2*len(probe.object) + 1)
	h.reopen(cfg)
	for source := 1; source <= 5; source++ {
		for range 2 {
			m := newMessage(t, core.KDFNone, "", 1, 3600, "n")
			m.ip = fmt.Sprintf("203.0.113.%d", source)
			h.mustCreate(m)
		}
	}
	m := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	m.ip = "203.0.113.6"
	if resp, _ := h.create(m); resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("a create past the daily bytes of its /24: %d", resp.StatusCode)
	}
}

func TestAbuseReportFilesArePrivate(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
	h.mustCreate(m)
	abuseReport(t, h, m, "203.0.113.45")
	for _, path := range []string{filepath.Dir(h.abuseFile(h.clock.Now())), h.abuseFile(h.clock.Now())} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("%s is readable by other users: %v", filepath.Base(path), info.Mode().Perm())
		}
	}
}

package server_test

import (
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/store"
)

func TestAK1CreateCarriesNoSecrets(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFPBKDF2, "tajná fráze", 1, 3600, "plaintext secret body")
	h.mustCreate(m)
	captured := h.lastCreateRequest()
	if !strings.Contains(string(captured), "X-Nylzo-Create: "+m.createHeader(0)) || !strings.Contains(string(captured), string(m.object)) {
		t.Fatal("wire capture missing the create request")
	}
	p, err := core.DerivePassphraseKey(m.kdf, "tajná fráze", &m.link.SaltPW)
	if err != nil {
		t.Fatal(err)
	}
	secrets := map[string][]byte{
		"S": m.secret[:], "K": m.content.K[:], "P": p, "R": m.link.R[:], "V": m.content.V[:], "M": m.manage[:],
	}
	for name, b := range secrets {
		for _, enc := range []string{core.Encode(b), hex.EncodeToString(b)} {
			if strings.Contains(string(captured), enc) {
				t.Fatalf("create request carries %s", name)
			}
		}
		if name != "R" && name != "V" && name != "M" && contains(captured, b) {
			t.Fatalf("create request carries raw %s", name)
		}
	}
	if strings.Contains(string(captured), "plaintext secret body") || strings.Contains(string(captured), "tajná fráze") {
		t.Fatal("create request carries plaintext or passphrase")
	}
}

func contains(haystack, needle []byte) bool {
	return len(needle) > 0 && strings.Contains(string(haystack), string(needle))
}

func TestFlowCreateMetadataClaimBlobAck(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 86400, "hello")
	h.mustCreate(m)
	for _, ua := range []string{"", "Slackbot-LinkExpanding 1.0"} {
		h.do(request{method: "GET", path: "/m/" + m.id.String(), headers: map[string]string{"User-Agent": ua}})
	}
	for i := 0; i < 5; i++ {
		resp, meta := h.metadata(m)
		if resp.StatusCode != 200 || meta["opens_remaining"].(float64) != 1 || meta["state"] != "available" || meta["passphrase_required"] != false || meta["kdf_id"].(float64) != 0 {
			t.Fatalf("metadata %d: %v", resp.StatusCode, meta)
		}
	}
	resp, object := h.claim(m, "", "")
	if resp.StatusCode != 200 {
		t.Fatalf("claim: %d %s", resp.StatusCode, object)
	}
	lease := resp.Header.Get("X-Nylzo-Lease")
	policy := resp.Header.Get("X-Nylzo-Policy")
	if lease == "" || policy == "" || resp.Header.Get("Content-Type") != "application/octet-stream" {
		t.Fatal("claim response headers")
	}
	payload, err := h.openObject(m, object, policy)
	if err != nil || payload.Meta.Text == nil || *payload.Meta.Text != "hello" {
		t.Fatalf("decrypt: %v", err)
	}
	pol, err := decodePolicy(policy)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []core.Policy{{OpensMax: pol.OpensMax + 1, TTLSeconds: pol.TTLSeconds}, {OpensMax: pol.OpensMax, TTLSeconds: pol.TTLSeconds - 1}} {
		if _, _, err := core.Open(&m.content.K, m.id, bad, m.kdf, object); !errors.Is(err, core.ErrIntegrity) {
			t.Fatalf("changed policy %+v: want ErrIntegrity, got %v", bad, err)
		}
	}
	resp, meta := h.metadata(m)
	if meta["state"] != "opened" || meta["opens_remaining"].(float64) != 0 || meta["first_opened_at"] == nil {
		t.Fatalf("metadata after claim: %v", meta)
	}
	for i := 0; i < 3; i++ {
		resp, again := h.blob(m, lease)
		if resp.StatusCode != 200 || string(again) != string(object) {
			t.Fatalf("blob %d: %d", i, resp.StatusCode)
		}
	}
	if resp, _ := h.blob(m, lease); resp.StatusCode != 429 {
		t.Fatalf("fourth download: %d", resp.StatusCode)
	}
	if resp, _ := h.blob(m, core.Encode(make([]byte, 32))); resp.StatusCode != 410 {
		t.Fatalf("bogus lease: %d", resp.StatusCode)
	}
	if h.pendingTimers() != 1 {
		t.Fatal("lease timer not scheduled")
	}
	if resp := h.ack(m, lease); resp.StatusCode != 204 {
		t.Fatalf("ack: %d", resp.StatusCode)
	}
	if h.blobExists(m.id) {
		t.Fatal("blob survived the last ack")
	}
	if resp, _ := h.blob(m, lease); resp.StatusCode != 410 {
		t.Fatalf("blob after ack: %d", resp.StatusCode)
	}
	resp, meta = h.metadata(m)
	if resp.StatusCode != 200 || meta["state"] != "opened" || meta["ended_at"] == nil {
		t.Fatalf("tombstone metadata: %v", meta)
	}
	if resp, _ := h.claim(m, "", ""); resp.StatusCode != 410 {
		t.Fatalf("claim after end: %d", resp.StatusCode)
	}
	resp, st := h.status(m)
	if resp.StatusCode != 200 || st["state"] != "opened" {
		t.Fatalf("status: %v", st)
	}
	h.clock.Advance(store.LeaseDuration + 2*time.Second)
	h.fireTimers()
}

func TestAK4ConcurrentClaimsHTTP(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	cfg := h.cfg
	cfg.Rates.ReadPerHour = 100000
	cfg.Rates.CreatePerHour = 100000
	cfg.Rates.CreatePerDay = 100000
	cfg.Rates.BucketPerSecond = 100000
	cfg.Rates.BucketBurst = 100000
	h.reopen(cfg)
	for i := 0; i < 100; i++ {
		m := newMessage(t, core.KDFNone, "", 1, 3600, "race")
		h.mustCreate(m)
		var wg sync.WaitGroup
		codes := make([]int, 2)
		for j := 0; j < 2; j++ {
			wg.Add(1)
			go func(j int) {
				defer wg.Done()
				resp, _ := h.claim(m, "", "")
				codes[j] = resp.StatusCode
			}(j)
		}
		wg.Wait()
		if !((codes[0] == 200 && codes[1] == 410) || (codes[0] == 410 && codes[1] == 200)) {
			t.Fatalf("iteration %d: %v", i, codes)
		}
	}
}

func TestAK5PassphrasePolicyHTTP(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	cfg := h.cfg
	cfg.Rates.ReadPerHour = 100000
	h.reopen(cfg)
	m := newMessage(t, core.KDFPBKDF2, "correct", 2, 86400, "guarded")
	m.ip = "203.0.113.10"
	h.mustCreate(m)
	wrong := core.Encode(make([]byte, 32))
	expect := func(ip string, retry float64, left float64) {
		t.Helper()
		resp, body := h.claim(m, wrong, ip)
		j := decodeJSON(t, body)
		if resp.StatusCode != 403 || j["error"] != "passphrase" || j["retry_after"].(float64) != retry || j["attempts_left"].(float64) != left {
			t.Fatalf("got %d %s, want retry %v left %v", resp.StatusCode, body, retry, left)
		}
	}
	for i := 1; i <= 5; i++ {
		expect("203.0.113.10", 0, float64(5-i))
	}
	expect("203.0.113.10", 2, 0)
	h.clock.Advance(2 * time.Second)
	expect("203.0.113.10", 4, 0)
	h.clock.Advance(4 * time.Second)
	expect("203.0.113.10", 8, 0)
	h.clock.Advance(8 * time.Second)
	expect("203.0.113.10", 16, 0)
	h.clock.Advance(16 * time.Second)
	expect("203.0.113.10", 32, 0)
	resp, meta := h.metadata(m)
	if meta["opens_remaining"].(float64) != 2 {
		t.Fatalf("wrong passphrase consumed an open: %v", meta)
	}
	resp, _ = h.claim(m, m.verifier, "203.0.113.99")
	if resp.StatusCode != 200 {
		t.Fatalf("correct passphrase from another address: %d", resp.StatusCode)
	}
	_, st := h.status(m)
	if st["failed_passphrase_attempts"].(float64) != 10 || st["opens_used"].(float64) != 1 {
		t.Fatalf("status: %v", st)
	}
	for i := 0; i < 90; i++ {
		ip := "198.51.100." + strconv.Itoa(i%250)
		resp, _ := h.claim(m, wrong, ip)
		if resp.StatusCode != 403 {
			t.Fatalf("flood attempt %d: %d", i, resp.StatusCode)
		}
	}
	resp, body := h.claim(m, m.verifier, "192.0.2.77")
	j := decodeJSON(t, body)
	if resp.StatusCode != 403 || j["retry_after"].(float64) <= 0 {
		t.Fatalf("daily cap must lock every address: %d %s", resp.StatusCode, body)
	}
	_, st = h.status(m)
	if st["failed_passphrase_attempts"].(float64) != 100 {
		t.Fatalf("status after cap: %v", st)
	}
	fresh := newMessage(t, core.KDFPBKDF2, "pw", 1, 3600, "x")
	h.mustCreate(fresh)
	resp, body = h.claim(fresh, "", "192.0.2.78")
	j = decodeJSON(t, body)
	if resp.StatusCode != 403 || j["attempts_left"].(float64) != 4 || j["retry_after"].(float64) != 0 {
		t.Fatalf("missing verifier: %d %s", resp.StatusCode, body)
	}
	if _, st := h.status(fresh); st["failed_passphrase_attempts"].(float64) != 1 {
		t.Fatalf("status after missing verifier: %v", st)
	}
}

func TestAK6LeaseExpiryWithoutAck(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	cfg := h.cfg
	cfg.Rates.ReadPerHour = 100000
	h.reopen(cfg)
	m := newMessage(t, core.KDFNone, "", 3, 86400, "three")
	h.mustCreate(m)
	var leases []string
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, _ := h.claim(m, "", "")
			if resp.StatusCode != 200 {
				t.Errorf("claim: %d", resp.StatusCode)
				return
			}
			mu.Lock()
			leases = append(leases, resp.Header.Get("X-Nylzo-Lease"))
			mu.Unlock()
		}()
	}
	wg.Wait()
	distinct := map[string]bool{}
	for _, l := range leases {
		distinct[l] = true
	}
	if len(distinct) != 3 {
		t.Fatalf("leases not distinct: %v", leases)
	}
	h.clock.Advance(store.LeaseDuration - time.Second)
	for _, l := range leases {
		if resp, _ := h.blob(m, l); resp.StatusCode != 200 {
			t.Fatalf("blob within lease: %d", resp.StatusCode)
		}
	}
	h.clock.Advance(time.Second)
	if !h.blobExists(m.id) {
		t.Fatal("blob deleted before any request, timer or sweep")
	}
	h.clock.Advance(store.LeaseTimerSlack)
	if fired := h.fireTimers(); fired != 1 {
		t.Fatalf("timers fired: %d", fired)
	}
	if h.blobExists(m.id) {
		t.Fatal("timer did not delete the blob")
	}
	for _, l := range leases {
		if resp, _ := h.blob(m, l); resp.StatusCode != 410 {
			t.Fatalf("blob after lease expiry: %d", resp.StatusCode)
		}
	}
	_, meta := h.metadata(m)
	if meta["state"] != "opened" {
		t.Fatalf("metadata: %v", meta)
	}
}

func TestAK6RequestAfterLeaseExpiryReleasesBlob(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 86400, "one")
	h.mustCreate(m)
	resp, _ := h.claim(m, "", "")
	if resp.StatusCode != 200 {
		t.Fatal("claim")
	}
	lease := resp.Header.Get("X-Nylzo-Lease")
	h.clock.Advance(store.LeaseDuration)
	if resp, _ := h.blob(m, lease); resp.StatusCode != 410 {
		t.Fatalf("blob at expiry: %d", resp.StatusCode)
	}
	if h.blobExists(m.id) {
		t.Fatal("request after the last lease expired must release the blob")
	}
	if h.pendingTimers() != 1 {
		t.Fatal("timer should still be pending")
	}
	h.clock.Advance(store.LeaseTimerSlack)
	h.fireTimers()
}

func TestAK6SweepAfterRestart(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 86400, "restart")
	h.mustCreate(m)
	if resp, _ := h.claim(m, "", ""); resp.StatusCode != 200 {
		t.Fatal("claim")
	}
	h.reopen(h.cfg)
	h.clock.Advance(store.LeaseDuration + time.Second)
	if !h.blobExists(m.id) {
		t.Fatal("blob gone before sweep")
	}
	report, err := h.srv.SweepOnce(t.Context())
	if err != nil || report.BlobsReleased != 1 {
		t.Fatalf("sweep: %+v %v", report, err)
	}
	if h.blobExists(m.id) {
		t.Fatal("sweep left the blob")
	}
}

func TestAK7ExpiryLazyAndSweep(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 60, "short")
	h.mustCreate(m)
	h.clock.Advance(61 * time.Second)
	resp, meta := h.metadata(m)
	if resp.StatusCode != 200 || meta["state"] != "expired" {
		t.Fatalf("lazy expiry: %d %v", resp.StatusCode, meta)
	}
	if h.blobExists(m.id) {
		t.Fatal("expired blob kept")
	}
	if resp, _ := h.claim(m, "", ""); resp.StatusCode != 410 {
		t.Fatalf("claim expired: %d", resp.StatusCode)
	}
	m2 := newMessage(t, core.KDFNone, "", 1, 60, "swept")
	h.mustCreate(m2)
	h.clock.Advance(61 * time.Second)
	report, err := h.srv.SweepOnce(t.Context())
	if err != nil || report.Expired != 1 || h.blobExists(m2.id) {
		t.Fatalf("sweep: %+v %v", report, err)
	}
	h.clock.Advance(7 * 24 * time.Hour)
	if _, err := h.srv.SweepOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if resp, _ := h.metadata(m); resp.StatusCode != 404 {
		t.Fatalf("after tombstone purge: %d", resp.StatusCode)
	}
	if resp, _ := h.create(m); resp.StatusCode != 201 {
		t.Fatalf("id reusable after purge: %d", resp.StatusCode)
	}
}

func TestAK11LogsContainNoSecrets(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFScrypt, "log fráze", 1, 3600, "logged text")
	m.ip = "203.0.113.55"
	h.mustCreate(m)
	h.metadata(m)
	resp, _ := h.claim(m, m.verifier, "")
	lease := resp.Header.Get("X-Nylzo-Lease")
	h.blob(m, lease)
	h.ack(m, lease)
	h.status(m)
	h.do(request{method: "POST", path: "/api/v1/messages/" + m.id.String() + "/cancel", headers: map[string]string{"Authorization": "Bearer " + m.manageTok}})
	logs := h.logBuf.String()
	if !strings.Contains(logs, m.id.String()) {
		t.Fatal("log should contain the request path with the id")
	}
	leaseBytes, _ := core.DecodeExact(lease, 32)
	forbidden := map[string][]byte{"S": m.secret[:], "R": m.link.R[:], "V": m.content.V[:], "M": m.manage[:], "K": m.content.K[:], "L": leaseBytes}
	for name, b := range forbidden {
		if strings.Contains(logs, core.Encode(b)) || strings.Contains(logs, hex.EncodeToString(b)) {
			t.Fatalf("log contains %s", name)
		}
	}
	for _, needle := range []string{"Authorization", "Bearer", "203.0.113.55", "127.0.0.1", "log fráze", "logged text", "X-Nylzo"} {
		if strings.Contains(logs, needle) {
			t.Fatalf("log contains %q", needle)
		}
	}
}

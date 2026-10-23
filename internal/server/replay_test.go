package server_test

import (
	"database/sql"
	"encoding/json"
	"maps"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/server"
)

func TestRejectedRequestsCannotBeReplayed(t *testing.T) {
	const victim, observer = "203.0.113.10", "198.51.100.20"
	cases := []struct {
		name   string
		config func(cfg *server.Config)
		before func(h *harness, m *message)
		header map[string]string
		status int
	}{
		{"token bucket 429", func(cfg *server.Config) { cfg.Rates.SourcePerSecond, cfg.Rates.SourceBurst = 1e-9, 1 }, func(h *harness, m *message) {
			h.do(request{method: "GET", path: "/api/v1/instance", ip: victim})
		}, nil, http.StatusTooManyRequests},
		{"read counter 429", func(cfg *server.Config) { cfg.Rates.ReadPerHour = 1 }, func(h *harness, m *message) {
			h.do(signedRequestFrom(victim, &m.link.R, core.PurposeRead, "GET", "/api/v1/messages/"+m.id.String(), h.srv.ChallengeForTest()))
		}, nil, http.StatusTooManyRequests},
		{"prefetch 503", nil, nil, map[string]string{"Sec-Purpose": "prefetch"}, http.StatusServiceUnavailable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := baseConfig(pepperA, nil)
			if c.config != nil {
				c.config(&cfg)
			}
			h := newHarness(t, cfg)
			m := newMessage(t, core.KDFNone, "", 1, 3600, "recorded")
			m.ip = "192.0.2.30"
			h.mustCreate(m)
			if c.before != nil {
				c.before(h, m)
			}
			claim := signedRequestFrom(victim, &m.link.R, core.PurposeRead, "POST", "/api/v1/messages/"+m.id.String()+"/claim", h.srv.ChallengeForTest())
			maps.Copy(claim.headers, c.header)
			if resp, _ := h.do(claim); resp.StatusCode != c.status {
				t.Fatalf("the victim's claim: %d, want %d", resp.StatusCode, c.status)
			}
			for k := range c.header {
				delete(claim.headers, k)
			}
			claim.ip = observer
			if resp, _ := h.do(claim); resp.StatusCode != http.StatusNotFound {
				t.Fatalf("a recorded claim that the server turned away was accepted later: %d", resp.StatusCode)
			}
			if rec, err := h.store.Get(t.Context(), m.id.String()); err != nil || rec.Message == nil || rec.Message.OpensRemaining != 1 {
				t.Fatalf("the replay spent an open: %+v %v", rec.Message, err)
			}
		})
	}
}

func signedRequestFrom(ip string, seed *[core.TokenLen]byte, purpose, method, path string, challenge [core.ChallengeLen]byte) request {
	req := signedRequest(seed, purpose, method, path, challenge, nil)
	req.ip = ip
	return req
}

func TestDegenerateKeysAreRejectedAtCreate(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	identity := core.Encode(append([]byte{1}, make([]byte, 31)...))
	order4 := core.Encode(make([]byte, 32))
	nonCanonical := func() string {
		b := make([]byte, 32)
		for i := range b {
			b[i] = 0xff
		}
		b[0], b[31] = 0xee, 0x7f
		return core.Encode(b)
	}()
	none := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	pw := newMessage(t, core.KDFPBKDF2, "correct horse battery staple", 1, 3600, "x")
	for name, header := range map[string]string{
		"identity read key":      mutateHeader(t, none, func(f map[string]any) { f["read_key"] = identity }),
		"small-order manage key": mutateHeader(t, none, func(f map[string]any) { f["manage_key"] = order4 }),
		"non-canonical read key": mutateHeader(t, none, func(f map[string]any) { f["read_key"] = nonCanonical }),
		"identity verifier key":  mutateHeader(t, pw, func(f map[string]any) { f["verifier_key"] = identity }),
		"manage key equals read": mutateHeader(t, none, func(f map[string]any) { f["manage_key"] = f["read_key"] }),
		"verifier equals read":   mutateHeader(t, pw, func(f map[string]any) { f["verifier_key"] = f["read_key"] }),
		"verifier equals manage": mutateHeader(t, pw, func(f map[string]any) { f["verifier_key"] = f["manage_key"] }),
	} {
		m := none
		if strings.Contains(name, "verifier") {
			m = pw
		}
		resp, _ := h.do(request{method: "POST", path: "/api/v1/messages", body: m.object, headers: map[string]string{"Content-Type": "application/octet-stream", "X-Nylzo-Create": header}})
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: %d", name, resp.StatusCode)
		}
	}
	for _, m := range []*message{none, pw} {
		if _, err := h.store.Get(t.Context(), m.id.String()); err == nil {
			t.Fatalf("a rejected create stored %s", m.id)
		}
	}
	h.mustCreate(none)
}

func TestStoredSmallOrderKeysCannotSign(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	ring, err := pepper.NewRing(pepperA, nil)
	if err != nil {
		t.Fatal(err)
	}
	identity := [core.PublicKeyLen]byte{1}
	forge := func(purpose, method, path string, c [core.ChallengeLen]byte) core.Signature {
		sig := core.Signature{PublicKey: identity, Challenge: c, Sig: [core.SignatureLen]byte{1}}
		if !sig.Verify(purpose, method, path, nil) {
			t.Fatal("the forged signature does not verify, so the test proves nothing")
		}
		return sig
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(h.dir, "nylzo.db")+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	setMAC := func(m *message, column string, p pepper.Purpose) {
		mac, _ := ring.MAC(p, identity[:])
		if _, err := db.Exec(`UPDATE messages SET `+column+` = ? WHERE id = ?`, mac[:], m.id.String()); err != nil {
			t.Fatal(err)
		}
	}

	m := newMessage(t, core.KDFNone, "", 1, 3600, "stored before keys were checked")
	h.mustCreate(m)
	setMAC(m, "read_mac", pepper.Read)
	setMAC(m, "manage_mac", pepper.Manage)
	path := "/api/v1/messages/" + m.id.String()
	for name, req := range map[string]request{
		"metadata": rawAuth(request{method: "GET", path: path}, forge(core.PurposeRead, "GET", path, h.srv.ChallengeForTest()).Authorization()),
		"status":   rawAuth(request{method: "GET", path: path + "/status"}, forge(core.PurposeManage, "GET", path+"/status", h.srv.ChallengeForTest()).Authorization()),
		"burn":     rawAuth(request{method: "POST", path: path + "/burn"}, forge(core.PurposeRead, "POST", path+"/burn", h.srv.ChallengeForTest()).Authorization()),
	} {
		if resp, body := h.do(req); resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s signed with the identity key: %d %s", name, resp.StatusCode, body)
		}
	}

	const pass = "correct horse battery staple"
	pw := newMessage(t, core.KDFPBKDF2, pass, 1, 3600, "verifier stored before keys were checked")
	h.mustCreate(pw)
	setMAC(pw, "verifier_mac", pepper.Verifier)
	claimPath := "/api/v1/messages/" + pw.id.String() + "/claim"
	challenge := h.srv.ChallengeForTest()
	req := signedRequest(&pw.link.R, core.PurposeRead, "POST", claimPath, challenge, nil)
	req.headers["X-Nylzo-Verifier"] = forge(core.PurposeVerifier, "POST", claimPath, challenge).VerifierHeader()
	resp, body := h.do(req)
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(body), `"attempts_left":4`) {
		t.Fatalf("claim with the identity verifier key: %d %s", resp.StatusCode, body)
	}
	if _, meta := h.metadata(pw); meta["opens_remaining"] != float64(1) {
		t.Fatalf("the refused verifier consumed an open: %v", meta)
	}
}

func TestHeadOnSignedRoutesIsNotFound(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 3600, "head")
	h.mustCreate(m)
	path := "/api/v1/messages/" + m.id.String()
	for _, p := range []string{path, path + "/status"} {
		purpose, seed := core.PurposeRead, &m.link.R
		if strings.HasSuffix(p, "/status") {
			purpose, seed = core.PurposeManage, m.manage
		}
		req := signedRequest(seed, purpose, "HEAD", p, h.srv.ChallengeForTest(), nil)
		if resp, body := h.do(req); resp.StatusCode != http.StatusNotFound || len(body) != 0 {
			t.Fatalf("HEAD %s: %d %q", p, resp.StatusCode, body)
		}
	}
}

func TestOnlyAnAuthorizedRequestSettlesAMessage(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 60, "expires")
	h.mustCreate(m)
	h.clock.Advance(3 * time.Minute)
	path := "/api/v1/messages/" + m.id.String()
	if resp, _ := h.do(signedRequest(core.NewSecret(), core.PurposeRead, "GET", path, h.srv.ChallengeForTest(), nil)); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("wrong key: %d", resp.StatusCode)
	}
	if rec, err := h.store.Get(t.Context(), m.id.String()); err != nil || rec.Message == nil || !h.blobExists(m.id) {
		t.Fatal("a request with a wrong key ended the expired message, so it did more work than an unknown id")
	}
	if _, meta := h.metadata(m); meta["state"] != "expired" {
		t.Fatalf("authorized metadata: %v", meta)
	}
	if rec, err := h.store.Get(t.Context(), m.id.String()); err != nil || rec.Tombstone == nil || h.blobExists(m.id) {
		t.Fatal("the authorized request did not finish the expiry")
	}
}

func TestRequestLinesLogNoSize(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 3600, "logged")
	h.mustCreate(m)
	resp, _ := h.claim(m, "", "")
	if r, _ := h.blob(m, resp.Header.Get("X-Nylzo-Lease")); r.StatusCode != http.StatusOK {
		t.Fatalf("blob: %d", r.StatusCode)
	}
	h.metadata(m)
	h.status(m)
	seen := map[string]bool{}
	for line := range strings.SplitSeq(strings.TrimSpace(h.logBuf.String()), "\n") {
		var entry map[string]any
		if json.Unmarshal([]byte(line), &entry) != nil || entry["msg"] != "request" {
			continue
		}
		route, _ := entry["route"].(string)
		_, bytes := entry["bytes"]
		_, latency := entry["duration_ms"]
		object := route == "POST /api/v1/messages" || strings.HasSuffix(route, "/claim") || strings.HasSuffix(route, "/blob")
		if bytes {
			t.Fatalf("%s logs its response size: %s", route, line)
		}
		if object && latency {
			t.Fatalf("%s logs its latency: %s", route, line)
		}
		if !object && route != "" && !latency {
			t.Fatalf("%s lost its latency: %s", route, line)
		}
		seen[route] = true
	}
	for _, r := range []string{"POST /api/v1/messages", "POST /api/v1/messages/{id}/claim", "GET /api/v1/messages/{id}/blob", "GET /api/v1/messages/{id}", "GET /api/v1/messages/{id}/status"} {
		if !seen[r] {
			t.Fatalf("no request line for %s", r)
		}
	}
}

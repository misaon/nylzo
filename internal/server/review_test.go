package server_test

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server"
	"github.com/misaon/nylzo/internal/store"
)

func (h *harness) post(m *message, action, token string) (*http.Response, []byte) {
	h.t.Helper()
	return h.do(request{method: "POST", path: "/api/v1/messages/" + m.id.String() + "/" + action, headers: map[string]string{"Authorization": "Bearer " + token}})
}

func TestBurnAndCancelEndpoints(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	burn := newMessage(t, core.KDFNone, "", 1, 3600, "b")
	cancel := newMessage(t, core.KDFNone, "", 2, 3600, "c")
	h.mustCreate(burn)
	h.mustCreate(cancel)
	unknown, unknownBody := h.do(request{method: "POST", path: "/api/v1/messages/" + core.NewID().String() + "/burn", headers: map[string]string{"Authorization": "Bearer " + burn.readToken}})
	if unknown.StatusCode != 404 {
		t.Fatalf("unknown id: %d", unknown.StatusCode)
	}
	for _, c := range []struct {
		m             *message
		action, token string
	}{{burn, "burn", burn.manageTok}, {cancel, "cancel", cancel.readToken}} {
		resp, body := h.post(c.m, c.action, c.token)
		if resp.StatusCode != 404 || string(body) != string(unknownBody) {
			t.Fatalf("%s with the token of the other purpose: %d %s", c.action, resp.StatusCode, body)
		}
		if !h.blobExists(c.m.id) {
			t.Fatalf("%s with a wrong token removed the blob", c.action)
		}
	}
	if resp, _ := h.post(burn, "burn", burn.readToken); resp.StatusCode != 204 || h.blobExists(burn.id) {
		t.Fatalf("burn: %d", resp.StatusCode)
	}
	if _, meta := h.metadata(burn); meta["state"] != "deleted" {
		t.Fatalf("metadata after burn: %v", meta)
	}
	if resp, _ := h.claim(cancel, "", ""); resp.StatusCode != 200 {
		t.Fatal("claim before cancel")
	}
	if resp, _ := h.post(cancel, "cancel", cancel.manageTok); resp.StatusCode != 204 || h.blobExists(cancel.id) {
		t.Fatalf("cancel: %d", resp.StatusCode)
	}
	if _, st := h.status(cancel); st["state"] != "cancelled" || st["first_opened_at"] == nil {
		t.Fatalf("status after cancel: %v", st)
	}
	resp, body := h.post(burn, "burn", burn.readToken)
	if resp.StatusCode != 410 || decodeJSON(t, body)["state"] != "deleted" {
		t.Fatalf("second burn: %d %s", resp.StatusCode, body)
	}
	if resp, _ := h.post(cancel, "cancel", cancel.manageTok); resp.StatusCode != 410 {
		t.Fatalf("second cancel: %d", resp.StatusCode)
	}
}

func mutateHeader(t *testing.T, m *message, f func(map[string]any)) string {
	t.Helper()
	data, err := base64.RawURLEncoding.DecodeString(m.createHeader(0))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	f(fields)
	out, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return core.Encode(out)
}

func TestCreateHeaderRules(t *testing.T) {
	cfg := server.Defaults()
	none := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	pw := newMessage(t, core.KDFPBKDF2, "pw", 1, 3600, "x")
	if !server.ParseCreateHeaderForTest(cfg, none.createHeader(0)) || !server.ParseCreateHeaderForTest(cfg, pw.createHeader(0)) {
		t.Fatal("valid headers rejected")
	}
	raw, _ := base64.RawURLEncoding.DecodeString(none.createHeader(0))
	cases := map[string]string{
		"unknown-key":           mutateHeader(t, none, func(f map[string]any) { f["extra"] = 1 }),
		"case-variant-key":      mutateHeader(t, none, func(f map[string]any) { f["ID"] = f["id"]; delete(f, "id") }),
		"verifier-with-kdf0":    mutateHeader(t, none, func(f map[string]any) { f["verifier_hash"] = core.Encode(make([]byte, 32)) }),
		"no-verifier-with-kdf2": mutateHeader(t, pw, func(f map[string]any) { delete(f, "verifier_hash") }),
		"opens-0":               mutateHeader(t, none, func(f map[string]any) { f["opens_max"] = 0 }),
		"ttl-59":                mutateHeader(t, none, func(f map[string]any) { f["ttl_s"] = 59 }),
		"kdf-3":                 mutateHeader(t, none, func(f map[string]any) { f["kdf_id"] = 3 }),
		"trailing-brace":        core.Encode(append(append([]byte(nil), raw...), '}')),
		"trailing-object":       core.Encode(append(append([]byte(nil), raw...), []byte(" {}")...)),
		"not-an-object":         core.Encode([]byte(`[1,2]`)),
	}
	for name, value := range cases {
		if server.ParseCreateHeaderForTest(cfg, value) {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestClaimBodyVariants(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFPBKDF2, "pw", 1, 3600, "x")
	h.mustCreate(m)
	for name, body := range map[string]string{"malformed-json": `{"verifier":`, "short-verifier": `{"verifier":"` + m.verifier[:42] + `"}`} {
		resp, _ := h.do(request{method: "POST", path: "/api/v1/messages/" + m.id.String() + "/claim", body: []byte(body), headers: map[string]string{"Authorization": "Bearer " + m.readToken}})
		if resp.StatusCode != 400 {
			t.Fatalf("%s: %d", name, resp.StatusCode)
		}
	}
	if _, st := h.status(m); st["failed_passphrase_attempts"].(float64) != 0 || st["opens_used"].(float64) != 0 {
		t.Fatalf("malformed claim bodies changed the message: %v", st)
	}
	r, _ := http.NewRequest("POST", h.ts.URL+"/api/v1/messages/"+m.id.String()+"/claim", io.MultiReader(strings.NewReader(`{"verifier":"`+m.verifier+`"}`)))
	r.Header.Set("Authorization", "Bearer "+m.readToken)
	r.Header.Set("X-Forwarded-For", "203.0.113.4")
	resp, err := h.client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || r.ContentLength != 0 {
		t.Fatalf("claim with a chunked body: %d (content length %d)", resp.StatusCode, r.ContentLength)
	}
}

func TestPoWRejectsForgedAndMalformedTokens(t *testing.T) {
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.GlobalDailyCreates = 2
	cfg.PoWDifficulty = 1
	h := newHarness(t, cfg)
	first := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	first.ip = "203.0.113.1"
	h.mustCreate(first)
	forge := func(issued time.Time) string {
		chal := make([]byte, 36)
		binary.BigEndian.PutUint32(chal, uint32(issued.Unix()))
		return core.Encode(chal)
	}
	m := newMessage(t, core.KDFNone, "", 1, 3600, "forged")
	m.ip = "203.0.113.2"
	zeroTag, err := server.SolvePoW(forge(h.clock.Now()), m.id, 1)
	if err != nil {
		t.Fatal(err)
	}
	tokens := map[string]string{"no-dot": "abc", "short-counter": forge(h.clock.Now()) + ".00", "zero-tag": zeroTag}
	for name, token := range tokens {
		resp, body := h.do(request{method: "POST", path: "/api/v1/messages", body: m.object, ip: m.ip, headers: map[string]string{"X-Nylzo-Create": m.createHeader(0), "X-Nylzo-Pow": token}})
		if resp.StatusCode != 403 || decodeJSON(t, body)["error"] != "pow" {
			t.Fatalf("%s: %d %s", name, resp.StatusCode, body)
		}
	}
}

func TestGlobalCounterCountsOnlyAdmittedCreates(t *testing.T) {
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.GlobalDailyCreates = 1000
	h := newHarness(t, cfg)
	for i := 0; i < 25; i++ {
		m := newMessage(t, core.KDFNone, "", 1, 3600, "n")
		m.ip = "203.0.113.5"
		h.create(m)
	}
	day := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	if c, err := h.store.RateGet(t.Context(), server.GlobalCreatesKey, day); err != nil || c.Count != 20 {
		t.Fatalf("rate-limited attempts moved the global counter: %+v %v", c, err)
	}
	small := baseConfig(pepperA, nil)
	small.Rates.GlobalDailyCreates = 40
	h.reopen(small)
	for i := 0; i < 3; i++ {
		m := newMessage(t, core.KDFNone, "", 1, 3600, "n")
		m.ip = fmt.Sprintf("198.51.100.%d", i+1)
		if resp, _ := h.create(m); resp.StatusCode != 403 {
			t.Fatalf("create without pow at 50 percent: %d", resp.StatusCode)
		}
	}
	if c, _ := h.store.RateGet(t.Context(), server.GlobalCreatesKey, day); c.Count != 20 {
		t.Fatalf("PoW failures moved the global counter: %+v", c)
	}
	h.reopen(cfg)
	existing := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	existing.ip = "198.51.100.10"
	h.mustCreate(existing)
	if resp, _ := h.create(existing); resp.StatusCode != 409 {
		t.Fatalf("duplicate id: %d", resp.StatusCode)
	}
	wrongKDF := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	wrongKDF.ip = "198.51.100.11"
	headerKDF2 := mutateHeader(t, wrongKDF, func(f map[string]any) { f["kdf_id"] = 2; f["verifier_hash"] = core.Encode(make([]byte, 32)) })
	if resp, _ := h.do(request{method: "POST", path: "/api/v1/messages", body: wrongKDF.object, ip: wrongKDF.ip, headers: map[string]string{"X-Nylzo-Create": headerKDF2}}); resp.StatusCode != 400 {
		t.Fatalf("body kdf differs from the header: %d", resp.StatusCode)
	}
	if c, _ := h.store.RateGet(t.Context(), server.GlobalCreatesKey, day); c.Count != 21 {
		t.Fatalf("only stored messages may move the global counter: %+v", c)
	}
}

func TestPoWTokenIsSingleUse(t *testing.T) {
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.GlobalDailyCreates = 4
	cfg.PoWDifficulty = 1
	h := newHarness(t, cfg)
	for i := range 2 {
		m := newMessage(t, core.KDFNone, "", 1, 3600, "n")
		m.ip = fmt.Sprintf("203.0.113.%d", i+1)
		h.mustCreate(m)
	}
	solve := func(m *message) string {
		_, body := h.do(request{method: "GET", path: "/api/v1/instance"})
		token, err := server.SolvePoW(decodeJSON(t, body)["pow"].(map[string]any)["challenge"].(string), m.id, 1)
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	m := newMessage(t, core.KDFNone, "", 1, 3600, "pow")
	m.ip = "203.0.113.9"
	token := solve(m)
	badHeader := mutateHeader(t, m, func(f map[string]any) { f["kdf_id"] = 2; f["verifier_hash"] = core.Encode(make([]byte, 32)) })
	send := func(header, pow string) int {
		resp, _ := h.do(request{method: "POST", path: "/api/v1/messages", body: m.object, ip: m.ip, headers: map[string]string{"X-Nylzo-Create": header, "X-Nylzo-Pow": pow}})
		return resp.StatusCode
	}
	if code := send(badHeader, token); code != 400 {
		t.Fatalf("first use with a mismatched body: %d", code)
	}
	if code := send(m.createHeader(0), token); code != 403 {
		t.Fatalf("replayed token: %d", code)
	}
	day := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	if c, _ := h.store.RateGet(t.Context(), server.GlobalCreatesKey, day); c.Count != 2 {
		t.Fatalf("failed and replayed creates moved the global counter: %+v", c)
	}
	other := newMessage(t, core.KDFNone, "", 1, 3600, "pow")
	other.ip = "203.0.113.10"
	challenge, _, _ := strings.Cut(token, ".")
	reuse, err := server.SolvePoW(challenge, other.id, 1)
	if err != nil {
		t.Fatal(err)
	}
	if resp, _ := h.do(request{method: "POST", path: "/api/v1/messages", body: other.object, ip: other.ip, headers: map[string]string{"X-Nylzo-Create": other.createHeader(0), "X-Nylzo-Pow": reuse}}); resp.StatusCode != 403 {
		t.Fatalf("a second token for a used challenge: %d", resp.StatusCode)
	}
	if code := send(m.createHeader(0), solve(m)); code != 201 {
		t.Fatalf("fresh challenge: %d", code)
	}
}

func TestCreateBytesPerDayLimit(t *testing.T) {
	cfg := baseConfig(pepperA, nil)
	h := newHarness(t, cfg)
	probe := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	cfg.Rates.CreateBytesPerDay = int64(2*len(probe.object) + 1)
	h.reopen(cfg)
	for i := 0; i < 2; i++ {
		m := newMessage(t, core.KDFNone, "", 1, 3600, "n")
		m.ip = "203.0.113.5"
		h.mustCreate(m)
	}
	m := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	m.ip = "203.0.113.5"
	if resp, _ := h.create(m); resp.StatusCode != 429 || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("third create over the daily byte limit: %d", resp.StatusCode)
	}
}

func TestReadRateLimitCountsBlob(t *testing.T) {
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.ReadPerHour = 2
	h := newHarness(t, cfg)
	m := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	m.ip = "203.0.113.5"
	h.mustCreate(m)
	resp, _ := h.claim(m, "", "")
	if resp.StatusCode != 200 {
		t.Fatalf("claim: %d", resp.StatusCode)
	}
	lease := resp.Header.Get("X-Nylzo-Lease")
	if resp, _ := h.blob(m, lease); resp.StatusCode != 200 {
		t.Fatalf("first download: %d", resp.StatusCode)
	}
	if resp, _ := h.blob(m, lease); resp.StatusCode != 429 {
		t.Fatalf("second download over the read limit: %d", resp.StatusCode)
	}
}

func TestPassphraseBackoffIsSharedByEveryReader(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFPBKDF2, "pw", 1, 3600, "x")
	h.mustCreate(m)
	wrong := core.Encode(make([]byte, 32))
	for i := 1; i <= 6; i++ {
		h.claim(m, wrong, fmt.Sprintf("203.0.113.%d", i))
	}
	resp, body := h.claim(m, m.verifier, "198.51.100.1")
	if resp.StatusCode != 403 || decodeJSON(t, body)["retry_after"].(float64) != 60 {
		t.Fatalf("the backoff must apply to every address, rounded up to the minute: %d %s", resp.StatusCode, body)
	}
}

func TestAbuseReportsRecordOnlyValidReports(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
	h.mustCreate(m)
	send := func(body string) int {
		resp, _ := h.do(request{method: "POST", path: "/api/v1/abuse", ip: "203.0.113.70", body: []byte(body), headers: map[string]string{"Content-Type": "application/json"}})
		return resp.StatusCode
	}
	long := strings.Repeat("r", 3000)
	if send(fmt.Sprintf(`{"id":%q,"read_token":%q,"reason":%q}`, m.id.String(), m.readToken, long)) != 202 ||
		send(fmt.Sprintf(`{"id":%q,"read_token":%q}`, core.NewID().String(), m.readToken)) != 202 ||
		send(fmt.Sprintf(`{"id":%q,"read_token":%q}`, m.id.String(), core.Encode(make([]byte, 32)))) != 202 {
		t.Fatal("reports must answer 202")
	}
	if send(`{"id":`) != 400 {
		t.Fatal("malformed report body")
	}
	data, err := os.ReadFile(filepath.Join(h.dir, "abuse-reports.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], m.id.String()) || strings.Contains(lines[0], strings.Repeat("r", 1001)) {
		t.Fatalf("recorded reports: %d lines", len(lines))
	}
	if resp, meta := h.metadata(m); resp.StatusCode != 200 || meta["state"] != "available" {
		t.Fatal("abuse report changed the message")
	}
}

func TestConcurrentCreateWithSameID(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	for i := 0; i < 20; i++ {
		m := newMessage(t, core.KDFNone, "", 1, 3600, "race")
		m.ip = fmt.Sprintf("198.51.100.%d", i+1)
		var wg sync.WaitGroup
		codes := make([]int, 2)
		for j := range codes {
			wg.Add(1)
			go func(j int) {
				defer wg.Done()
				resp, _ := h.create(m)
				codes[j] = resp.StatusCode
			}(j)
		}
		wg.Wait()
		if !((codes[0] == 201 && codes[1] == 409) || (codes[0] == 409 && codes[1] == 201)) {
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

func TestServerLogsHideAddressesAndSecrets(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	srv := h.srv.HTTPServer()
	if srv.ErrorLog == nil {
		t.Fatal("ErrorLog is nil: net/http would log peer addresses to stderr")
	}
	srv.ErrorLog.Printf("http: TLS handshake error from 203.0.113.99:4242: EOF")
	s := core.NewSecret()
	id := core.NewID()
	h.do(request{method: "GET", path: "/m/" + id.String() + "%231.0." + core.Encode(s[:])})
	logs := h.logBuf.String()
	if strings.Contains(logs, "203.0.113.99") || strings.Contains(logs, core.Encode(s[:])) {
		t.Fatal("log contains a peer address or a link secret")
	}
	if !strings.Contains(logs, "net/http error") {
		t.Fatal("net/http errors must still leave a signal")
	}
}

func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.BucketPerSecond = 1
	cfg.Rates.BucketBurst = 2
	h := newHarness(t, cfg)
	want := map[string]string{
		"Referrer-Policy":              "no-referrer",
		"Cache-Control":                "no-store",
		"X-Robots-Tag":                 "noindex, nofollow",
		"X-Content-Type-Options":       "nosniff",
		"Cross-Origin-Opener-Policy":   "same-origin",
		"Cross-Origin-Resource-Policy": "same-origin",
		"Strict-Transport-Security":    "max-age=31536000; includeSubDomains",
		"Permissions-Policy":           "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()",
	}
	check := func(name string, resp *http.Response, status int) {
		t.Helper()
		if resp.StatusCode != status {
			t.Fatalf("%s: status %d, want %d", name, resp.StatusCode, status)
		}
		for k, v := range want {
			if resp.Header.Get(k) != v {
				t.Fatalf("%s: %s = %q", name, k, resp.Header.Get(k))
			}
		}
	}
	resp, _ := h.do(request{method: "GET", path: "/"})
	check("page", resp, 200)
	if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "'nonce-") {
		t.Fatal("CSP missing on /")
	}
	resp, _ = h.do(request{method: "GET", path: "/api/v1/instance", headers: map[string]string{"Sec-Purpose": "prefetch"}})
	check("sec-purpose", resp, 503)
	resp, _ = h.do(request{method: "POST", path: "/api/v1/messages", body: []byte("x")})
	check("create-400", resp, 400)
	resp, _ = h.do(request{method: "GET", path: "/api/v1/instance"})
	resp, _ = h.do(request{method: "GET", path: "/api/v1/instance"})
	check("throttled", resp, 429)
}

func TestTruncatedCreateStoresNothing(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 3600, "short")
	conn, err := net.Dial("tcp", h.listen())
	if err != nil {
		t.Fatal(err)
	}
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprintf(conn, "POST /api/v1/messages HTTP/1.1\r\nHost: nylzo\r\nContent-Length: %d\r\nX-Nylzo-Create: %s\r\n\r\n", len(m.object), m.createHeader(0))
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

func TestSweepOnceWithoutPepperEndsExpiredMessages(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	h.clock.mu.Lock()
	h.clock.now = time.Now().Add(-time.Hour)
	h.clock.mu.Unlock()
	m := newMessage(t, core.KDFNone, "", 1, 60, "sweep")
	h.mustCreate(m)
	cfg := h.cfg
	cfg.Pepper = nil
	report, err := server.SweepOnce(t.Context(), h.store, h.blobs, time.Now())
	if err != nil || report.Expired != 1 || h.blobExists(m.id) {
		t.Fatalf("sweep without pepper: %+v %v", report, err)
	}
}

func TestSweepReconcilesOncePerDay(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	h.startOfNextUTCDay()
	put := func(ref string) {
		if err := h.blobs.Put(t.Context(), ref, strings.NewReader("x"), 1); err != nil {
			t.Fatal(err)
		}
	}
	put("orphan-one")
	report, err := h.srv.SweepOnce(t.Context())
	if err != nil || report.OrphanedBlobs != 1 {
		t.Fatalf("first sweep of the day: %+v %v", report, err)
	}
	put("orphan-two")
	h.reopen(h.cfg)
	if report, err := h.srv.SweepOnce(t.Context()); err != nil || report.OrphanedBlobs != 0 {
		t.Fatalf("a restarted server reconciled twice in one day: %+v %v", report, err)
	}
	h.clock.Advance(24 * time.Hour)
	if report, err := h.srv.SweepOnce(t.Context()); err != nil || report.OrphanedBlobs != 1 {
		t.Fatalf("first sweep of the next day: %+v %v", report, err)
	}
}

func TestSweepRetriesReconcileAfterFailure(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	h.startOfNextUTCDay()
	fb := h.withFaultyBlobs()
	for _, ref := range []string{"0-stuck", "orphan"} {
		if err := h.blobs.Put(t.Context(), ref, strings.NewReader("x"), 1); err != nil {
			t.Fatal(err)
		}
	}
	m := newMessage(t, core.KDFNone, "", 1, 60, "expiring")
	h.mustCreate(m)
	h.clock.Advance(2 * time.Minute)
	fb.set(func(b *faultyBlobs) { b.failList = errInjected; b.failDelete[m.id.String()] = true })
	report, err := h.srv.SweepOnce(t.Context())
	if err == nil || report.Expired != 0 || report.OrphanedBlobs != 0 {
		t.Fatalf("a failed listing: %+v %v", report, err)
	}
	fb.set(func(b *faultyBlobs) { b.failList = nil; clear(b.failDelete); b.failDelete["0-stuck"] = true })
	report, err = h.srv.SweepOnce(t.Context())
	if err == nil || report.Expired != 1 || report.OrphanedBlobs != 1 {
		t.Fatalf("a reconcile whose listing failed must run again the same day and skip past a failed delete: %+v %v", report, err)
	}
	report, err = h.srv.SweepOnce(t.Context())
	if err != nil || report.OrphanedBlobs != 0 {
		t.Fatalf("a reconcile that listed the store must be recorded for the day: %+v %v", report, err)
	}
}

func TestBlobLossWithoutEndingIsAStorageFault(t *testing.T) {
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
	h := newHarness(t, baseConfig(pepperA, nil))
	fb := h.withFaultyBlobs()
	m := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	h.mustCreate(m)
	fb.set(func(b *faultyBlobs) { b.failRead = errInjected })
	req, err := http.NewRequest("POST", h.ts.URL+"/api/v1/messages/"+m.id.String()+"/claim", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+m.readToken)
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
	fb.set(func(b *faultyBlobs) { b.failRead = nil })
	resp, object := h.blob(m, lease)
	if resp.StatusCode != 200 || !bytes.Equal(object, m.object) {
		t.Fatalf("retry through the lease: %d", resp.StatusCode)
	}
}

func TestCreateStorageFailureAnswers500(t *testing.T) {
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

func TestRejectedCreateKeepsH2CConnection(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	addr := h.listen()
	var protocols http.Protocols
	protocols.SetUnencryptedHTTP2(true)
	client := &http.Client{Transport: &http.Transport{Protocols: &protocols}, Timeout: 5 * time.Second}
	var reused []bool
	trace := &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { reused = append(reused, info.Reused) }}
	send := func(method, path string) *http.Response {
		r, _ := http.NewRequestWithContext(httptrace.WithClientTrace(t.Context(), trace), method, "http://"+addr+path, nil)
		resp, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return resp
	}
	if resp := send("POST", "/api/v1/messages"); resp.StatusCode != 400 || resp.ProtoMajor != 2 {
		t.Fatalf("rejected create over h2c: %d HTTP/%d", resp.StatusCode, resp.ProtoMajor)
	}
	if resp := send("GET", "/healthz"); resp.StatusCode != 200 || resp.ProtoMajor != 2 {
		t.Fatalf("follow-up over h2c: %d HTTP/%d", resp.StatusCode, resp.ProtoMajor)
	}
	if len(reused) != 2 || !reused[1] {
		t.Fatalf("a rejected create closed the shared h2c connection: %v", reused)
	}
}

func TestSourceLimiterIsolatesClients(t *testing.T) {
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.SourcePerSecond = 1
	cfg.Rates.SourceBurst = 2
	h := newHarness(t, cfg)
	get := func(ip string) int {
		resp, _ := h.do(request{method: "GET", path: "/api/v1/instance", ip: ip})
		return resp.StatusCode
	}
	if get("203.0.113.1") != 200 || get("203.0.113.1") != 200 || get("203.0.113.1") != 429 {
		t.Fatal("one source must be limited after its burst")
	}
	if get("2001:db8:1:1::1") != 200 || get("2001:db8:1:1::2") != 200 || get("2001:db8:1:1::3") != 429 {
		t.Fatal("addresses in one /64 must share a bucket")
	}
	if get("203.0.113.2") != 200 {
		t.Fatal("a busy source must not throttle other clients")
	}
	if resp, _ := h.do(request{method: "GET", path: "/healthz", ip: "203.0.113.1"}); resp.StatusCode != 200 {
		t.Fatal("non-API paths are not throttled")
	}
	h.clock.Advance(time.Second)
	if get("203.0.113.1") != 200 {
		t.Fatal("the source bucket did not refill")
	}
}

func TestAbuseReportHardening(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
	h.mustCreate(m)
	link := core.FormatLink("https://nylzo.example", m.id, m.kdf, m.secret)
	_, fragment, _ := strings.Cut(link, "#")
	body := fmt.Sprintf(`{"id":%q,"read_token":%q,"reason":%q}`, m.id.String(), m.readToken, "malware at "+link+" and again "+fragment)
	for _, ct := range []string{"", "text/plain", "application/x-www-form-urlencoded"} {
		resp, _ := h.do(request{method: "POST", path: "/api/v1/abuse", ip: "203.0.113.80", body: []byte(body), headers: map[string]string{"Content-Type": ct}})
		if resp.StatusCode != 400 {
			t.Fatalf("content type %q: %d", ct, resp.StatusCode)
		}
	}
	resp, _ := h.do(request{method: "POST", path: "/api/v1/abuse", ip: "203.0.113.81", body: []byte(body), headers: map[string]string{"Content-Type": "application/json; charset=utf-8"}})
	if resp.StatusCode != 202 {
		t.Fatalf("json report: %d", resp.StatusCode)
	}
	path := filepath.Join(h.dir, "abuse-reports.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	secret := core.Encode(m.secret[:])
	if strings.Contains(string(data), secret) || !strings.Contains(string(data), "[redacted]") {
		t.Fatalf("the report kept the link secret: %s", data)
	}
	if err := os.Truncate(path, 16<<20); err != nil {
		t.Fatal(err)
	}
	resp, _ = h.do(request{method: "POST", path: "/api/v1/abuse", ip: "203.0.113.82", body: []byte(body), headers: map[string]string{"Content-Type": "application/json"}})
	info, err := os.Stat(path)
	if resp.StatusCode != 503 || err != nil || info.Size() != 16<<20 || !strings.Contains(h.logBuf.String(), "abuse-file-full") {
		t.Fatalf("a full report file must answer 503, log an error and not grow: %d %v %d", resp.StatusCode, err, info.Size())
	}
}

func TestConfigBounds(t *testing.T) {
	base := map[string]string{"NYLZO_PEPPER": strings.Repeat("ab", 32)}
	with := func(k, v string) func(string) string {
		return func(key string) string {
			if key == k {
				return v
			}
			return base[key]
		}
	}
	for name, env := range map[string]func(string) string{
		"max-ttl-over-7d":      with("NYLZO_MAX_TTL", "169h"),
		"max-ttl-over-7d-sec":  with("NYLZO_MAX_TTL", "604801"),
		"default-ttl-under-60": with("NYLZO_DEFAULT_TTL", "59"),
		"pow-concurrency-17":   with("NYLZO_POW_CONCURRENCY", "17"),
		"ip-scope-removed":     with("NYLZO_PASSPHRASE_IP_SCOPE", "on"),
		"max-opens-above-100":  with("NYLZO_MAX_OPENS", "101"),
		"pow-difficulty-9":     with("NYLZO_POW_DIFFICULTY", "9"),
		"default-kdf-none":     with("NYLZO_DEFAULT_KDF", "0"),
		"source-rps-zero":      with("NYLZO_RATE_SOURCE_RPS", "0"),
		"source-rps-nan":       with("NYLZO_RATE_SOURCE_RPS", "NaN"),
		"source-burst-inf":     with("NYLZO_RATE_SOURCE_BURST", "+Inf"),
		"source-burst-half":    with("NYLZO_RATE_SOURCE_BURST", "0.5"),
		"bucket-burst-half":    with("NYLZO_RATE_BUCKET_BURST", "0.5"),
	} {
		if _, err := server.FromEnv(env, true); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	cfg, err := server.FromEnv(with("NYLZO_MAX_TTL", "604800"), true)
	if err != nil || cfg.Limits.MaxTTL != store.TombstoneTTL {
		t.Fatalf("max ttl of 7 days: %v %v", err, cfg.Limits.MaxTTL)
	}
}

func TestAK27PreviousPepperHorizon(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	old := newMessage(t, core.KDFNone, "", 1, 7*24*3600, "max ttl")
	h.mustCreate(old)
	h.reopen(baseConfig(pepperB, pepperA))
	h.clock.Advance(7*24*time.Hour + time.Minute)
	if _, err := h.srv.SweepOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if resp, meta := h.metadata(old); resp.StatusCode != 200 || meta["state"] != "expired" {
		t.Fatalf("tombstone with the previous pepper still configured: %d %v", resp.StatusCode, meta)
	}
	if rec, err := h.store.Get(t.Context(), old.id.String()); err != nil || rec.Message != nil {
		t.Fatal("no live message may remain after the maximum TTL")
	}
	h.clock.Advance(store.TombstoneTTL)
	if _, err := h.srv.SweepOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	h.reopen(baseConfig(pepperB, nil))
	if _, err := h.store.Get(t.Context(), old.id.String()); err == nil {
		t.Fatal("tombstone must be purged before the previous pepper is dropped")
	}
}

func TestMalformedIDsDoNotSpendReadLimit(t *testing.T) {
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.ReadPerHour = 1
	h := newHarness(t, cfg)
	m := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	m.ip = "203.0.113.5"
	h.mustCreate(m)
	for _, path := range []string{"/api/v1/messages/short", "/api/v1/messages/short/claim", "/api/v1/messages/short/blob"} {
		method := "GET"
		if strings.HasSuffix(path, "/claim") {
			method = "POST"
		}
		if resp, _ := h.do(request{method: method, path: path, ip: m.ip, headers: map[string]string{"Authorization": "Bearer " + m.readToken}}); resp.StatusCode != 404 {
			t.Fatalf("%s %s: %d", method, path, resp.StatusCode)
		}
	}
	if resp, _ := h.metadata(m); resp.StatusCode != 200 {
		t.Fatalf("malformed ids spent the read limit: %d", resp.StatusCode)
	}
}

func TestSweepLogAttrsCountsEveryFailedItem(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	fb := h.withFaultyBlobs()
	for range 2 {
		m := newMessage(t, core.KDFNone, "", 1, 60, "x")
		h.mustCreate(m)
		fb.set(func(b *faultyBlobs) { b.failDelete[m.id.String()] = true })
	}
	h.clock.Advance(2 * time.Minute)
	report, err := h.srv.SweepOnce(t.Context())
	attrs := server.SweepLogAttrs(report, err)
	got := map[any]any{}
	for i := 0; i+1 < len(attrs); i += 2 {
		got[attrs[i]] = attrs[i+1]
	}
	if got["errors"] != 2 || strings.Contains(fmt.Sprint(got["err"]), "\n") {
		t.Fatalf("want 2 failed items and only the first error: %v", got)
	}
}

func TestRefusedSourceTakesNoInstanceToken(t *testing.T) {
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.SourcePerSecond, cfg.Rates.SourceBurst = 1, 1
	cfg.Rates.BucketPerSecond, cfg.Rates.BucketBurst = 1, 3
	h := newHarness(t, cfg)
	get := func(ip string) int {
		resp, _ := h.do(request{method: "GET", path: "/api/v1/instance", ip: ip})
		return resp.StatusCode
	}
	if get("203.0.113.1") != 200 {
		t.Fatal("first request")
	}
	for range 5 {
		if get("203.0.113.1") != 429 {
			t.Fatal("source not limited")
		}
	}
	if get("198.51.100.2") != 200 || get("192.0.2.3") != 200 {
		t.Fatal("a refused source drained the instance bucket")
	}
}

func TestPrefixTierLimitsOneRange(t *testing.T) {
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.BucketPerSecond, cfg.Rates.BucketBurst = 10, 10
	h := newHarness(t, cfg)
	codes := []int{}
	for i := range 8 {
		resp, _ := h.do(request{method: "GET", path: "/api/v1/instance", ip: fmt.Sprintf("2001:db8:1:%x::1", i)})
		codes = append(codes, resp.StatusCode)
	}
	if codes[4] != 200 || codes[5] != 429 || codes[7] != 429 {
		t.Fatalf("one /48 must get at most half of the instance bucket: %v", codes)
	}
	if resp, _ := h.do(request{method: "GET", path: "/api/v1/instance", ip: "2001:db8:2::1"}); resp.StatusCode != 200 {
		t.Fatal("another /48 was throttled")
	}
}

func TestEverySweepPurgesStaleTempFiles(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	h.startOfNextUTCDay()
	stale := func(name string) {
		if err := os.WriteFile(filepath.Join(h.dir, "blobs", "tmp", name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	stale("blob-first")
	if report, err := h.srv.SweepOnce(t.Context()); err != nil || report.StaleTempFiles != 1 {
		t.Fatalf("first sweep: %+v %v", report, err)
	}
	stale("blob-second")
	if report, err := h.srv.SweepOnce(t.Context()); err != nil || report.OrphanedBlobs != 0 || report.StaleTempFiles != 1 {
		t.Fatalf("a sweep after the daily reconcile must still purge temp files: %+v %v", report, err)
	}
}

func TestStorageConflictMatchesEarlyConflict(t *testing.T) {
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
	h := newHarness(t, baseConfig(pepperA, nil))
	fb := h.withFaultyBlobs()
	fb.set(func(b *faultyBlobs) { b.failPut = context.Canceled })
	if resp, _ := h.create(newMessage(t, core.KDFNone, "", 1, 3600, "x")); resp.StatusCode != 500 {
		t.Fatalf("storage cancellation with a live client: %d", resp.StatusCode)
	}
}

func TestHTTPErrorLogCategoriesAndLimit(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	errLog := h.srv.HTTPServer().ErrorLog
	errLog.Printf("http: TLS handshake error from 203.0.113.99:4242: EOF")
	for range 40 {
		errLog.Printf("http2: server: error reading preface from client 203.0.113.99:4242: EOF")
	}
	errLog.Printf("http2: panic serving 203.0.113.99:4242: boom")
	logs := h.logBuf.String()
	for _, want := range []string{`"category":"tls-handshake"`, `"category":"http2"`, `"category":"panic"`} {
		if !strings.Contains(logs, want) {
			t.Fatalf("%s missing: %s", want, logs)
		}
	}
	if n := strings.Count(logs, `"level":"WARN","msg":"net/http error"`); n > 25 {
		t.Fatalf("net/http warnings are not rate limited: %d", n)
	}
	if strings.Contains(logs, "203.0.113.99") || strings.Contains(logs, "boom") {
		t.Fatal("net/http error text reached the log")
	}
}

func TestAbuseReasonRedactionEdges(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
	h.mustCreate(m)
	secret := core.Encode(m.secret[:])
	link := core.FormatLink("https://nylzo.example", m.id, m.kdf, m.secret)
	reasons := []string{
		"see https://x.example/p#token-abc def",
		strings.Repeat("a ", 499) + "aé tail",
		"https://safelinks.example/?url=" + strings.NewReplacer(":", "%3A", "/", "%2F", "#", "%23").Replace(link),
		"double %2523" + secret[:30] + " and glued key_1.0." + secret,
		"bare " + secret + " end",
		strings.Repeat("<", 600),
		"see https%3A%2F%2Fx.example%2Fp%23token-abc def",
		"see p%2523token-abc def",
		"see p%252523token-abc def",
		"see p%25252523token-abc def",
		"a\u009bb\u202ec\u007fd\u2066e%E2%80%AEf\nnext",
	}
	for i, reason := range reasons {
		reported := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
		h.mustCreate(reported)
		body, _ := json.Marshal(map[string]string{"id": reported.id.String(), "read_token": reported.readToken, "reason": reason})
		resp, _ := h.do(request{method: "POST", path: "/api/v1/abuse", ip: fmt.Sprintf("203.0.%d.1", 90+i), body: body, headers: map[string]string{"Content-Type": "application/json"}})
		if resp.StatusCode != 202 {
			t.Fatalf("report %d: %d", i, resp.StatusCode)
		}
	}
	data, err := os.ReadFile(filepath.Join(h.dir, "abuse-reports.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret[:22]) {
		t.Fatalf("a link secret reached the report file: %s", data)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	reason := func(i int) string {
		var row struct{ Reason string }
		if err := json.Unmarshal([]byte(lines[i]), &row); err != nil {
			t.Fatal(err)
		}
		return row.Reason
	}
	if got := reason(0); got != "see https://x.example/p[redacted] def" {
		t.Fatalf("hash run: %q", got)
	}
	if got := reason(6); got != "see https://x.example/p[redacted] def" {
		t.Fatalf("percent-encoded hash run: %q", got)
	}
	for i, want := range map[int]string{7: "see p[redacted] def", 8: "see p[redacted] def", 9: "see p%23token-abc def", 10: "a bc def next"} {
		if got := reason(i); got != want {
			t.Fatalf("reason %d: %q, want %q", i, got, want)
		}
	}
	if got := reason(1); len(got) != 999 || strings.ContainsRune(got, utf8.RuneError) {
		t.Fatalf("cut is not UTF-8 safe: %d", len(got))
	}
	for i, line := range lines {
		if len(line) > 1400 {
			t.Fatalf("line %d is %d bytes after escaping", i, len(line))
		}
	}
}

func TestAbuseReportsCappedPerMessage(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
	other := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
	third := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
	h.mustCreate(m)
	h.mustCreate(other)
	h.mustCreate(third)
	path := filepath.Join(h.dir, "abuse-reports.jsonl")
	report := func(msg *message, ip string) int {
		body := fmt.Sprintf(`{"id":%q,"read_token":%q,"reason":"spam"}`, msg.id.String(), msg.readToken)
		resp, _ := h.do(request{method: "POST", path: "/api/v1/abuse", ip: ip, body: []byte(body), headers: map[string]string{"Content-Type": "application/json"}})
		return resp.StatusCode
	}
	stored := func() int {
		data, _ := os.ReadFile(path)
		return strings.Count(string(data), "\n")
	}
	for i := range 4 {
		if code := report(m, fmt.Sprintf("198.51.%d.1", 7+i)); code != 202 {
			t.Fatalf("report %d: %d", i, code)
		}
	}
	if n := stored(); n != 3 {
		t.Fatalf("one message stored %d reports from different ranges, want 3", n)
	}
	report(other, "198.51.7.9")
	if n := stored(); n != 4 {
		t.Fatalf("another message must keep its own budget: %d", n)
	}
	if err := os.Truncate(path, 16<<20); err != nil {
		t.Fatal(err)
	}
	if code := report(third, "192.0.2.1"); code != 503 {
		t.Fatalf("full file: %d", code)
	}
	if err := os.Truncate(path, 0); err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		report(third, fmt.Sprintf("192.0.2.%d", i+2))
	}
	if n := stored(); n != 3 {
		t.Fatalf("a report refused for a full file must not use up the message budget: %d", n)
	}
	h.clock.Advance(24 * time.Hour)
	report(m, "198.51.7.20")
	if n := stored(); n != 4 {
		t.Fatalf("the budget must reset the next UTC day: %d", n)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(h.dir, "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var tied int
	if err := db.QueryRow(`SELECT COUNT(*) FROM rate_counters WHERE key LIKE 'abuse:id:%:%'`).Scan(&tied); err != nil || tied != 0 {
		t.Fatalf("a counter ties a message to a reporter range: %d %v", tied, err)
	}
}

func TestBurnFinishesRecordedCancel(t *testing.T) {
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
	h := newHarness(t, baseConfig(pepperA, nil))
	fb := h.withFaultyBlobs()
	m := newMessage(t, core.KDFNone, "", 2, 3600, "x")
	h.mustCreate(m)
	if _, _, _, err := h.store.MarkEnding(t.Context(), m.id.String(), store.StateCancelled, h.clock.Now()); err != nil {
		t.Fatal(err)
	}
	fb.set(func(b *faultyBlobs) { b.failDelete[m.id.String()] = true })
	unknown, unknownBody := h.do(request{method: "GET", path: "/api/v1/messages/" + core.NewID().String(), headers: map[string]string{"Authorization": "Bearer " + m.readToken}})
	wrong, wrongBody := h.do(request{method: "GET", path: "/api/v1/messages/" + m.id.String(), headers: map[string]string{"Authorization": "Bearer " + core.Encode(make([]byte, 32))}})
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

func TestHTTPErrorLogReportsDrops(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	errLog := h.srv.HTTPServer().ErrorLog
	for range 41 {
		errLog.Printf("http2: server: error reading preface")
	}
	h.clock.Advance(time.Second)
	errLog.Printf("http2: server: error reading preface")
	if !strings.Contains(h.logBuf.String(), `"dropped":21`) {
		t.Fatalf("drop count not reported: %s", h.logBuf.String())
	}
}

func TestRefusedPrefixTakesNoInstanceToken(t *testing.T) {
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.BucketPerSecond, cfg.Rates.BucketBurst = 10, 10
	h := newHarness(t, cfg)
	for i := range 20 {
		h.do(request{method: "GET", path: "/api/v1/instance", ip: fmt.Sprintf("2001:db8:1:%x::1", i)})
	}
	for i := range 5 {
		if resp, _ := h.do(request{method: "GET", path: "/api/v1/instance", ip: fmt.Sprintf("2001:db8:%x::1", i+2)}); resp.StatusCode != 200 {
			t.Fatalf("a refused /48 drained the instance bucket: request %d got %d", i, resp.StatusCode)
		}
	}
}

func TestDefaultObjectLimitMatchesSpec(t *testing.T) {
	if got := server.Defaults().Limits.MaxObjectBytes; got != 5373983 {
		t.Fatalf("default max_object_bytes %d, spec section 3 says 5373983", got)
	}
	h := newHarness(t, baseConfig(pepperA, nil))
	_, body := h.do(request{method: "GET", path: "/api/v1/instance"})
	limits := decodeJSON(t, body)["limits"].(map[string]any)
	if limits["max_object_bytes"] != float64(5373983) || limits["max_text_bytes"] != float64(core.MaxTextBytes) {
		t.Fatalf("instance reports %v", limits)
	}
}

type slotSignals struct {
	store.Store
	reserved chan struct{}
	released chan struct{}
}

func (s *slotSignals) RateReserve(ctx context.Context, hit store.RateHit) (int64, bool, error) {
	n, ok, err := s.Store.RateReserve(ctx, hit)
	if hit.Key == server.GlobalCreatesKey {
		s.reserved <- struct{}{}
	}
	return n, ok, err
}

func (s *slotSignals) RateRelease(ctx context.Context, key string, windowStart time.Time) error {
	err := s.Store.RateRelease(ctx, key, windowStart)
	if key == server.GlobalCreatesKey {
		s.released <- struct{}{}
	}
	return err
}

func TestDisconnectedCreateReturnsItsCapSlot(t *testing.T) {
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.GlobalDailyCreates = 10
	h := newHarness(t, cfg)
	signals := &slotSignals{Store: h.store, reserved: make(chan struct{}, 1), released: make(chan struct{}, 1)}
	h.store = signals
	h.reopen(h.cfg)
	m := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	conn, err := net.Dial("tcp", h.listen())
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(conn, "POST /api/v1/messages HTTP/1.1\r\nHost: nylzo\r\nContent-Length: %d\r\nX-Nylzo-Create: %s\r\n\r\n", len(m.object), m.createHeader(0))
	conn.Write(m.object[:100])
	wait := func(ch chan struct{}, what string) {
		select {
		case <-ch:
		// gstack-timing-ok: an upper bound on waiting for the event, not a measurement
		case <-time.After(5 * time.Second):
			t.Fatalf("the create never %s its cap slot", what)
		}
	}
	wait(signals.reserved, "reserved")
	conn.Close()
	wait(signals.released, "released")
	day := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	if c, err := h.store.RateGet(t.Context(), server.GlobalCreatesKey, day); err != nil || c.Count != 0 {
		t.Fatalf("a disconnected create kept its cap slot: %+v %v", c, err)
	}
}

func TestCapFullOfPendingCreatesAsksForAShortRetry(t *testing.T) {
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.GlobalDailyCreates = 2
	h := newHarness(t, cfg)
	day := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	hit := store.RateHit{Key: server.GlobalCreatesKey, WindowStart: day, PurgeAt: day.Add(48 * time.Hour), Limit: 2}
	for range 2 {
		if _, ok, err := h.store.RateReserve(t.Context(), hit); err != nil || !ok {
			t.Fatal(err)
		}
	}
	m := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	resp, _ := h.create(m)
	if resp.StatusCode != 503 || resp.Header.Get("Retry-After") != "60" {
		t.Fatalf("pending creates must ask for a short retry: %d %q", resp.StatusCode, resp.Header.Get("Retry-After"))
	}
	if _, err := h.store.RateHit(t.Context(), []store.RateHit{{Key: server.GlobalStoredKey, WindowStart: day, PurgeAt: day.Add(48 * time.Hour)}, {Key: server.GlobalStoredKey, WindowStart: day, PurgeAt: day.Add(48 * time.Hour)}}); err != nil {
		t.Fatal(err)
	}
	resp, _ = h.create(m)
	if retry, _ := strconv.Atoi(resp.Header.Get("Retry-After")); resp.StatusCode != 503 || retry < 3600 {
		t.Fatalf("a cap full of stored messages must point to the next day: %d %q", resp.StatusCode, resp.Header.Get("Retry-After"))
	}
}

type failingEndStore struct {
	store.Store
}

func (s failingEndStore) End(ctx context.Context, id string, state store.State, now time.Time) error {
	return errInjected
}

func TestUnfinishableEndingReportsRecordedState(t *testing.T) {
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

type staleGateStore struct{ store.Store }

func (s staleGateStore) RateGet(ctx context.Context, key string, ws time.Time) (store.RateCount, error) {
	if key == server.GlobalCreatesKey {
		return store.RateCount{}, nil
	}
	return s.Store.RateGet(ctx, key, ws)
}

func TestSlotPastHalfTheCapNeedsPoW(t *testing.T) {
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.GlobalDailyCreates = 4
	h := newHarness(t, cfg)
	h.mustCreate(newMessage(t, core.KDFNone, "", 1, 3600, "a"))
	h.mustCreate(newMessage(t, core.KDFNone, "", 1, 3600, "b"))
	inner := h.store
	h.store = staleGateStore{Store: inner}
	h.reopen(h.cfg)
	if resp, body := h.create(newMessage(t, core.KDFNone, "", 1, 3600, "c")); resp.StatusCode != 403 || decodeJSON(t, body)["error"] != "pow" {
		t.Fatalf("the third of four slots was taken without PoW: %d %s", resp.StatusCode, body)
	}
	day := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	if c, err := inner.RateGet(t.Context(), server.GlobalCreatesKey, day); err != nil || c.Count != 2 {
		t.Fatalf("the refused create must return its slot: %+v %v", c, err)
	}
}

type endingThenFailingGet struct {
	store.Store
	armed atomic.Bool
}

func (s *endingThenFailingGet) MarkEnding(ctx context.Context, id string, state store.State, now time.Time) (store.State, string, bool, error) {
	if err := s.Store.End(ctx, id, store.StateCancelled, now); err != nil {
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

func TestStoredCapPointsToTheNextDay(t *testing.T) {
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.GlobalDailyCreates = 1
	h := newHarness(t, cfg)
	h.mustCreate(newMessage(t, core.KDFNone, "", 1, 3600, "x"))
	resp, _ := h.create(newMessage(t, core.KDFNone, "", 1, 3600, "y"))
	if retry, _ := strconv.Atoi(resp.Header.Get("Retry-After")); resp.StatusCode != 503 || retry < 3600 {
		t.Fatalf("a cap filled by stored messages: %d %q", resp.StatusCode, resp.Header.Get("Retry-After"))
	}
}

type fillingStore struct {
	store.Store
	once sync.Once
	fill func()
}

func (s *fillingStore) RateGet(ctx context.Context, key string, ws time.Time) (store.RateCount, error) {
	c, err := s.Store.RateGet(ctx, key, ws)
	if key == server.GlobalCreatesKey {
		s.once.Do(s.fill)
	}
	return c, err
}

func TestReservationLostAfterTheGateAnswers503(t *testing.T) {
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.GlobalDailyCreates = 1
	h := newHarness(t, cfg)
	day := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	inner := h.store
	h.store = &fillingStore{Store: inner, fill: func() {
		if _, ok, err := inner.RateReserve(context.Background(), store.RateHit{Key: server.GlobalCreatesKey, WindowStart: day, PurgeAt: day.Add(48 * time.Hour), Limit: 1}); err != nil || !ok {
			t.Error(ok, err)
		}
	}}
	h.reopen(h.cfg)
	m := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	if resp, _ := h.create(m); resp.StatusCode != 503 {
		t.Fatalf("a create that lost its reservation: %d", resp.StatusCode)
	}
	if c, _ := inner.RateGet(t.Context(), server.GlobalCreatesKey, day); c.Count != 1 {
		t.Fatalf("the cap was exceeded: %+v", c)
	}
	if resp, _ := h.metadata(m); resp.StatusCode != 404 {
		t.Fatalf("a refused create stored a row: %d", resp.StatusCode)
	}
}

func TestOverBudgetReportWithAFullFileIsAccepted(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
	fresh := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
	h.mustCreate(m)
	h.mustCreate(fresh)
	report := func(msg *message, ip string) int {
		body, _ := json.Marshal(map[string]string{"id": msg.id.String(), "read_token": msg.readToken, "reason": "spam"})
		resp, _ := h.do(request{method: "POST", path: "/api/v1/abuse", ip: ip, body: body, headers: map[string]string{"Content-Type": "application/json"}})
		return resp.StatusCode
	}
	for i := range 3 {
		if code := report(m, fmt.Sprintf("198.51.7.%d", i+1)); code != 202 {
			t.Fatalf("report %d: %d", i+1, code)
		}
	}
	if err := os.Truncate(filepath.Join(h.dir, "abuse-reports.jsonl"), 16<<20); err != nil {
		t.Fatal(err)
	}
	if code := report(m, "198.51.7.9"); code != 202 {
		t.Fatalf("an over-budget report with a full file: %d", code)
	}
	if code := report(fresh, "198.51.8.1"); code != 503 {
		t.Fatalf("a report that would be stored in a full file: %d", code)
	}
}

func TestClaimOpenErrorSpendsNoOpen(t *testing.T) {
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

func TestFormatCharactersCannotSplitARedactedToken(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
	h.mustCreate(m)
	secret := core.Encode(m.secret[:])
	split := secret[:15] + "​" + secret[15:30] + "­" + secret[30:]
	body, _ := json.Marshal(map[string]string{"id": m.id.String(), "read_token": m.readToken, "reason": "pasted " + split + " end"})
	if resp, _ := h.do(request{method: "POST", path: "/api/v1/abuse", ip: "203.0.113.7", body: body, headers: map[string]string{"Content-Type": "application/json"}}); resp.StatusCode != 202 {
		t.Fatalf("report: %d", resp.StatusCode)
	}
	data, err := os.ReadFile(filepath.Join(h.dir, "abuse-reports.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret[:15]) {
		t.Fatalf("a token split by format characters reached the report file: %s", data)
	}
}

func TestHTTPErrorLogReportsAndResetsDroppedLines(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	errLog := h.srv.HTTPServer().ErrorLog
	for range 40 {
		errLog.Printf("http2: server: error reading preface from client 203.0.113.99:4242: EOF")
	}
	h.clock.Advance(time.Second)
	errLog.Printf("http2: server: error reading preface from client 203.0.113.99:4242: EOF")
	h.clock.Advance(time.Second)
	errLog.Printf("http2: server: error reading preface from client 203.0.113.99:4242: EOF")
	lines := strings.Split(strings.TrimSpace(h.logBuf.String()), "\n")
	if n := len(lines); n < 2 || !strings.Contains(lines[n-2], `"dropped":20`) || !strings.Contains(lines[n-1], `"dropped":0`) {
		t.Fatalf("dropped lines must be reported once and then reset: %v", lines[max(0, len(lines)-2):])
	}
}

func TestAbuseRedactsRunsFromTwentyTwoCharacters(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
	h.mustCreate(m)
	run21, run22 := strings.Repeat("a", 21), strings.Repeat("b", 22)
	body, _ := json.Marshal(map[string]string{"id": m.id.String(), "read_token": m.readToken, "reason": run21 + " " + run22})
	if resp, _ := h.do(request{method: "POST", path: "/api/v1/abuse", ip: "203.0.113.9", body: body, headers: map[string]string{"Content-Type": "application/json"}}); resp.StatusCode != 202 {
		t.Fatalf("report: %d", resp.StatusCode)
	}
	data, err := os.ReadFile(filepath.Join(h.dir, "abuse-reports.jsonl"))
	if err != nil || !strings.Contains(string(data), `"`+run21+` [redacted]"`) {
		t.Fatalf("runs of 22 or more base64url characters must be redacted and shorter ones kept: %s %v", data, err)
	}
}

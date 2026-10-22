package server_test

import (
	"bufio"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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
	tokens := map[string]string{"no-dot": "abc", "short-counter": forge(h.clock.Now()) + ".00"}
	for name, issued := range map[string]time.Time{"zero-tag": h.clock.Now(), "future-window": h.clock.Now().Add(time.Hour)} {
		token, err := server.SolvePoW(forge(issued), m.id, 1)
		if err != nil {
			t.Fatal(err)
		}
		tokens[name] = token
	}
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
	if c, err := h.store.RateGet(t.Context(), "global:d", day); err != nil || c.Count != 20 {
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
	if c, _ := h.store.RateGet(t.Context(), "global:d", day); c.Count != 20 {
		t.Fatalf("PoW failures moved the global counter: %+v", c)
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

func TestPassphraseBackoffWithoutIPScope(t *testing.T) {
	cfg := baseConfig(pepperA, nil)
	cfg.PassphraseIPScope = false
	h := newHarness(t, cfg)
	m := newMessage(t, core.KDFPBKDF2, "pw", 1, 3600, "x")
	h.mustCreate(m)
	wrong := core.Encode(make([]byte, 32))
	for i := 1; i <= 6; i++ {
		h.claim(m, wrong, fmt.Sprintf("203.0.113.%d", i))
	}
	resp, body := h.claim(m, m.verifier, "198.51.100.1")
	if resp.StatusCode != 403 || decodeJSON(t, body)["retry_after"].(float64) != 2 {
		t.Fatalf("one shared bucket must apply the backoff across addresses: %d %s", resp.StatusCode, body)
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
	conn, err := net.Dial("tcp", h.ts.Listener.Addr().String())
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
	sweeper, err := server.NewSweeper(cfg, h.store, h.blobs, nil)
	if err != nil {
		t.Fatal(err)
	}
	report, err := sweeper.SweepOnce(t.Context())
	if err != nil || report.Expired != 1 || h.blobExists(m.id) {
		t.Fatalf("sweep without pepper: %+v %v", report, err)
	}
}

func TestSweepReconcilesOncePerDay(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
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
	if report, err := h.srv.SweepOnce(t.Context()); err != nil || report.OrphanedBlobs != 0 {
		t.Fatalf("second sweep of the day must not list the store: %+v %v", report, err)
	}
	h.clock.Advance(24 * time.Hour)
	if report, err := h.srv.SweepOnce(t.Context()); err != nil || report.OrphanedBlobs != 1 {
		t.Fatalf("first sweep of the next day: %+v %v", report, err)
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
		"max-ttl-over-7d":      with("NYLZO_MAX_TTL", "8d"),
		"max-ttl-over-7d-sec":  with("NYLZO_MAX_TTL", "604801"),
		"default-ttl-under-60": with("NYLZO_DEFAULT_TTL", "59"),
		"pow-concurrency-17":   with("NYLZO_POW_CONCURRENCY", "17"),
		"ip-scope-garbage":     with("NYLZO_PASSPHRASE_IP_SCOPE", "maybe"),
	} {
		if _, err := server.FromEnv(env, true); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	cfg, err := server.FromEnv(with("NYLZO_PASSPHRASE_IP_SCOPE", "off"), true)
	if err != nil || cfg.PassphraseIPScope {
		t.Fatalf("ip scope off: %v %v", err, cfg.PassphraseIPScope)
	}
	cfg, err = server.FromEnv(with("NYLZO_MAX_TTL", "604800"), true)
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

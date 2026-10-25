package server_test

import (
	"encoding/binary"
	"fmt"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server"
)

func TestAK12CreateRateLimitAndGlobalCap(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	for i := 1; i <= 20; i++ {
		m := newMessage(t, core.KDFNone, "", 1, 3600, "n")
		m.ip = "203.0.113.5"
		h.mustCreate(m)
	}
	over := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	over.ip = "203.0.113.5"
	resp, body := h.create(over)
	if resp.StatusCode != 429 || resp.Header.Get("Retry-After") == "" ||
		decodeJSON(t, body)["error"] != "rate_limited" {
		t.Fatalf("21st create: %d %s", resp.StatusCode, body)
	}
	other := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	other.ip = "198.51.100.9"
	h.mustCreate(other)
}

func TestAK12GlobalCap(t *testing.T) {
	t.Parallel()
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.GlobalDailyCreates = 2
	h := newHarness(t, cfg)
	readable := newMessage(t, core.KDFNone, "", 1, 3600, "read me")
	readable.ip = "192.0.2.1"
	h.mustCreate(readable)
	second := newMessage(t, core.KDFNone, "", 1, 3600, "two")
	second.ip = "192.0.2.2"
	resp, body := h.create(second)
	if resp.StatusCode != 403 || decodeJSON(t, body)["error"] != "pow" {
		t.Fatalf("at 50 percent PoW must be required: %d %s", resp.StatusCode, body)
	}
	_, body = h.do(request{method: "GET", path: "/api/v1/instance"})
	challenge := decodeJSON(t, body)["pow"].(map[string]any)["challenge"].(string)
	token, err := server.SolvePoW(challenge, second.id, h.cfg.PoWDifficulty)
	if err != nil {
		t.Fatal(err)
	}
	resp, body = h.do(
		request{
			method:  "POST",
			path:    "/api/v1/messages",
			body:    second.object,
			ip:      second.ip,
			headers: map[string]string{"X-Nylzo-Create": second.createHeader(0), "X-Nylzo-Pow": token},
		},
	)
	if resp.StatusCode != 201 {
		t.Fatalf("second create with pow: %d %s", resp.StatusCode, body)
	}
	third := newMessage(t, core.KDFNone, "", 1, 3600, "three")
	third.ip = "192.0.2.3"
	resp, body = h.create(third)
	if resp.StatusCode != 503 || decodeJSON(t, body)["error"] != "unavailable" || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("over the cap: %d %s", resp.StatusCode, body)
	}
	if resp, _ := h.claim(readable, "", ""); resp.StatusCode != 200 {
		t.Fatalf("claim during cap: %d", resp.StatusCode)
	}
	h.reopen(cfg)
	_, body = h.do(request{method: "GET", path: "/api/v1/instance"})
	if decodeJSON(t, body)["pow"].(map[string]any)["required"] != true {
		t.Fatalf("instance after restart must still report pow: %s", body)
	}
}

func TestAK12ForwardedForOnlyFromTrustedProxy(t *testing.T) {
	t.Parallel()
	cfg := baseConfig(pepperA, nil)
	cfg.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	h := newHarness(t, cfg)
	for i := 1; i <= 20; i++ {
		m := newMessage(t, core.KDFNone, "", 1, 3600, "n")
		m.ip = fmt.Sprintf("203.0.113.%d", i)
		h.mustCreate(m)
	}
	spoofed := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	spoofed.ip = "203.0.113.200"
	if resp, _ := h.create(spoofed); resp.StatusCode != 429 {
		t.Fatalf("spoofed X-Forwarded-For from an untrusted peer must share the peer bucket: %d", resp.StatusCode)
	}
	trusted := baseConfig(pepperA, nil)
	trusted.TrustedProxies = append(trusted.TrustedProxies, netip.MustParsePrefix("10.0.0.0/8"))
	h.reopen(trusted)
	for i := 1; i <= 25; i++ {
		m := newMessage(t, core.KDFNone, "", 1, 3600, "n")
		m.ip = fmt.Sprintf("198.51.100.%d, 10.1.1.1", i)
		h.mustCreate(m)
	}
}

func TestAK23PrefixBuckets(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	for i := 1; i <= 20; i++ {
		m := newMessage(t, core.KDFNone, "", 1, 3600, "n")
		m.ip = fmt.Sprintf("2001:db8:1:2::%x", i)
		h.mustCreate(m)
	}
	sameSlash64 := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	sameSlash64.ip = "2001:db8:1:2:ffff::1"
	if resp, _ := h.create(sameSlash64); resp.StatusCode != 429 {
		t.Fatalf("second address in the same /64 must share the bucket: %d", resp.StatusCode)
	}
	for sub := 3; sub <= 6; sub++ {
		for i := 1; i <= 20; i++ {
			m := newMessage(t, core.KDFNone, "", 1, 3600, "n")
			m.ip = fmt.Sprintf("2001:db8:1:%d::%x", sub, i)
			h.mustCreate(m)
		}
	}
	fifth := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	fifth.ip = "2001:db8:1:7::1"
	if resp, _ := h.create(fifth); resp.StatusCode != 429 {
		t.Fatalf("101st create in the same /48 must hit the coarse bucket: %d", resp.StatusCode)
	}
	otherSite := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	otherSite.ip = "2001:db8:2:1::1"
	h.mustCreate(otherSite)
	for i := 1; i <= 20; i++ {
		m := newMessage(t, core.KDFNone, "", 1, 3600, "n")
		m.ip = fmt.Sprintf("203.0.113.%d", i)
		h.mustCreate(m)
	}
	for i := 1; i <= 80; i++ {
		m := newMessage(t, core.KDFNone, "", 1, 3600, "n")
		m.ip = fmt.Sprintf("203.0.113.%d", 20+i)
		h.mustCreate(m)
	}
	v4 := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	v4.ip = "203.0.113.250"
	if resp, _ := h.create(v4); resp.StatusCode != 429 {
		t.Fatalf("101st create in the same /24: %d", resp.StatusCode)
	}
}

func TestReadRateLimitCountsMetadataClaimBlob(t *testing.T) {
	t.Parallel()
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.ReadPerHour = 5
	h := newHarness(t, cfg)
	m := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	m.ip = "203.0.113.5"
	h.mustCreate(m)
	for i := range 5 {
		if resp, _ := h.metadata(m); resp.StatusCode != 200 {
			t.Fatalf("metadata %d: %d", i, resp.StatusCode)
		}
	}
	if resp, _ := h.metadata(m); resp.StatusCode != 429 {
		t.Fatal("sixth read not limited")
	}
	if resp, _ := h.claim(m, "", ""); resp.StatusCode != 429 {
		t.Fatal("claim shares the read limit")
	}
	m.ip = "203.0.113.6"
	if resp, _ := h.claim(m, "", ""); resp.StatusCode != 200 {
		t.Fatal("other address")
	}
}

func TestTokenBucketThrottlesAPIPaths(t *testing.T) {
	t.Parallel()
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.BucketPerSecond = 1
	cfg.Rates.BucketBurst = 3
	h := newHarness(t, cfg)
	codes := []int{}
	for i := range 5 {
		resp, _ := h.do(request{method: "GET", path: "/api/v1/instance", ip: fmt.Sprintf("203.0.%d.1", i)})
		codes = append(codes, resp.StatusCode)
	}
	if codes[0] != 200 || codes[2] != 200 || codes[3] != 429 || codes[4] != 429 {
		t.Fatalf("codes %v", codes)
	}
	if resp, _ := h.do(request{method: "GET", path: "/healthz"}); resp.StatusCode != 200 {
		t.Fatal("non-API path throttled")
	}
	h.clock.Advance(2 * time.Second)
	if resp, _ := h.do(request{method: "GET", path: "/api/v1/instance", ip: "203.0.9.1"}); resp.StatusCode != 200 {
		t.Fatal("bucket did not refill")
	}
}

func TestPoWChallengeAndSolve(t *testing.T) {
	t.Parallel()
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.GlobalDailyCreates = 8
	cfg.PoWDifficulty = 3
	h := newHarness(t, cfg)
	for i := range 4 {
		m := newMessage(t, core.KDFNone, "", 1, 3600, "n")
		m.ip = fmt.Sprintf("203.0.113.%d", i+1)
		h.mustCreate(m)
	}
	resp, body := h.do(request{method: "GET", path: "/api/v1/instance"})
	inst := decodeJSON(t, body)
	pow := inst["pow"].(map[string]any)
	if resp.StatusCode != 200 || pow["required"] != true || pow["difficulty"].(float64) != 3 ||
		pow["challenge"] == nil {
		t.Fatalf("instance: %v", inst)
	}
	m := newMessage(t, core.KDFNone, "", 1, 3600, "pow")
	m.ip = "203.0.113.9"
	if resp, _ := h.create(m); resp.StatusCode != 403 {
		t.Fatalf("create without pow: %d", resp.StatusCode)
	}
	token, err := server.SolvePoW(pow["challenge"].(string), m.id, 3)
	if err != nil {
		t.Fatal(err)
	}
	_, body = h.do(request{method: "GET", path: "/api/v1/instance"})
	other, err := server.SolvePoW(decodeJSON(t, body)["pow"].(map[string]any)["challenge"].(string), m.id, 3)
	if err != nil {
		t.Fatal(err)
	}
	bad := newMessage(t, core.KDFNone, "", 1, 3600, "pow")
	for server.PoWSatisfiesForTest(other, bad.id, 3) {
		bad = newMessage(t, core.KDFNone, "", 1, 3600, "pow")
	}
	bad.ip = "203.0.113.9"
	resp, _ = h.do(
		request{
			method:  "POST",
			path:    "/api/v1/messages",
			body:    bad.object,
			ip:      bad.ip,
			headers: map[string]string{"X-Nylzo-Create": bad.createHeader(0), "X-Nylzo-Pow": other},
		},
	)
	if resp.StatusCode != 403 {
		t.Fatalf("token bound to another id accepted: %d", resp.StatusCode)
	}
	resp, body = h.do(
		request{
			method:  "POST",
			path:    "/api/v1/messages",
			body:    m.object,
			ip:      m.ip,
			headers: map[string]string{"X-Nylzo-Create": m.createHeader(0), "X-Nylzo-Pow": token},
		},
	)
	if resp.StatusCode != 201 {
		t.Fatalf("create with pow: %d %s", resp.StatusCode, body)
	}
	_, body = h.do(request{method: "GET", path: "/api/v1/instance"})
	expiring := decodeJSON(t, body)["pow"].(map[string]any)["challenge"].(string)
	h.clock.Advance(121 * time.Second)
	stale := newMessage(t, core.KDFNone, "", 1, 3600, "pow")
	stale.ip = "203.0.113.9"
	staleToken, _ := server.SolvePoW(expiring, stale.id, 3)
	resp, _ = h.do(
		request{
			method:  "POST",
			path:    "/api/v1/messages",
			body:    stale.object,
			ip:      stale.ip,
			headers: map[string]string{"X-Nylzo-Create": stale.createHeader(0), "X-Nylzo-Pow": staleToken},
		},
	)
	if resp.StatusCode != 403 {
		t.Fatalf("expired challenge accepted: %d", resp.StatusCode)
	}
}

func TestAbuseReports(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
	h.mustCreate(m)
	report := func(id, token string) int {
		resp, _ := h.do(
			request{
				method:  "POST",
				path:    "/api/v1/abuse",
				ip:      "203.0.113.77",
				body:    []byte(fmt.Sprintf(`{"id":%q,"reason":"spam"}`, id)),
				token:   token,
				headers: map[string]string{"Content-Type": "application/json"},
			},
		)
		return resp.StatusCode
	}
	if report(m.id.String(), m.readToken) != http.StatusAccepted {
		t.Fatal("valid report")
	}
	if report(core.NewID().String(), m.readToken) != http.StatusAccepted {
		t.Fatal("unknown id must get the same 202")
	}
	for range 3 {
		report(m.id.String(), core.Encode(make([]byte, 32)))
	}
	if report(m.id.String(), m.readToken) != http.StatusTooManyRequests {
		t.Fatal("sixth report per hour must be limited")
	}
}

func TestRefusedSourceDoesNotSpendItsRange(t *testing.T) {
	t.Parallel()
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.CreatePerHour = 1
	cfg.Rates.CoarseMultiplier = 2
	h := newHarness(t, cfg)
	for i := range 4 {
		m := newMessage(t, core.KDFNone, "", 1, 3600, "n")
		m.ip = "198.51.100.1"
		resp, _ := h.create(m)
		if want := map[bool]int{true: 201, false: 429}[i == 0]; resp.StatusCode != want {
			t.Fatalf("create %d from one source: %d, want %d", i+1, resp.StatusCode, want)
		}
	}
	neighbour := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	neighbour.ip = "198.51.100.2"
	if resp, body := h.create(neighbour); resp.StatusCode != 201 {
		t.Fatalf("a refused source must not use up its /24: %d %s", resp.StatusCode, body)
	}
	third := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	third.ip = "198.51.100.3"
	if resp, _ := h.create(third); resp.StatusCode != 429 {
		t.Fatalf("the /24 budget still applies: %d", resp.StatusCode)
	}
}

func TestSourceOverItsDailyBytesDoesNotSpendItsRangeHour(t *testing.T) {
	t.Parallel()
	cfg := baseConfig(pepperA, nil)
	h := newHarness(t, cfg)
	big := strings.Repeat("n", 5000)
	probe := newMessage(t, core.KDFNone, "", 1, 3600, big)
	cfg.Rates.CreatePerHour = 2
	cfg.Rates.CoarseMultiplier = 1
	cfg.Rates.CreateBytesPerDay = int64(len(probe.object) - 1)
	h.reopen(cfg)
	for i := range 2 {
		m := newMessage(t, core.KDFNone, "", 1, 3600, big)
		m.ip = "198.51.100.1"
		if resp, _ := h.create(m); resp.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("create %d over the daily bytes: %d", i+1, resp.StatusCode)
		}
	}
	neighbour := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	neighbour.ip = "198.51.100.2"
	if resp, body := h.create(neighbour); resp.StatusCode != http.StatusCreated {
		t.Fatalf("a source refused by its daily limit must not spend its /24 hour: %d %s", resp.StatusCode, body)
	}
}

func TestRefusedReaderDoesNotSpendItsRange(t *testing.T) {
	t.Parallel()
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.ReadPerHour = 1
	cfg.Rates.CoarseMultiplier = 2
	h := newHarness(t, cfg)
	m := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	h.mustCreate(m)
	read := func(ip string) int {
		reader := *m
		reader.ip = ip
		resp, _ := h.metadata(&reader)
		return resp.StatusCode
	}
	for i := range 4 {
		if want := map[bool]int{true: 200, false: 429}[i == 0]; read("198.51.100.1") != want {
			t.Fatalf("read %d from one source, want %d", i+1, want)
		}
	}
	if code := read("198.51.100.2"); code != http.StatusOK {
		t.Fatalf("a refused reader must not use up its /24: %d", code)
	}
	if code := read("198.51.100.3"); code != http.StatusTooManyRequests {
		t.Fatalf("the /24 read budget still applies: %d", code)
	}
}

func TestRetryAfterPointsToTheRefusingWindow(t *testing.T) {
	t.Parallel()
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.CreatePerHour = 1
	h := newHarness(t, cfg)
	h.mustCreate(newMessage(t, core.KDFNone, "", 1, 3600, "a"))
	resp, body := h.create(newMessage(t, core.KDFNone, "", 1, 3600, "b"))
	if resp.StatusCode != http.StatusTooManyRequests || resp.Header.Get("Retry-After") != "3600" ||
		decodeJSON(t, body)["retry_after"] != float64(3600) {
		t.Fatalf(
			"an hourly refusal at 10:00 must point to 11:00: %d %q %s",
			resp.StatusCode,
			resp.Header.Get("Retry-After"),
			body,
		)
	}
	resp, _ = h.do(
		request{method: "GET", path: "/api/v1/instance", headers: map[string]string{"Sec-Purpose": "prefetch"}},
	)
	if resp.StatusCode != http.StatusServiceUnavailable || resp.Header.Get("Retry-After") != "60" {
		t.Fatalf("prefetch: %d %q", resp.StatusCode, resp.Header.Get("Retry-After"))
	}
}

func TestPoWRejectsForgedAndMalformedTokens(t *testing.T) {
	t.Parallel()
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
		resp, body := h.do(
			request{
				method:  "POST",
				path:    "/api/v1/messages",
				body:    m.object,
				ip:      m.ip,
				headers: map[string]string{"X-Nylzo-Create": m.createHeader(0), "X-Nylzo-Pow": token},
			},
		)
		if resp.StatusCode != 403 || decodeJSON(t, body)["error"] != "pow" {
			t.Fatalf("%s: %d %s", name, resp.StatusCode, body)
		}
	}
}

func TestPoWTokenIsSingleUse(t *testing.T) {
	t.Parallel()
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
	badHeader := mutateHeader(
		t,
		m,
		func(f map[string]any) { f["kdf_id"] = 1; f["verifier_key"] = publicKey(core.NewSecret()) },
	)
	send := func(header, pow string) int {
		resp, _ := h.do(
			request{
				method:  "POST",
				path:    "/api/v1/messages",
				body:    m.object,
				ip:      m.ip,
				headers: map[string]string{"X-Nylzo-Create": header, "X-Nylzo-Pow": pow},
			},
		)
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
	if resp, _ := h.do(
		request{
			method:  "POST",
			path:    "/api/v1/messages",
			body:    other.object,
			ip:      other.ip,
			headers: map[string]string{"X-Nylzo-Create": other.createHeader(0), "X-Nylzo-Pow": reuse},
		},
	); resp.StatusCode != 403 {
		t.Fatalf("a second token for a used challenge: %d", resp.StatusCode)
	}
	if code := send(m.createHeader(0), solve(m)); code != 201 {
		t.Fatalf("fresh challenge: %d", code)
	}
}

func TestCreateBytesPerDayLimit(t *testing.T) {
	t.Parallel()
	cfg := baseConfig(pepperA, nil)
	h := newHarness(t, cfg)
	probe := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	cfg.Rates.CreateBytesPerDay = int64(2*len(probe.object) + 1)
	h.reopen(cfg)
	for range 2 {
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
	t.Parallel()
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

func TestSourceLimiterIsolatesClients(t *testing.T) {
	t.Parallel()
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.SourcePerSecond = 1
	cfg.Rates.SourceBurst = 2
	h := newHarness(t, cfg)
	get := func(ip string) int {
		resp, _ := h.do(request{method: "GET", path: "/api/v1/instance", ip: ip})
		return resp.StatusCode
	}
	if codes := []int{
		get("203.0.113.1"),
		get("203.0.113.1"),
		get("203.0.113.1"),
	}; !slices.Equal(
		codes,
		[]int{200, 200, 429},
	) {
		t.Fatalf("one source must be limited after its burst: %v", codes)
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

func TestMalformedIDsDoNotSpendReadLimit(t *testing.T) {
	t.Parallel()
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
		if resp, _ := h.do(request{method: method, path: path, ip: m.ip, token: m.readToken}); resp.StatusCode != 404 {
			t.Fatalf("%s %s: %d", method, path, resp.StatusCode)
		}
	}
	if resp, _ := h.metadata(m); resp.StatusCode != 200 {
		t.Fatalf("malformed ids spent the read limit: %d", resp.StatusCode)
	}
}

func TestRefusedSourceTakesNoInstanceToken(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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

func TestRefusedPrefixTakesNoInstanceToken(t *testing.T) {
	t.Parallel()
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.BucketPerSecond, cfg.Rates.BucketBurst = 10, 10
	h := newHarness(t, cfg)
	for i := range 20 {
		h.do(request{method: "GET", path: "/api/v1/instance", ip: fmt.Sprintf("2001:db8:1:%x::1", i)})
	}
	for i := range 5 {
		if resp, _ := h.do(
			request{method: "GET", path: "/api/v1/instance", ip: fmt.Sprintf("2001:db8:%x::1", i+2)},
		); resp.StatusCode != 200 {
			t.Fatalf("a refused /48 drained the instance bucket: request %d got %d", i, resp.StatusCode)
		}
	}
}

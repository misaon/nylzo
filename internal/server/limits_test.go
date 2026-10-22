package server_test

import (
	"fmt"
	"net/http"
	"net/netip"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server"
)

func TestAK12CreateRateLimitAndGlobalCap(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	for i := 1; i <= 20; i++ {
		m := newMessage(t, core.KDFNone, "", 1, 3600, "n")
		m.ip = "203.0.113.5"
		h.mustCreate(m)
	}
	over := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	over.ip = "203.0.113.5"
	resp, body := h.create(over)
	if resp.StatusCode != 429 || resp.Header.Get("Retry-After") == "" || decodeJSON(t, body)["error"] != "rate_limited" {
		t.Fatalf("21st create: %d %s", resp.StatusCode, body)
	}
	other := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	other.ip = "198.51.100.9"
	h.mustCreate(other)
}

func TestAK12GlobalCap(t *testing.T) {
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
	resp, body = h.do(request{method: "POST", path: "/api/v1/messages", body: second.object, ip: second.ip, headers: map[string]string{"X-Nylzo-Create": second.createHeader(0), "X-Nylzo-Pow": token}})
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
	resp, body = h.do(request{method: "GET", path: "/api/v1/instance"})
	if decodeJSON(t, body)["pow"].(map[string]any)["required"] != true {
		t.Fatalf("instance after restart must still report pow: %s", body)
	}
}

func TestAK12ForwardedForOnlyFromTrustedProxy(t *testing.T) {
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
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.ReadPerHour = 5
	h := newHarness(t, cfg)
	m := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	m.ip = "203.0.113.5"
	h.mustCreate(m)
	for i := 0; i < 5; i++ {
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
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.BucketPerSecond = 1
	cfg.Rates.BucketBurst = 3
	h := newHarness(t, cfg)
	codes := []int{}
	for i := 0; i < 5; i++ {
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
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.GlobalDailyCreates = 8
	cfg.PoWDifficulty = 3
	h := newHarness(t, cfg)
	for i := 0; i < 4; i++ {
		m := newMessage(t, core.KDFNone, "", 1, 3600, "n")
		m.ip = fmt.Sprintf("203.0.113.%d", i+1)
		h.mustCreate(m)
	}
	resp, body := h.do(request{method: "GET", path: "/api/v1/instance"})
	inst := decodeJSON(t, body)
	pow := inst["pow"].(map[string]any)
	if resp.StatusCode != 200 || pow["required"] != true || pow["difficulty"].(float64) != 3 || pow["challenge"] == nil {
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
	resp, _ = h.do(request{method: "POST", path: "/api/v1/messages", body: bad.object, ip: bad.ip, headers: map[string]string{"X-Nylzo-Create": bad.createHeader(0), "X-Nylzo-Pow": other}})
	if resp.StatusCode != 403 {
		t.Fatalf("token bound to another id accepted: %d", resp.StatusCode)
	}
	resp, body = h.do(request{method: "POST", path: "/api/v1/messages", body: m.object, ip: m.ip, headers: map[string]string{"X-Nylzo-Create": m.createHeader(0), "X-Nylzo-Pow": token}})
	if resp.StatusCode != 201 {
		t.Fatalf("create with pow: %d %s", resp.StatusCode, body)
	}
	h.clock.Advance(121 * time.Second)
	stale := newMessage(t, core.KDFNone, "", 1, 3600, "pow")
	stale.ip = "203.0.113.9"
	staleToken, _ := server.SolvePoW(pow["challenge"].(string), stale.id, 3)
	resp, _ = h.do(request{method: "POST", path: "/api/v1/messages", body: stale.object, ip: stale.ip, headers: map[string]string{"X-Nylzo-Create": stale.createHeader(0), "X-Nylzo-Pow": staleToken}})
	if resp.StatusCode != 403 {
		t.Fatalf("expired challenge accepted: %d", resp.StatusCode)
	}
}

func TestAbuseReports(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
	h.mustCreate(m)
	report := func(id, token string) int {
		resp, _ := h.do(request{method: "POST", path: "/api/v1/abuse", ip: "203.0.113.77", body: []byte(fmt.Sprintf(`{"id":%q,"read_token":%q,"reason":"spam"}`, id, token)), headers: map[string]string{"Content-Type": "application/json"}})
		return resp.StatusCode
	}
	if report(m.id.String(), m.readToken) != http.StatusAccepted {
		t.Fatal("valid report")
	}
	if report(core.NewID().String(), m.readToken) != http.StatusAccepted {
		t.Fatal("unknown id must get the same 202")
	}
	for i := 0; i < 3; i++ {
		report(m.id.String(), core.Encode(make([]byte, 32)))
	}
	if report(m.id.String(), m.readToken) != http.StatusTooManyRequests {
		t.Fatal("sixth report per hour must be limited")
	}
}

func TestRefusedSourceDoesNotSpendItsRange(t *testing.T) {
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

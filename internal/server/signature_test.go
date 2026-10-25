package server_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
)

func badSignature(seed *[core.TokenLen]byte, purpose, method, path string, challenge [core.ChallengeLen]byte) string {
	sig := core.SignRequest(seed, purpose, method, path, challenge, nil)
	sig.Sig[0] ^= 1
	return sig.Authorization()
}

func openDB(t *testing.T, h *harness) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(h.dir, "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func abuseLines(t *testing.T, h *harness, id core.ID) int {
	t.Helper()
	data, err := h.abuseReports()
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return strings.Count(string(data), id.String())
}

func TestSignedRequestsAreSingleUse(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 2, 3600, "single use")
	h.mustCreate(m)
	path := "/api/v1/messages/" + m.id.String()
	unknown, notFound := h.do(
		signedRequest(
			&m.link.R,
			core.PurposeRead,
			"GET",
			"/api/v1/messages/"+core.NewID().String(),
			h.srv.ChallengeForTest(),
			nil,
		),
	)
	if unknown.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown id: %d", unknown.StatusCode)
	}
	replayed := func(name string, req request, first int) {
		t.Helper()
		if resp, body := h.do(req); resp.StatusCode != first {
			t.Fatalf("%s: %d %s", name, resp.StatusCode, body)
		}
		resp, body := h.do(req)
		if resp.StatusCode != http.StatusNotFound || string(body) != string(notFound) ||
			!sameHeaders(headersWithoutDate(resp.Header), headersWithoutDate(unknown.Header)) {
			t.Fatalf("%s replay: %d %s %v", name, resp.StatusCode, body, resp.Header)
		}
	}
	replayed(
		"metadata",
		signedRequest(&m.link.R, core.PurposeRead, "GET", path, h.srv.ChallengeForTest(), nil),
		http.StatusOK,
	)
	replayed(
		"status",
		signedRequest(m.manage, core.PurposeManage, "GET", path+"/status", h.srv.ChallengeForTest(), nil),
		http.StatusOK,
	)
	replayed(
		"claim",
		signedRequest(&m.link.R, core.PurposeRead, "POST", path+"/claim", h.srv.ChallengeForTest(), nil),
		http.StatusOK,
	)
	if _, st := h.status(m); st["opens_used"] != 1.0 {
		t.Fatalf("a replayed claim spent an open: %v", st)
	}
	replayed(
		"burn",
		signedRequest(&m.link.R, core.PurposeRead, "POST", path+"/burn", h.srv.ChallengeForTest(), nil),
		http.StatusNoContent,
	)

	other := newMessage(t, core.KDFNone, "", 1, 3600, "cancel once")
	h.mustCreate(other)
	replayed(
		"cancel",
		signedRequest(
			other.manage,
			core.PurposeManage,
			"POST",
			"/api/v1/messages/"+other.id.String()+"/cancel",
			h.srv.ChallengeForTest(),
			nil,
		),
		http.StatusNoContent,
	)

	reported := newMessage(t, core.KDFNone, "", 1, 3600, "reported once")
	h.mustCreate(reported)
	body := []byte(`{"id":"` + reported.id.String() + `","reason":"spam"}`)
	report := signedRequest(&reported.link.R, core.PurposeRead, "POST", "/api/v1/abuse", h.srv.ChallengeForTest(), body)
	report.headers["Content-Type"] = "application/json"
	for range 2 {
		if resp, _ := h.do(report); resp.StatusCode != http.StatusAccepted {
			t.Fatalf("abuse report: %d", resp.StatusCode)
		}
	}
	if n := abuseLines(t, h, reported.id); n != 1 {
		t.Fatalf("a replayed abuse report was stored %d times", n)
	}
}

func TestSignatureBindsTheRequest(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 3600, "bound")
	h.mustCreate(m)
	path := "/api/v1/messages/" + m.id.String()
	moved := func(method, to string) request {
		req := signedRequest(&m.link.R, core.PurposeRead, "GET", path, h.srv.ChallengeForTest(), nil)
		req.method, req.path = method, to
		return req
	}
	for name, req := range map[string]request{
		"blob path":  moved("GET", path+"/blob"),
		"claim":      moved("POST", path+"/claim"),
		"burn":       moved("POST", path+"/burn"),
		"status":     moved("GET", path+"/status"),
		"manage key": signedRequest(&m.link.R, core.PurposeManage, "GET", path+"/status", h.srv.ChallengeForTest(), nil),
		"read as M":  signedRequest(m.manage, core.PurposeRead, "GET", path, h.srv.ChallengeForTest(), nil),
		"bad sig":    rawAuth(request{method: "GET", path: path}, badSignature(&m.link.R, core.PurposeRead, "GET", path, h.srv.ChallengeForTest())),
	} {
		if resp, body := h.do(req); resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s: %d %s", name, resp.StatusCode, body)
		}
	}
	if _, st := h.status(m); st["state"] != "available" || st["opens_used"] != 0.0 {
		t.Fatalf("a signature moved to another request changed the message: %v", st)
	}
	spent := moved("POST", path+"/burn")
	h.do(spent)
	spent.method, spent.path = "GET", path
	if resp, _ := h.do(spent); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("a failed attempt must spend its challenge: %d", resp.StatusCode)
	}
	if resp, body := h.do(
		signedRequest(&m.link.R, core.PurposeRead, "GET", path, h.srv.ChallengeForTest(), nil),
	); resp.StatusCode != http.StatusOK ||
		!strings.Contains(string(body), `"state":"available"`) {
		t.Fatalf("a fresh signature: %d %s", resp.StatusCode, body)
	}

	body := []byte(`{"id":"` + m.id.String() + `","reason":"first"}`)
	tampered := signedRequest(&m.link.R, core.PurposeRead, "POST", "/api/v1/abuse", h.srv.ChallengeForTest(), body)
	tampered.body = []byte(`{"id":"` + m.id.String() + `","reason":"other"}`)
	tampered.headers["Content-Type"] = "application/json"
	if resp, _ := h.do(tampered); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("tampered abuse report: %d", resp.StatusCode)
	}
	if n := abuseLines(t, h, m.id); n != 0 {
		t.Fatal("an abuse report with a changed body was stored")
	}
}

func TestVerifierSignatureIsBoundToTheClaim(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	const pass = "correct horse battery staple"
	m := newMessage(t, core.KDFScrypt, pass, 1, 3600, "behind a passphrase")
	h.mustCreate(m)
	path := "/api/v1/messages/" + m.id.String() + "/claim"
	stranger := core.NewSecret()
	pkV := core.PublicKey(&m.content.V)
	cases := []struct {
		name   string
		header func(challenge [core.ChallengeLen]byte) string
	}{
		{"other challenge", func([core.ChallengeLen]byte) string {
			return core.SignRequest(&m.content.V, core.PurposeVerifier, "POST", path, h.srv.ChallengeForTest(), nil).
				VerifierHeader()
		}},
		{"read purpose", func(c [core.ChallengeLen]byte) string {
			return core.SignRequest(&m.content.V, core.PurposeRead, "POST", path, c, nil).VerifierHeader()
		}},
		{"other path", func(c [core.ChallengeLen]byte) string {
			return core.SignRequest(&m.content.V, core.PurposeVerifier, "POST", path+"x", c, nil).VerifierHeader()
		}},
		{"right key, foreign signature", func(c [core.ChallengeLen]byte) string {
			sig := core.SignRequest(stranger, core.PurposeVerifier, "POST", path, c, nil)
			sig.PublicKey = pkV
			return sig.VerifierHeader()
		}},
	}
	for i, c := range cases {
		challenge := h.srv.ChallengeForTest()
		req := signedRequest(&m.link.R, core.PurposeRead, "POST", path, challenge, nil)
		req.headers["X-Nylzo-Verifier"] = c.header(challenge)
		resp, body := h.do(req)
		var got passphraseResponse
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("%s: %d %s: %v", c.name, resp.StatusCode, body, err)
		}
		if resp.StatusCode != http.StatusForbidden || got.AttemptsLeft != 4-i {
			t.Fatalf("%s: %d %+v", c.name, resp.StatusCode, got)
		}
	}
	challenge := h.srv.ChallengeForTest()
	req := signedRequest(&m.link.R, core.PurposeRead, "POST", path, challenge, nil)
	req.headers["X-Nylzo-Verifier"] = core.SignRequest(&m.content.V, core.PurposeVerifier, "POST", path, challenge, nil).
		VerifierHeader()
	if resp, _ := h.do(req); resp.StatusCode != http.StatusOK {
		t.Fatalf("correct verifier: %d", resp.StatusCode)
	}
}

type passphraseResponse struct {
	Error        string `json:"error"`
	RetryAfter   int    `json:"retry_after"`
	AttemptsLeft int    `json:"attempts_left"`
}

func TestChallengeWindowAndRotation(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 3600, "challenges")
	h.mustCreate(m)
	path := "/api/v1/messages/" + m.id.String()

	resp, body := h.do(request{method: "GET", path: "/api/v1/challenge"})
	var issued struct {
		Challenge string `json:"challenge"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal(body, &issued); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("challenge: %d %s", resp.StatusCode, body)
	}
	raw, err := core.DecodeExact(issued.Challenge, core.ChallengeLen)
	if err != nil || len(issued.Challenge) != 48 ||
		issued.ExpiresAt != h.clock.Now().Add(time.Minute).UTC().Format(time.RFC3339) {
		t.Fatalf("challenge shape: %+v", issued)
	}
	if resp, _ := h.do(
		signedRequest(&m.link.R, core.PurposeRead, "GET", path, [core.ChallengeLen]byte(raw), nil),
	); resp.StatusCode != http.StatusOK {
		t.Fatalf("issued challenge: %d", resp.StatusCode)
	}

	now := h.clock.Now()
	forged := h.srv.ChallengeForTest()
	forged[core.ChallengeLen-1] ^= 1
	for name, c := range map[string]struct {
		challenge [core.ChallengeLen]byte
		status    int
	}{
		"issued 60 s ago":    {h.srv.ChallengeAtForTest(now.Add(-60 * time.Second)), http.StatusOK},
		"issued 61 s ago":    {h.srv.ChallengeAtForTest(now.Add(-61 * time.Second)), http.StatusNotFound},
		"issued 5 s ahead":   {h.srv.ChallengeAtForTest(now.Add(5 * time.Second)), http.StatusOK},
		"issued 6 s ahead":   {h.srv.ChallengeAtForTest(now.Add(6 * time.Second)), http.StatusNotFound},
		"forged tag":         {forged, http.StatusNotFound},
		"all zero challenge": {[core.ChallengeLen]byte{}, http.StatusNotFound},
	} {
		if resp, body := h.do(
			signedRequest(&m.link.R, core.PurposeRead, "GET", path, c.challenge, nil),
		); resp.StatusCode != c.status {
			t.Fatalf("%s: %d %s", name, resp.StatusCode, body)
		}
	}
	for name, auth := range map[string]string{
		"bearer R":        "Bearer " + m.readToken,
		"lowercase":       strings.ToLower(core.AuthScheme) + " x",
		"two parts":       core.AuthScheme + " " + publicKey(&m.link.R) + "." + core.Encode(raw),
		"padded":          core.AuthScheme + " " + publicKey(&m.link.R) + "=.x.y",
		"missing":         "",
		"double space":    strings.Replace(signedRequest(&m.link.R, core.PurposeRead, "GET", path, h.srv.ChallengeForTest(), nil).headers["Authorization"], " ", "  ", 1),
		"trailing period": signedRequest(&m.link.R, core.PurposeRead, "GET", path, h.srv.ChallengeForTest(), nil).headers["Authorization"] + ".",
	} {
		if resp, _ := h.do(rawAuth(request{method: "GET", path: path}, auth)); resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s: %d", name, resp.StatusCode)
		}
	}

	before := h.srv.ChallengeForTest()
	h.reopen(baseConfig(pepperB, pepperA))
	if resp, _ := h.do(
		signedRequest(&m.link.R, core.PurposeRead, "GET", path, before, nil),
	); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("a challenge from before the rotation: %d", resp.StatusCode)
	}
	if resp, _ := h.do(
		signedRequest(&m.link.R, core.PurposeRead, "GET", path, h.srv.ChallengeForTest(), nil),
	); resp.StatusCode != http.StatusOK {
		t.Fatalf("a fresh challenge after the rotation: %d", resp.StatusCode)
	}
}

func TestSignedEndpointsRequireAnEmptyBody(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 3600, "no bodies")
	h.mustCreate(m)
	path := "/api/v1/messages/" + m.id.String()
	for _, e := range []struct {
		method, suffix, purpose string
		seed                    *[core.TokenLen]byte
	}{
		{"GET", "", core.PurposeRead, &m.link.R},
		{"POST", "/claim", core.PurposeRead, &m.link.R},
		{"GET", "/blob", core.PurposeRead, &m.link.R},
		{"POST", "/ack", core.PurposeRead, &m.link.R},
		{"POST", "/burn", core.PurposeRead, &m.link.R},
		{"GET", "/status", core.PurposeManage, m.manage},
		{"POST", "/cancel", core.PurposeManage, m.manage},
	} {
		req := signedRequest(e.seed, e.purpose, e.method, path+e.suffix, h.srv.ChallengeForTest(), []byte("x"))
		if resp, _ := h.do(req); resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s %s with a body: %d", e.method, e.suffix, resp.StatusCode)
		}
	}
	if _, st := h.status(m); st["state"] != "available" || st["opens_used"] != 0.0 {
		t.Fatalf("requests with a body changed the message: %v", st)
	}
}

func TestSpentChallengesExpire(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 3600, "sweep")
	h.mustCreate(m)
	path := "/api/v1/messages/" + m.id.String()
	challenge := h.srv.ChallengeForTest()
	if resp, _ := h.do(
		signedRequest(&m.link.R, core.PurposeRead, "GET", path, challenge, nil),
	); resp.StatusCode != http.StatusOK {
		t.Fatalf("metadata: %d", resp.StatusCode)
	}
	spentRows := func() int {
		t.Helper()
		db := openDB(t, h)
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM rate_counters WHERE key LIKE 'challenge:%'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if spentRows() != 1 {
		t.Fatal("a passed request must record its challenge")
	}
	h.clock.Advance(time.Minute)
	if _, err := h.srv.SweepOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if spentRows() != 1 {
		t.Fatal("a sweep dropped a challenge that could still be replayed by a slower instance")
	}
	h.clock.Advance(2 * time.Minute)
	if _, err := h.srv.SweepOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if spentRows() != 0 {
		t.Fatal("the sweep kept a spent challenge after it expired")
	}
}

package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"filippo.io/edwards25519"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/server/config"
	"github.com/misaon/nylzo/internal/store"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

func headersWithoutDate(harness http.Header) map[string][]string {
	out := map[string][]string{}
	for k, v := range harness {
		if k == "Date" {
			continue
		}
		out[k] = v
	}
	return out
}

func sameHeaders(a, b map[string][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok || len(va) != len(vb) {
			return false
		}
		for i := range va {
			if va[i] != vb[i] {
				return false
			}
		}
	}
	return true
}

const medianSlackPercent = 115

func TestAK21UniformNotFoundAndConflict(t *testing.T) {
	settings := baseSettings(pepperA, nil)
	settings.Rates.ReadPerHour = 1 << 30
	settings.Rates.BucketPerSecond = 1 << 20
	settings.Rates.BucketBurst = 1 << 20
	harness := newHarness(t, settings)
	live := newMessage(t, core.KDFNone, "", 1, 3600, "live")
	harness.mustCreate(live)
	ended := newMessage(t, core.KDFNone, "", 1, 3600, "ended")
	harness.mustCreate(ended)
	if response, _ := harness.do(
		&testRequest{
			method: http.MethodPost,
			path:   "/api/v1/messages/" + ended.id.String() + "/burn",
			token:  ended.readToken,
		},
	); response.StatusCode != http.StatusNoContent {
		t.Fatalf("burn: %d", response.StatusCode)
	}
	if record, err := harness.store.Get(t.Context(), ended.id.String()); err != nil || record.Tombstone == nil {
		t.Fatal("the ended message must be a tombstone before probing 409")
	}
	wrongSeed := core.NewSecret()
	livePath := "/api/v1/messages/" + live.id.String()
	var offCurve [core.PublicKeyLen]byte
	for i := byte(2); ; i++ {
		offCurve[0] = i
		if _, err := new(edwards25519.Point).SetBytes(offCurve[:]); err != nil {
			break
		}
	}
	classes := []timingClass{
		{"unknown-id", "metadata", func() *testRequest {
			path := "/api/v1/messages/" + core.NewID().String()
			return signedRequest(wrongSeed, core.PurposeRead, "GET", path, harness.challenge(), nil)
		}},
		{"wrong-R", "metadata", func() *testRequest {
			return signedRequest(wrongSeed, core.PurposeRead, "GET", livePath, harness.challenge(), nil)
		}},
		{"unknown-id", "status", func() *testRequest {
			path := "/api/v1/messages/" + core.NewID().String() + "/status"
			return signedRequest(wrongSeed, core.PurposeManage, "GET", path, harness.challenge(), nil)
		}},
		{"wrong-M", "status", func() *testRequest {
			return signedRequest(
				wrongSeed,
				core.PurposeManage,
				"GET",
				livePath+"/status",
				harness.challenge(),
				nil,
			)
		}},
		{"bad-sig", "metadata", func() *testRequest {
			return rawAuth(
				&testRequest{method: http.MethodGet, path: livePath},
				badSignature(&live.link.R, core.PurposeRead, "GET", livePath, harness.challenge()),
			)
		}},
		{"off-curve", "metadata", func() *testRequest {
			signature := core.SignRequest(wrongSeed, core.PurposeRead, "GET", livePath, harness.challenge(), nil)
			signature.PublicKey = offCurve
			return rawAuth(&testRequest{method: http.MethodGet, path: livePath}, signature.Authorization())
		}},
		{"stale", "metadata", func() *testRequest {
			return signedRequest(
				&live.link.R,
				core.PurposeRead,
				"GET",
				livePath,
				harness.challengeAt(harness.clock.Now().Add(-2*time.Minute)),
				nil,
			)
		}},
		{"malformed", "metadata", func() *testRequest {
			return rawAuth(&testRequest{method: http.MethodGet, path: livePath}, core.AuthScheme+" x.y.z")
		}},
		{"bad-sig", "status", func() *testRequest {
			return rawAuth(
				&testRequest{method: http.MethodGet, path: livePath + "/status"},
				badSignature(live.manage, core.PurposeManage, "GET", livePath+"/status", harness.challenge()),
			)
		}},
	}
	var refBody []byte
	var refHeaders map[string][]string
	for _, c := range classes {
		response, body := harness.do(c.request())
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("%s: %d", c.name, response.StatusCode)
		}
		hdr := headersWithoutDate(response.Header)
		if refBody == nil {
			refBody, refHeaders = body, hdr
			continue
		}
		if string(body) != string(refBody) || !sameHeaders(hdr, refHeaders) {
			t.Fatalf("%s differs: %s %v vs %s %v", c.name, body, hdr, refBody, refHeaders)
		}
	}
	for _, message := range []*testMessage{live, ended} {
		response, body := harness.create(message)
		if response.StatusCode != http.StatusConflict || string(body) != `{"error":"conflict"}` {
			t.Fatalf("409 for %s: %d %s", message.text, response.StatusCode, body)
		}
	}
	var failure string
	for range 2 {
		if failure = measureUniformity(t, harness, classes); failure == "" {
			return
		}
		t.Logf("AK21 measuring again after: %s", failure)
	}
	t.Fatal(failure)
}

type timingClass struct {
	name     string
	endpoint string
	request  func() *testRequest
}

func measureUniformity(t *testing.T, harness *testHarness, classes []timingClass) string {
	t.Helper()
	const samples = 1000
	durations := make([][]time.Duration, len(classes))
	for i := range samples {
		for ci, c := range classes {
			request := c.request()
			start := time.Now()
			response, _ := harness.do(request)
			durations[ci] = append(durations[ci], time.Since(start))
			if response.StatusCode != http.StatusNotFound {
				t.Fatalf("%s sample %d: %d", c.name, i, response.StatusCode)
			}
		}
	}
	medians := make([]time.Duration, len(classes))
	p95s := make([]time.Duration, len(classes))
	for ci := range classes {
		d := durations[ci]
		slices.Sort(d)
		medians[ci] = d[len(d)/2]
		p95s[ci] = d[len(d)*95/100]
		t.Logf("AK21 %-8s %-10s median %s p95 %s", classes[ci].endpoint, classes[ci].name, medians[ci], p95s[ci])
	}
	spendsNoChallenge := map[string]bool{"stale": true, "malformed": true}
	minMedian, minP95 := map[string]time.Duration{}, map[string]time.Duration{}
	for ci, c := range classes {
		if spendsNoChallenge[c.name] {
			continue
		}
		if message, ok := minMedian[c.endpoint]; !ok || medians[ci] < message {
			minMedian[c.endpoint] = medians[ci]
		}
		if p, ok := minP95[c.endpoint]; !ok || p95s[ci] < p {
			minP95[c.endpoint] = p95s[ci]
		}
	}
	for ci, c := range classes {
		if spendsNoChallenge[c.name] {
			continue
		}
		// gstack-timing-ok: AK21 timing uniformity is the contract under test (eng review D2, D23)
		if medians[ci]*100 > minMedian[c.endpoint]*medianSlackPercent {
			return fmt.Sprintf(
				"%s %s median %s exceeds %d%% of the fastest equal-work class %s on the same endpoint",
				c.endpoint,
				c.name,
				medians[ci],
				medianSlackPercent,
				minMedian[c.endpoint],
			)
		}
		// gstack-timing-ok: AK21 timing uniformity is the contract under test (eng review D2, D23)
		if p95s[ci] > 5*minP95[c.endpoint] {
			return fmt.Sprintf(
				"%s %s p95 %s exceeds 5x the fastest equal-work class %s on the same endpoint",
				c.endpoint,
				c.name,
				p95s[ci],
				minP95[c.endpoint],
			)
		}
	}
	return ""
}

func badSignature(seed *[core.TokenLen]byte, purpose, method, path string, challenge [core.ChallengeLen]byte) string {
	signature := core.SignRequest(seed, purpose, method, path, challenge, nil)
	signature.Sig[0] ^= 1
	return signature.Authorization()
}

func openDB(t *testing.T, harness *testHarness) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(harness.dir, "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func abuseLines(t *testing.T, harness *testHarness, id core.ID) int {
	t.Helper()
	data, err := harness.abuseReports()
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return strings.Count(string(data), id.String())
}

func TestSignedRequestsAreSingleUse(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	message := newMessage(t, core.KDFNone, "", 2, 3600, "single use")
	harness.mustCreate(message)
	path := "/api/v1/messages/" + message.id.String()
	unknown, notFound := harness.do(
		signedRequest(
			&message.link.R,
			core.PurposeRead,
			"GET",
			"/api/v1/messages/"+core.NewID().String(),
			harness.challenge(),
			nil,
		),
	)
	if unknown.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown id: %d", unknown.StatusCode)
	}
	replayed := func(name string, request *testRequest, first int) {
		t.Helper()
		if response, body := harness.do(request); response.StatusCode != first {
			t.Fatalf("%s: %d %s", name, response.StatusCode, body)
		}
		response, body := harness.do(request)
		if response.StatusCode != http.StatusNotFound || string(body) != string(notFound) ||
			!sameHeaders(headersWithoutDate(response.Header), headersWithoutDate(unknown.Header)) {
			t.Fatalf("%s replay: %d %s %v", name, response.StatusCode, body, response.Header)
		}
	}
	replayed(
		"metadata",
		signedRequest(&message.link.R, core.PurposeRead, "GET", path, harness.challenge(), nil),
		http.StatusOK,
	)
	replayed(
		"status",
		signedRequest(message.manage, core.PurposeManage, "GET", path+"/status", harness.challenge(), nil),
		http.StatusOK,
	)
	replayed(
		"claim",
		signedRequest(&message.link.R, core.PurposeRead, "POST", path+"/claim", harness.challenge(), nil),
		http.StatusOK,
	)
	if _, status := harness.status(message); status["opens_used"] != 1.0 {
		t.Fatalf("a replayed claim spent an open: %v", status)
	}
	replayed(
		"burn",
		signedRequest(&message.link.R, core.PurposeRead, "POST", path+"/burn", harness.challenge(), nil),
		http.StatusNoContent,
	)

	other := newMessage(t, core.KDFNone, "", 1, 3600, "cancel once")
	harness.mustCreate(other)
	replayed(
		"cancel",
		signedRequest(
			other.manage,
			core.PurposeManage,
			"POST",
			"/api/v1/messages/"+other.id.String()+"/cancel",
			harness.challenge(),
			nil,
		),
		http.StatusNoContent,
	)

	reported := newMessage(t, core.KDFNone, "", 1, 3600, "reported once")
	harness.mustCreate(reported)
	body := []byte(`{"id":"` + reported.id.String() + `","reason":"spam"}`)
	report := signedRequest(&reported.link.R, core.PurposeRead, "POST", "/api/v1/abuse", harness.challenge(), body)
	report.headers["Content-Type"] = "application/json"
	for range 2 {
		if response, _ := harness.do(report); response.StatusCode != http.StatusAccepted {
			t.Fatalf("abuse report: %d", response.StatusCode)
		}
	}
	if n := abuseLines(t, harness, reported.id); n != 1 {
		t.Fatalf("a replayed abuse report was stored %d times", n)
	}
}

func TestSignatureBindsTheRequest(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	message := newMessage(t, core.KDFNone, "", 1, 3600, "bound")
	harness.mustCreate(message)
	path := "/api/v1/messages/" + message.id.String()
	moved := func(method, to string) *testRequest {
		request := signedRequest(&message.link.R, core.PurposeRead, "GET", path, harness.challenge(), nil)
		request.method, request.path = method, to
		return request
	}
	for name, request := range map[string]*testRequest{
		"blob path":  moved("GET", path+"/blob"),
		"claim":      moved("POST", path+"/claim"),
		"burn":       moved("POST", path+"/burn"),
		"status":     moved("GET", path+"/status"),
		"manage key": signedRequest(&message.link.R, core.PurposeManage, "GET", path+"/status", harness.challenge(), nil),
		"read as M":  signedRequest(message.manage, core.PurposeRead, "GET", path, harness.challenge(), nil),
		"bad sig":    rawAuth(&testRequest{method: http.MethodGet, path: path}, badSignature(&message.link.R, core.PurposeRead, "GET", path, harness.challenge())),
	} {
		if response, body := harness.do(request); response.StatusCode != http.StatusNotFound {
			t.Fatalf("%s: %d %s", name, response.StatusCode, body)
		}
	}
	if _, status := harness.status(message); status["state"] != "available" || status["opens_used"] != 0.0 {
		t.Fatalf("a signature moved to another request changed the message: %v", status)
	}
	spent := moved("POST", path+"/burn")
	harness.do(spent)
	spent.method, spent.path = "GET", path
	if response, _ := harness.do(spent); response.StatusCode != http.StatusNotFound {
		t.Fatalf("a failed attempt must spend its challenge: %d", response.StatusCode)
	}
	if response, body := harness.do(
		signedRequest(&message.link.R, core.PurposeRead, "GET", path, harness.challenge(), nil),
	); response.StatusCode != http.StatusOK ||
		!strings.Contains(string(body), `"state":"available"`) {
		t.Fatalf("a fresh signature: %d %s", response.StatusCode, body)
	}

	body := []byte(`{"id":"` + message.id.String() + `","reason":"first"}`)
	tampered := signedRequest(&message.link.R, core.PurposeRead, "POST", "/api/v1/abuse", harness.challenge(), body)
	tampered.body = []byte(`{"id":"` + message.id.String() + `","reason":"other"}`)
	tampered.headers["Content-Type"] = "application/json"
	if response, _ := harness.do(tampered); response.StatusCode != http.StatusAccepted {
		t.Fatalf("tampered abuse report: %d", response.StatusCode)
	}
	if n := abuseLines(t, harness, message.id); n != 0 {
		t.Fatal("an abuse report with a changed body was stored")
	}
}

func TestVerifierSignatureIsBoundToTheClaim(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	const pass = "correct horse battery staple"
	message := newMessage(t, core.KDFScrypt, pass, 1, 3600, "behind a passphrase")
	harness.mustCreate(message)
	path := "/api/v1/messages/" + message.id.String() + "/claim"
	stranger := core.NewSecret()
	pkV := core.PublicKey(&message.content.V)
	cases := []struct {
		name   string
		header func(challenge [core.ChallengeLen]byte) string
	}{
		{"other challenge", func([core.ChallengeLen]byte) string {
			return core.SignRequest(&message.content.V, core.PurposeVerifier, "POST", path, harness.challenge(), nil).
				VerifierHeader()
		}},
		{"read purpose", func(c [core.ChallengeLen]byte) string {
			return core.SignRequest(&message.content.V, core.PurposeRead, "POST", path, c, nil).VerifierHeader()
		}},
		{"other path", func(c [core.ChallengeLen]byte) string {
			return core.SignRequest(&message.content.V, core.PurposeVerifier, "POST", path+"x", c, nil).VerifierHeader()
		}},
		{"right key, foreign signature", func(c [core.ChallengeLen]byte) string {
			signature := core.SignRequest(stranger, core.PurposeVerifier, "POST", path, c, nil)
			signature.PublicKey = pkV
			return signature.VerifierHeader()
		}},
	}
	for i, c := range cases {
		challenge := harness.challenge()
		request := signedRequest(&message.link.R, core.PurposeRead, "POST", path, challenge, nil)
		request.headers["X-Nylzo-Verifier"] = c.header(challenge)
		response, body := harness.do(request)
		var got passphraseResponse
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("%s: %d %s: %v", c.name, response.StatusCode, body, err)
		}
		if response.StatusCode != http.StatusForbidden || got.AttemptsLeft != 4-i {
			t.Fatalf("%s: %d %+v", c.name, response.StatusCode, got)
		}
	}
	challenge := harness.challenge()
	request := signedRequest(&message.link.R, core.PurposeRead, "POST", path, challenge, nil)
	request.headers["X-Nylzo-Verifier"] = core.SignRequest(&message.content.V, core.PurposeVerifier, "POST", path, challenge, nil).
		VerifierHeader()
	if response, _ := harness.do(request); response.StatusCode != http.StatusOK {
		t.Fatalf("correct verifier: %d", response.StatusCode)
	}
}

type passphraseResponse struct {
	Error        string `json:"error"`
	RetryAfter   int    `json:"retry_after"`
	AttemptsLeft int    `json:"attempts_left"`
}

func TestChallengeWindowAndRotation(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	message := newMessage(t, core.KDFNone, "", 1, 3600, "challenges")
	harness.mustCreate(message)
	path := "/api/v1/messages/" + message.id.String()

	response, body := harness.do(&testRequest{method: http.MethodGet, path: "/api/v1/challenge"})
	var issued struct {
		Challenge string `json:"challenge"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal(body, &issued); err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("challenge: %d %s", response.StatusCode, body)
	}
	raw, err := core.DecodeExact(issued.Challenge, core.ChallengeLen)
	if err != nil || len(issued.Challenge) != 48 ||
		issued.ExpiresAt != harness.clock.Now().Add(time.Minute).UTC().Format(time.RFC3339) {
		t.Fatalf("challenge shape: %+v", issued)
	}
	if got, _ := harness.do(
		signedRequest(&message.link.R, core.PurposeRead, "GET", path, [core.ChallengeLen]byte(raw), nil),
	); got.StatusCode != http.StatusOK {
		t.Fatalf("issued challenge: %d", got.StatusCode)
	}

	now := harness.clock.Now()
	forged := harness.challenge()
	forged[core.ChallengeLen-1] ^= 1
	for name, c := range map[string]struct {
		challenge [core.ChallengeLen]byte
		status    int
	}{
		"issued 60 s ago":    {harness.challengeAt(now.Add(-60 * time.Second)), http.StatusOK},
		"issued 61 s ago":    {harness.challengeAt(now.Add(-61 * time.Second)), http.StatusNotFound},
		"issued 5 s ahead":   {harness.challengeAt(now.Add(5 * time.Second)), http.StatusOK},
		"issued 6 s ahead":   {harness.challengeAt(now.Add(6 * time.Second)), http.StatusNotFound},
		"forged tag":         {forged, http.StatusNotFound},
		"all zero challenge": {[core.ChallengeLen]byte{}, http.StatusNotFound},
	} {
		if got, refused := harness.do(
			signedRequest(&message.link.R, core.PurposeRead, http.MethodGet, path, c.challenge, nil),
		); got.StatusCode != c.status {
			t.Fatalf("%s: %d %s", name, got.StatusCode, refused)
		}
	}
	for name, auth := range map[string]string{
		"bearer R":        "Bearer " + message.readToken,
		"lowercase":       strings.ToLower(core.AuthScheme) + " x",
		"two parts":       core.AuthScheme + " " + publicKey(&message.link.R) + "." + core.Encode(raw),
		"padded":          core.AuthScheme + " " + publicKey(&message.link.R) + "=.x.y",
		"missing":         "",
		"double space":    strings.Replace(signedRequest(&message.link.R, core.PurposeRead, "GET", path, harness.challenge(), nil).headers["Authorization"], " ", "  ", 1),
		"trailing period": signedRequest(&message.link.R, core.PurposeRead, "GET", path, harness.challenge(), nil).headers["Authorization"] + ".",
	} {
		if got, _ := harness.do(
			rawAuth(&testRequest{method: http.MethodGet, path: path}, auth),
		); got.StatusCode != http.StatusNotFound {
			t.Fatalf("%s: %d", name, got.StatusCode)
		}
	}

	before := harness.challenge()
	harness.reopen(baseSettings(pepperB, pepperA))
	if got, _ := harness.do(
		signedRequest(&message.link.R, core.PurposeRead, "GET", path, before, nil),
	); got.StatusCode != http.StatusNotFound {
		t.Fatalf("a challenge from before the rotation: %d", got.StatusCode)
	}
	if got, _ := harness.do(
		signedRequest(&message.link.R, core.PurposeRead, "GET", path, harness.challenge(), nil),
	); got.StatusCode != http.StatusOK {
		t.Fatalf("a fresh challenge after the rotation: %d", got.StatusCode)
	}
}

func TestSignedEndpointsRequireAnEmptyBody(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	message := newMessage(t, core.KDFNone, "", 1, 3600, "no bodies")
	harness.mustCreate(message)
	path := "/api/v1/messages/" + message.id.String()
	for _, e := range []struct {
		method, suffix, purpose string
		seed                    *[core.TokenLen]byte
	}{
		{"GET", "", core.PurposeRead, &message.link.R},
		{"POST", "/claim", core.PurposeRead, &message.link.R},
		{"GET", "/blob", core.PurposeRead, &message.link.R},
		{"POST", "/ack", core.PurposeRead, &message.link.R},
		{"POST", "/burn", core.PurposeRead, &message.link.R},
		{"GET", "/status", core.PurposeManage, message.manage},
		{"POST", "/cancel", core.PurposeManage, message.manage},
	} {
		request := signedRequest(e.seed, e.purpose, e.method, path+e.suffix, harness.challenge(), []byte("x"))
		if response, _ := harness.do(request); response.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s %s with a body: %d", e.method, e.suffix, response.StatusCode)
		}
	}
	if _, status := harness.status(message); status["state"] != "available" || status["opens_used"] != 0.0 {
		t.Fatalf("requests with a body changed the message: %v", status)
	}
}

func TestSpentChallengesExpire(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	message := newMessage(t, core.KDFNone, "", 1, 3600, "sweep")
	harness.mustCreate(message)
	path := "/api/v1/messages/" + message.id.String()
	challenge := harness.challenge()
	if response, _ := harness.do(
		signedRequest(&message.link.R, core.PurposeRead, "GET", path, challenge, nil),
	); response.StatusCode != http.StatusOK {
		t.Fatalf("metadata: %d", response.StatusCode)
	}
	spentRows := func() int {
		t.Helper()
		db := openDB(t, harness)
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM rate_counters WHERE key LIKE 'challenge:%'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if spentRows() != 1 {
		t.Fatal("a passed request must record its challenge")
	}
	harness.clock.Advance(time.Minute)
	if _, err := harness.server.SweepOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if spentRows() != 1 {
		t.Fatal("a sweep dropped a challenge that could still be replayed by a slower instance")
	}
	harness.clock.Advance(2 * time.Minute)
	if _, err := harness.server.SweepOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if spentRows() != 0 {
		t.Fatal("the sweep kept a spent challenge after it expired")
	}
}

func TestRejectedRequestsCannotBeReplayed(t *testing.T) {
	t.Parallel()
	const victim, observer = "203.0.113.10", "198.51.100.20"
	cases := []struct {
		name   string
		config func(settings *config.Server)
		before func(harness *testHarness, message *testMessage)
		header map[string]string
		status int
	}{
		{
			"token bucket 429",
			func(settings *config.Server) { settings.Rates.SourcePerSecond, settings.Rates.SourceBurst = 1e-9, 1 },
			func(harness *testHarness, message *testMessage) {
				harness.do(&testRequest{method: http.MethodGet, path: "/api/v1/instance", address: victim})
			},
			nil,
			http.StatusTooManyRequests,
		},
		{
			"read counter 429",
			func(settings *config.Server) { settings.Rates.ReadPerHour = 1 },
			func(harness *testHarness, message *testMessage) {
				harness.do(
					signedRequestFrom(
						victim,
						&message.link.R,
						core.PurposeRead,
						"GET",
						"/api/v1/messages/"+message.id.String(),
						harness.challenge(),
					),
				)
			},
			nil,
			http.StatusTooManyRequests,
		},
		{"prefetch 503", nil, nil, map[string]string{"Sec-Purpose": "prefetch"}, http.StatusServiceUnavailable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			settings := baseSettings(pepperA, nil)
			if c.config != nil {
				c.config(settings)
			}
			harness := newHarness(t, settings)
			message := newMessage(t, core.KDFNone, "", 1, 3600, "recorded")
			message.address = "192.0.2.30"
			harness.mustCreate(message)
			if c.before != nil {
				c.before(harness, message)
			}
			claim := signedRequestFrom(
				victim,
				&message.link.R,
				core.PurposeRead,
				"POST",
				"/api/v1/messages/"+message.id.String()+"/claim",
				harness.challenge(),
			)
			maps.Copy(claim.headers, c.header)
			if response, _ := harness.do(claim); response.StatusCode != c.status {
				t.Fatalf("the victim's claim: %d, want %d", response.StatusCode, c.status)
			}
			for name := range c.header {
				delete(claim.headers, name)
			}
			claim.address = observer
			if response, _ := harness.do(claim); response.StatusCode != http.StatusNotFound {
				t.Fatalf("a recorded claim that the server turned away was accepted later: %d", response.StatusCode)
			}
			if record, err := harness.store.Get(
				t.Context(),
				message.id.String(),
			); err != nil || record.Message == nil ||
				record.Message.OpensRemaining != 1 {
				t.Fatalf("the replay spent an open: %+v %v", record.Message, err)
			}
		})
	}
}

func signedRequestFrom(
	address string,
	seed *[core.TokenLen]byte,
	purpose, method, path string,
	challenge [core.ChallengeLen]byte,
) *testRequest {
	request := signedRequest(seed, purpose, method, path, challenge, nil)
	request.address = address
	return request
}

func TestStoredSmallOrderKeysCannotSign(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	ring, err := pepper.NewRing(pepperA, nil)
	if err != nil {
		t.Fatal(err)
	}
	identity := [core.PublicKeyLen]byte{1}
	forge := func(purpose, method, path string, c [core.ChallengeLen]byte) core.Signature {
		signature := core.Signature{PublicKey: identity, Challenge: c, Sig: [core.SignatureLen]byte{1}}
		if !signature.Verify(purpose, method, path, nil) {
			t.Fatal("the forged signature does not verify, so the test proves nothing")
		}
		return signature
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(harness.dir, "nylzo.db")+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	setMAC := func(message *testMessage, column string, p pepper.Purpose) {
		mac, _ := ring.MAC(p, identity[:])
		if _, execErr := db.Exec(
			`UPDATE messages SET `+column+` = ? WHERE id = ?`,
			mac[:],
			message.id.String(),
		); execErr != nil {
			t.Fatal(execErr)
		}
	}

	message := newMessage(t, core.KDFNone, "", 1, 3600, "stored before keys were checked")
	harness.mustCreate(message)
	setMAC(message, "read_mac", pepper.Read)
	setMAC(message, "manage_mac", pepper.Manage)
	path := "/api/v1/messages/" + message.id.String()
	for name, request := range map[string]*testRequest{
		"metadata": rawAuth(&testRequest{method: http.MethodGet, path: path}, forge(core.PurposeRead, "GET", path, harness.challenge()).Authorization()),
		"status":   rawAuth(&testRequest{method: http.MethodGet, path: path + "/status"}, forge(core.PurposeManage, "GET", path+"/status", harness.challenge()).Authorization()),
		"burn":     rawAuth(&testRequest{method: http.MethodPost, path: path + "/burn"}, forge(core.PurposeRead, "POST", path+"/burn", harness.challenge()).Authorization()),
	} {
		if response, body := harness.do(request); response.StatusCode != http.StatusNotFound {
			t.Fatalf("%s signed with the identity key: %d %s", name, response.StatusCode, body)
		}
	}

	const pass = "correct horse battery staple"
	pw := newMessage(t, core.KDFScrypt, pass, 1, 3600, "verifier stored before keys were checked")
	harness.mustCreate(pw)
	setMAC(pw, "verifier_mac", pepper.Verifier)
	claimPath := "/api/v1/messages/" + pw.id.String() + "/claim"
	challenge := harness.challenge()
	request := signedRequest(&pw.link.R, core.PurposeRead, "POST", claimPath, challenge, nil)
	request.headers["X-Nylzo-Verifier"] = forge(core.PurposeVerifier, "POST", claimPath, challenge).VerifierHeader()
	response, body := harness.do(request)
	if response.StatusCode != http.StatusForbidden || !strings.Contains(string(body), `"attempts_left":4`) {
		t.Fatalf("claim with the identity verifier key: %d %s", response.StatusCode, body)
	}
	if _, metadata := harness.metadata(pw); metadata["opens_remaining"] != float64(1) {
		t.Fatalf("the refused verifier consumed an open: %v", metadata)
	}
}

func TestHeadOnSignedRoutesIsNotFound(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	message := newMessage(t, core.KDFNone, "", 1, 3600, "head")
	harness.mustCreate(message)
	path := "/api/v1/messages/" + message.id.String()
	for _, p := range []string{path, path + "/status"} {
		purpose, seed := core.PurposeRead, &message.link.R
		if strings.HasSuffix(p, "/status") {
			purpose, seed = core.PurposeManage, message.manage
		}
		request := signedRequest(seed, purpose, "HEAD", p, harness.challenge(), nil)
		if response, body := harness.do(request); response.StatusCode != http.StatusNotFound || len(body) != 0 {
			t.Fatalf("HEAD %s: %d %q", p, response.StatusCode, body)
		}
	}
	for _, p := range []string{"/api/v1/instance", "/api/v1/challenge", "/api/v1/messages", "/api/v1/abuse", path + "/claim", path + "/ack"} {
		if response, body := harness.do(
			&testRequest{method: http.MethodHead, path: p, address: message.address},
		); response.StatusCode != http.StatusNotFound ||
			len(body) != 0 {
			t.Fatalf("HEAD %s: %d %q", p, response.StatusCode, body)
		}
	}
}

func TestOnlyAnAuthorizedRequestSettlesAMessage(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	message := newMessage(t, core.KDFNone, "", 1, 60, "expires")
	harness.mustCreate(message)
	harness.clock.Advance(3 * time.Minute)
	path := "/api/v1/messages/" + message.id.String()
	if response, _ := harness.do(
		signedRequest(core.NewSecret(), core.PurposeRead, "GET", path, harness.challenge(), nil),
	); response.StatusCode != http.StatusNotFound {
		t.Fatalf("wrong key: %d", response.StatusCode)
	}
	if record, err := harness.store.Get(
		t.Context(),
		message.id.String(),
	); err != nil || record.Message == nil ||
		!harness.blobExists(message.id) {
		t.Fatal("a request with a wrong key ended the expired message, so it did more work than an unknown id")
	}
	if _, metadata := harness.metadata(message); metadata["state"] != "expired" {
		t.Fatalf("authorized metadata: %v", metadata)
	}
	if record, err := harness.store.Get(
		t.Context(),
		message.id.String(),
	); err != nil || record.Tombstone == nil ||
		harness.blobExists(message.id) {
		t.Fatal("the authorized request did not finish the expiry")
	}
}

func TestAK27PepperRotation(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	old := newMessage(t, core.KDFScrypt, "pw", 2, 7*24*3600, "before rotation")
	harness.mustCreate(old)
	harness.reopen(baseSettings(pepperB, pepperA))
	if response, _ := harness.metadata(old); response.StatusCode != http.StatusOK {
		t.Fatalf("old message after rotation: %d", response.StatusCode)
	}
	response, _ := harness.claim(old, old.verifier, "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("claim old message after rotation: %d", response.StatusCode)
	}
	if got, _ := harness.status(old); got.StatusCode != http.StatusOK {
		t.Fatal("status with old manage mac")
	}
	fresh := newMessage(t, core.KDFNone, "", 1, 3600, "after rotation")
	harness.mustCreate(fresh)
	harness.reopen(baseSettings(pepperB, nil))
	if got, _ := harness.metadata(fresh); got.StatusCode != http.StatusOK {
		t.Fatal("new message must verify with the new pepper alone")
	}
	if got, _ := harness.metadata(old); got.StatusCode != http.StatusNotFound {
		t.Fatalf("message under a dropped pepper must look unknown: %d", got.StatusCode)
	}
	if _, err := New(baseSettings(pepperA, pepperA), harness.store, harness.blobs); err == nil {
		t.Fatal("identical peppers accepted")
	}
}

func TestAK27PreviousPepperHorizon(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	old := newMessage(t, core.KDFNone, "", 1, 7*24*3600, "max ttl")
	harness.mustCreate(old)
	harness.reopen(baseSettings(pepperB, pepperA))
	harness.clock.Advance(7*24*time.Hour + time.Minute)
	if _, err := harness.server.SweepOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if response, metadata := harness.metadata(
		old,
	); response.StatusCode != http.StatusOK ||
		metadata["state"] != "expired" {
		t.Fatalf("tombstone with the previous pepper still configured: %d %v", response.StatusCode, metadata)
	}
	if record, err := harness.store.Get(t.Context(), old.id.String()); err != nil || record.Message != nil {
		t.Fatal("no live message may remain after the maximum TTL")
	}
	harness.clock.Advance(store.TombstoneTTL)
	if _, err := harness.server.SweepOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	harness.reopen(baseSettings(pepperB, nil))
	if _, err := harness.store.Get(t.Context(), old.id.String()); err == nil {
		t.Fatal("tombstone must be purged before the previous pepper is dropped")
	}
}

func TestReplayAfterRestartIsRefused(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	message := newMessage(t, core.KDFNone, "", 2, 3600, "replay after restart")
	harness.mustCreate(message)
	path := "/api/v1/messages/" + message.id.String()
	claim := signedRequest(&message.link.R, core.PurposeRead, "POST", path+"/claim", harness.challenge(), nil)
	status := signedRequest(message.manage, core.PurposeManage, "GET", path+"/status", harness.challenge(), nil)
	for _, request := range []*testRequest{claim, status} {
		if response, _ := harness.do(request); response.StatusCode != http.StatusOK {
			t.Fatalf("%s: %d", request.path, response.StatusCode)
		}
	}
	harness.reopen(&harness.settings)
	for _, request := range []*testRequest{claim, status} {
		if response, _ := harness.do(request); response.StatusCode != http.StatusNotFound {
			t.Fatalf("%s replayed against a fresh process: %d", request.path, response.StatusCode)
		}
	}
	if _, status := harness.status(message); status["opens_used"] != 1.0 {
		t.Fatalf("the replay spent an open: %v", status)
	}
}

func TestAuthorizeDoesTheSameWorkForKeysThatFailTheKeyCheck(t *testing.T) {
	s, _ := testServer(t, openStore(t), nil)
	path := "/api/v1/messages/" + core.NewID().String()
	var offCurve [core.PublicKeyLen]byte
	for i := byte(2); ; i++ {
		offCurve[0] = i
		if _, err := new(edwards25519.Point).SetBytes(offCurve[:]); err != nil {
			break
		}
	}
	testRequest := func(pk *[core.PublicKeyLen]byte) *http.Request {
		c, _ := s.challenges.Issue(s.clock())
		signature := core.SignRequest(core.NewSecret(), core.PurposeRead, "GET", path, c, nil)
		if pk != nil {
			signature.PublicKey = *pk
		}
		r := httptest.NewRequest(http.MethodGet, path, http.NoBody)
		r.Header.Set("Authorization", signature.Authorization())
		return r.WithContext(context.WithValue(r.Context(), spentKey{}, &signature.Challenge))
	}
	const samples = 300
	var wrong, off []time.Duration
	for range samples {
		for _, pk := range []*[core.PublicKeyLen]byte{nil, &offCurve} {
			r := testRequest(pk)
			start := time.Now()
			if _, ok, err := s.authorize(
				r.Context(),
				r,
				path[len("/api/v1/messages/"):],
				pepper.Read,
				nil,
			); ok ||
				err != nil {
				t.Fatalf("authorize: %v %v", ok, err)
			}
			if pk == nil {
				wrong = append(wrong, time.Since(start))
			} else {
				off = append(off, time.Since(start))
			}
		}
	}
	median := func(d []time.Duration) time.Duration {
		slices.Sort(d)
		return d[len(d)/2]
	}
	// gstack-timing-ok: uniform work for every failed key is the contract under test (spec §5.5)
	if mw, mo := median(wrong), median(off); mo*130 < mw*100 {
		t.Fatalf("a key that fails the key check is answered in %s, a wrong key in %s", mo, mw)
	}
}

type failingSpends struct {
	store.Store

	fail bool
}

func (s *failingSpends) RateReserveBatch(ctx context.Context, hits []store.RateHit) ([]bool, error) {
	if s.fail {
		return nil, errors.New("test: the store is down")
	}
	return s.Store.RateReserveBatch(ctx, hits)
}

func TestAFailedSpendRefusesTheChallenge(t *testing.T) {
	t.Parallel()
	settings := config.Defaults()
	settings.DataDir = t.TempDir()
	settings.Pepper = bytes.Repeat([]byte{1}, 32)
	database, err := sqlite.Open(t.Context(), filepath.Join(settings.DataDir, "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	failing := &failingSpends{Store: database, fail: true}
	s, err := New(&settings, failing, nil, WithLogger(slog.New(slog.DiscardHandler)))
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/messages/" + core.NewID().String()
	c, _ := s.challenges.Issue(s.clock())
	auth := core.SignRequest(core.NewSecret(), core.PurposeRead, "GET", path, c, nil).Authorization()
	send := func() int {
		r := httptest.NewRequest(http.MethodGet, path, http.NoBody)
		r.Header.Set("Authorization", auth)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w.Code
	}
	if code := send(); code != http.StatusInternalServerError {
		t.Fatalf("a spend the store could not record: %d", code)
	}
	failing.fail = false
	if code := send(); code != http.StatusNotFound {
		t.Fatalf("the same process accepted the challenge after a failed spend: %d", code)
	}
}

func TestStorageFaultKeepsUniformNotFound(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	faulty := harness.withFaultyBlobs()
	message := newMessage(t, core.KDFNone, "", 2, 3600, "x")
	harness.mustCreate(message)
	if _, _, _, err := harness.store.MarkEnding(
		t.Context(),
		message.id.String(),
		store.StateCancelled,
		harness.clock.Now(),
	); err != nil {
		t.Fatal(err)
	}
	faulty.set(func(b *faultyBlobs) { b.failDelete[message.id.String()] = true })
	unknown, unknownBody := harness.do(
		&testRequest{
			method: http.MethodGet,
			path:   "/api/v1/messages/" + core.NewID().String(),
			token:  message.readToken,
		},
	)
	wrong, wrongBody := harness.do(
		&testRequest{
			method: http.MethodGet,
			path:   "/api/v1/messages/" + message.id.String(),
			token:  core.Encode(make([]byte, 32)),
		},
	)
	if unknown.StatusCode != http.StatusNotFound || wrong.StatusCode != http.StatusNotFound ||
		!bytes.Equal(unknownBody, wrongBody) {
		t.Fatalf(
			"a storage fault must not change the 404 for a wrong token: %d %d",
			unknown.StatusCode,
			wrong.StatusCode,
		)
	}
	response, body := harness.claim(message, "", "")
	if response.StatusCode != http.StatusGone || decodeJSON(t, body)["state"] != "cancelled" {
		t.Fatalf("claim on a recorded cancel whose delete keeps failing: %d %s", response.StatusCode, body)
	}
	if _, metadata := harness.metadata(message); metadata["state"] != "cancelled" {
		t.Fatalf("metadata: %v", metadata)
	}
}

func BenchmarkSignedMetadataWithASpentChallenge(b *testing.B) {
	s, _ := testServer(b, openStore(b), func(c *config.Server) {
		c.Rates.SourcePerSecond, c.Rates.SourceBurst = 1e9, 1e9
		c.Rates.BucketPerSecond, c.Rates.BucketBurst = 1e9, 1e9
		c.Rates.ReadPerHour = 0
	})
	path := "/api/v1/messages/" + core.NewID().String()
	c, _ := s.challenges.Issue(s.clock().Add(-2 * time.Minute))
	auth := core.SignRequest(core.NewSecret(), core.PurposeRead, "GET", path, c, nil).Authorization()
	for b.Loop() {
		r := httptest.NewRequest(http.MethodGet, path, http.NoBody)
		r.Header.Set("Authorization", auth)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != http.StatusNotFound {
			b.Fatalf("status %d", w.Code)
		}
	}
}

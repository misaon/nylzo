package server

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/core/signing"
	"github.com/misaon/nylzo/internal/server/adminstore"
	"github.com/misaon/nylzo/internal/server/config"
	"github.com/misaon/nylzo/internal/server/ratelimit"
	"github.com/misaon/nylzo/internal/server/visits"
)

const (
	adminOrigin       = "https://nylzo.test"
	adminRelyingParty = "nylzo.test"
	browserAgent      = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 Version/26.0 Safari/605.1.15"
)

type adminHarness struct {
	*testHarness

	store *adminstore.Store
	path  string
}

func newAdminHarness(t *testing.T, changes ...func(*config.Server)) *adminHarness {
	t.Helper()
	path := filepath.Join(t.TempDir(), adminstore.FileName)
	store, err := adminstore.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	settings := baseSettings(pepperA, nil)
	settings.PublicURL = adminOrigin
	settings.EdgeReports = true
	for _, change := range changes {
		change(settings)
	}
	return &adminHarness{testHarness: newHarness(t, settings, WithAdminStore(store)), store: store, path: path}
}

// softwarePasskey stands in for an authenticator that verifies its user.
type softwarePasskey struct {
	key   *ecdsa.PrivateKey
	id    []byte
	count uint32
}

func newSoftwarePasskey(t *testing.T) *softwarePasskey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 32)
	rand.Read(id)
	return &softwarePasskey{key: key, id: id}
}

func (p *softwarePasskey) authenticatorData(attested bool) []byte {
	hash := sha256.Sum256([]byte(adminRelyingParty))
	flags := byte(0x05)
	if attested {
		flags |= 0x40
	}
	p.count++
	data := append(hash[:], flags)
	data = binary.BigEndian.AppendUint32(data, p.count)
	if attested {
		data = append(data, make([]byte, 16)...)
		data = binary.BigEndian.AppendUint16(data, uint16(len(p.id)))
		data = append(data, p.id...)
		data = append(data, 0xa0)
	}
	return data
}

func clientData(t *testing.T, kind string, challenge []byte) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{"type": kind, "challenge": core.Encode(challenge), "origin": adminOrigin})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func (h *adminHarness) post(path string, body any, address string) (testResponse, jsonFields) {
	h.t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		h.t.Fatal(err)
	}
	response, answer := h.do(&testRequest{method: http.MethodPost, path: path, body: encoded, address: address})
	return response, decodeJSON(h.t, answer)
}

func ceremonyChallenge(t *testing.T, answer jsonFields) []byte {
	t.Helper()
	options, _ := answer["public_key"].(map[string]any)
	encoded, _ := options["challenge"].(string)
	challenge, err := core.DecodeExact(encoded, 32)
	if err != nil {
		t.Fatalf("no ceremony challenge in %v", answer)
	}
	return challenge
}

func (h *adminHarness) openEnrollment() string {
	h.t.Helper()
	token := make([]byte, 24)
	rand.Read(token)
	if err := h.store.OpenEnrollment(
		h.t.Context(),
		adminstore.HashToken(token),
		time.Now().Add(time.Minute),
	); err != nil {
		h.t.Fatal(err)
	}
	return core.Encode(token)
}

func (h *adminHarness) enroll(token string, passkey *softwarePasskey) int {
	h.t.Helper()
	response, answer := h.post(api.AdminPath+"/enrollment/options", map[string]string{"token": token}, "")
	if response.StatusCode != http.StatusOK {
		return response.StatusCode
	}
	publicKey, err := x509.MarshalPKIXPublicKey(&passkey.key.PublicKey)
	if err != nil {
		h.t.Fatal(err)
	}
	response, _ = h.post(api.AdminPath+"/enrollment", map[string]any{
		"token":              token,
		"credential_id":      core.Encode(passkey.id),
		"client_data":        core.Encode(clientData(h.t, "webauthn.create", ceremonyChallenge(h.t, answer))),
		"authenticator_data": core.Encode(passkey.authenticatorData(true)),
		"public_key":         core.Encode(publicKey),
		"algorithm":          -7,
	}, "")
	return response.StatusCode
}

type signedIn struct {
	seed      *[core.SeedLen]byte
	challenge string
	status    int
	answer    map[string]string
}

// signIn returns the seed of the session key and the challenge for its first request.
func (h *adminHarness) signIn(passkey *softwarePasskey) signedIn {
	h.t.Helper()
	seed := core.NewSecret()
	key := signing.PublicKey(seed)
	_, answer := h.post(api.AdminPath+"/session/options", map[string]string{"key": core.Encode(key[:])}, "")
	data := passkey.authenticatorData(false)
	client := clientData(h.t, "webauthn.get", ceremonyChallenge(h.t, answer))
	clientHash := sha256.Sum256(client)
	digest := sha256.Sum256(append(bytes.Clone(data), clientHash[:]...))
	signature, err := ecdsa.SignASN1(rand.Reader, passkey.key, digest[:])
	if err != nil {
		h.t.Fatal(err)
	}
	assertion := map[string]string{
		"key":                core.Encode(key[:]),
		"credential_id":      core.Encode(passkey.id),
		"client_data":        core.Encode(client),
		"authenticator_data": core.Encode(data),
		"signature":          core.Encode(signature),
	}
	response, session := h.post(api.AdminPath+"/session", assertion, "")
	challenge, _ := session["challenge"].(string)
	return signedIn{seed: seed, challenge: challenge, status: response.StatusCode, answer: assertion}
}

func (h *adminHarness) signed(seed *[core.SeedLen]byte, method, path, challenge string) answered {
	h.t.Helper()
	var raw [signing.ChallengeLen]byte
	if !core.DecodeInto(raw[:], challenge) {
		h.t.Fatalf("no challenge to sign %s with", path)
	}
	authorization := signing.SignRequest(seed, signing.PurposeAdmin, method, path, raw, nil, signing.Components{}).
		Authorization()
	return h.authorized(method, path, authorization)
}

type answered struct {
	status int
	fields jsonFields
	next   string
}

func (h *adminHarness) authorized(method, path, authorization string) answered {
	h.t.Helper()
	headers := map[string]string{}
	if authorization != "" {
		headers["Authorization"] = authorization
	}
	response, answer := h.do(&testRequest{method: method, path: path, headers: headers})
	fields := decodeJSON(h.t, answer)
	next, _ := fields["challenge"].(string)
	return answered{status: response.StatusCode, fields: fields, next: next}
}

// TestAK28AdminAnswersOnlyToASessionAPasskeyOpened covers the admin area's one gate: a figure leaves the server
// only for a request that a live session signed over a challenge of its own, and the session exists only after a
// passkey of the instance signed it in.
func TestAK28AdminAnswersOnlyToASessionAPasskeyOpened(t *testing.T) {
	harness := newAdminHarness(t)
	overview, live := api.AdminPath+"/overview?range=24h", api.AdminPath+"/live"
	fresh := func() string { return core.Encode(new(harness.challenge())[:]) }
	refused := func(what string, answer answered) {
		t.Helper()
		if answer.status != http.StatusUnauthorized {
			t.Fatalf("%s got %d", what, answer.status)
		}
	}
	refused("an unsigned request", harness.authorized(http.MethodGet, overview, ""))
	for _, path := range []string{api.AdminPath, overview, live} {
		if head := harness.authorized(http.MethodHead, path, ""); head.status != http.StatusNotFound {
			t.Fatalf("HEAD %s reached a handler: %d", path, head.status)
		}
	}
	passkey := newSoftwarePasskey(t)
	if status := harness.enroll(harness.openEnrollment(), passkey); status != http.StatusCreated {
		t.Fatalf("enrollment: %d", status)
	}
	refused("a key that no passkey signed in", harness.signed(core.NewSecret(), http.MethodGet, overview, fresh()))
	if stranger := harness.signIn(newSoftwarePasskey(t)); stranger.status != http.StatusUnauthorized {
		t.Fatalf("a passkey the instance never enrolled signed in: %d", stranger.status)
	}
	session := harness.signIn(passkey)
	if session.status != http.StatusOK || session.challenge == "" {
		t.Fatalf("sign-in: %d, challenge %q", session.status, session.challenge)
	}
	sign := func(path, challenge string) string {
		var raw [signing.ChallengeLen]byte
		core.DecodeInto(raw[:], challenge)
		return signing.SignRequest(session.seed, signing.PurposeAdmin, http.MethodGet, path, raw, nil, signing.Components{}).
			Authorization()
	}
	authorization := sign(overview, session.challenge)
	first := harness.authorized(http.MethodGet, overview, authorization)
	if first.status != http.StatusOK || first.fields["range"] != "24h" || first.fields["stored"] != float64(0) ||
		first.next == "" {
		t.Fatalf("a signed request got %d %v", first.status, first.fields)
	}
	refused("a replayed request", harness.authorized(http.MethodGet, overview, authorization))
	refused("a signature for another address", harness.authorized(http.MethodGet, live, sign(overview, fresh())))
	second := harness.signed(session.seed, http.MethodGet, live, first.next)
	if second.status != http.StatusOK {
		t.Fatalf("live: %d", second.status)
	}
	signedOut := harness.signed(session.seed, http.MethodDelete, api.AdminPath+"/session", second.next)
	if signedOut.status != http.StatusOK {
		t.Fatalf("sign-out: %d", signedOut.status)
	}
	refused("a signed-out session", harness.signed(session.seed, http.MethodGet, overview, fresh()))
}

// The page asks for the live figures every five seconds, so the list of refused clients travels only when it changed
// since the version the page holds.
func TestLiveSendsTheRefusedClientsOnlyWhenTheyChanged(t *testing.T) {
	harness := newAdminHarness(t)
	passkey := newSoftwarePasskey(t)
	if status := harness.enroll(harness.openEnrollment(), passkey); status != http.StatusCreated {
		t.Fatalf("enrollment: %d", status)
	}
	session := harness.signIn(passkey)
	next := session.challenge
	live := func(query string) answered {
		t.Helper()
		answer := harness.signed(session.seed, http.MethodGet, api.AdminPath+"/live"+query, next)
		next = answer.next
		return answer
	}
	harness.server.refusals.Note(netip.MustParseAddr("192.0.2.1"), "requests", time.Now())
	first := live("")
	version := fmt.Sprint(first.fields["refused_version"])
	if listed, ok := first.fields["refused"].([]any); first.status != http.StatusOK || !ok || len(listed) != 1 {
		t.Fatalf("the first live answer: %d %v", first.status, first.fields)
	}
	if same := live("?since=0&refused=" + version); same.status != http.StatusOK || same.fields["refused"] != nil {
		t.Fatalf("an unchanged list was sent again: %d %v", same.status, same.fields["refused"])
	}
	harness.server.refusals.Note(netip.MustParseAddr("192.0.2.2"), "requests", time.Now())
	if changed, ok := live("?refused=" + version).fields["refused"].([]any); !ok || len(changed) != 2 {
		t.Fatalf("a changed list was not sent: %v", changed)
	}
	if malformed := live("?since=soon"); malformed.status != http.StatusBadRequest {
		t.Fatalf("a malformed since: %d", malformed.status)
	}
}

// A reset on the server host is the operator's way to shut a session they do not trust: it ends every session at
// the next minute's work, and a sign-in whose answer is sent twice opens one session.
func TestAdminResetEndsOpenSessions(t *testing.T) {
	harness := newAdminHarness(t)
	passkey := newSoftwarePasskey(t)
	if status := harness.enroll(harness.openEnrollment(), passkey); status != http.StatusCreated {
		t.Fatalf("enrollment: %d", status)
	}
	session := harness.signIn(passkey)
	if replayed, _ := harness.post(
		api.AdminPath+"/session",
		session.answer,
		"",
	); replayed.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a sign-in sent twice got %d", replayed.StatusCode)
	}
	live := api.AdminPath + "/live"
	first := harness.signed(session.seed, http.MethodGet, live, session.challenge)
	if first.status != http.StatusOK {
		t.Fatalf("live: %d", first.status)
	}
	now := time.Now()
	harness.visit(api.VisitPerson, api.PageHome, "203.0.113.9", browserAgent, false)
	harness.server.refusals.Note(netip.MustParseAddr("203.0.113.9"), reasonCreate, now)
	if _, err := harness.store.RemovePasskeys(t.Context()); err != nil {
		t.Fatal(err)
	}
	harness.server.keepAdmin(t.Context(), now, false)
	if refused, online := harness.server.refusals.List(), harness.server.visits.Live(now).Online; len(refused) != 0 ||
		online != 0 {
		t.Fatalf("a server without a passkey still knows %d refused clients and %d visitors", len(refused), online)
	}
	if answer := harness.signed(
		session.seed,
		http.MethodGet,
		live,
		first.next,
	); answer.status != http.StatusUnauthorized {
		t.Fatalf("a session of a removed passkey got %d", answer.status)
	}
	if harness.server.counts.Enabled() {
		t.Fatal("the server counts although no passkey is left")
	}
}

// TestAK29EnrollmentLinkAddsOnePasskeyOnce covers the only way into the admin area: the token that the operator
// drew on the server host enrolls one passkey, and a wrong or used token puts its sender among the refused clients.
func TestAK29EnrollmentLinkAddsOnePasskeyOnce(t *testing.T) {
	harness := newAdminHarness(t)
	if harness.server.counts.Enabled() {
		t.Fatal("a server without a passkey counts")
	}
	token := harness.openEnrollment()
	guess := core.Encode(make([]byte, 24))
	for _, wrong := range []string{guess, "not base64url!"} {
		response, _ := harness.post(
			api.AdminPath+"/enrollment/options",
			map[string]string{"token": wrong},
			"203.0.113.9",
		)
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("the token %q got %d", wrong, response.StatusCode)
		}
	}
	if kept := harness.server.refusals.List(); len(kept) != 0 {
		t.Fatalf("a server without a passkey keeps the addresses of %v", kept)
	}
	enrolled := newSoftwarePasskey(t)
	if status := harness.enroll(token, enrolled); status != http.StatusCreated {
		t.Fatalf("enrollment: %d", status)
	}
	if status := harness.enroll(harness.openEnrollment(), enrolled); status != http.StatusUnauthorized {
		t.Fatalf("a passkey the instance already holds got %d", status)
	}
	if !harness.server.counts.Enabled() {
		t.Fatal("the server does not count although a passkey can sign in")
	}
	if status := harness.enroll(token, newSoftwarePasskey(t)); status != http.StatusUnauthorized {
		t.Fatalf("the used token got %d", status)
	}
	held, err := harness.store.CountPasskeys(t.Context())
	if err != nil || held != 1 {
		t.Fatalf("%d passkeys, %v", held, err)
	}
	response, _ := harness.post(api.AdminPath+"/enrollment/options", map[string]string{"token": guess}, "203.0.113.9")
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a guessed token got %d", response.StatusCode)
	}
	guesser := netip.MustParseAddr("203.0.113.9")
	refused := harness.server.refusals.List()
	if !slices.ContainsFunc(refused, func(client ratelimit.Refused) bool { return client.Client.Addr() == guesser }) {
		t.Fatalf("the refused clients are %v", refused)
	}
}

func (h *adminHarness) visit(signal, page, address, userAgent string, navigation bool) {
	h.t.Helper()
	body, _ := json.Marshal(api.Visit{Signal: signal, Page: page, Navigation: navigation})
	path := api.VisitsPath
	if signal == api.VisitDocument {
		path = api.DocumentVisitsPath
	}
	response, _ := h.do(&testRequest{
		method:  http.MethodPost,
		path:    path,
		body:    body,
		address: address,
		headers: map[string]string{"User-Agent": userAgent},
	})
	if response.StatusCode != http.StatusOK {
		h.t.Fatalf("visit: %d", response.StatusCode)
	}
}

// TestAK30VisitCountsKeepNoAddress covers the promise of spec §5.3 for the visit counts: what the server writes
// about visits is a count per hour, and neither the admin database nor the log holds the address of a visitor or of
// a client that a limit refused.
func TestAK30VisitCountsKeepNoAddress(t *testing.T) {
	harness := newAdminHarness(t)
	if status := harness.enroll(harness.openEnrollment(), newSoftwarePasskey(t)); status != http.StatusCreated {
		t.Fatalf("enrollment: %d", status)
	}
	const ip4, ip6 = "203.0.113.77", "2001:db8:77::1"
	started := time.Now()
	for _, address := range []string{ip4, ip6} {
		harness.visit(api.VisitDocument, api.PageHome, address, browserAgent, true)
		harness.visit(api.VisitPerson, api.PageHome, address, browserAgent, false)
		harness.visit(api.VisitDocument, api.PageTerms, address, "curl/8.17.0", false)
		harness.server.refusals.Note(netip.MustParseAddr(address), reasonCreate, time.Now())
	}
	now := time.Now()
	harness.server.keepAdmin(t.Context(), now, false)
	for _, resolution := range []adminstore.Resolution{adminstore.Hourly, adminstore.Daily} {
		if !adminstore.Hourly.Start(started).Equal(adminstore.Hourly.Start(now)) {
			break
		}
		running, readErr := harness.store.Counts(t.Context(), resolution, now.Add(-48*time.Hour), now.Add(48*time.Hour))
		if readErr != nil || len(running) != 0 {
			t.Fatalf("the minute's work wrote counts of a period that still runs: %v, %v", running, readErr)
		}
	}
	counted := harness.dailyCounts()
	if counted[visits.MetricPeople] != 2 || counted[visits.MetricVisitors] != 2 || counted[visits.MetricRobots] != 2 {
		t.Fatalf("the stored counts are %v", counted)
	}
	if err := harness.store.Close(); err != nil {
		t.Fatal(err)
	}
	raw6 := netip.MustParseAddr(ip6).As16()
	raw4 := netip.MustParseAddr(ip4).As4()
	needles := [][]byte{
		[]byte(ip4), []byte(ip6), []byte(netip.MustParseAddr(ip6).StringExpanded()), raw6[:], raw4[:],
		[]byte("curl/8.17.0"), []byte("Macintosh"),
	}
	for _, file := range adminstore.Files(harness.path) {
		content, readErr := os.ReadFile(file)
		if readErr != nil {
			t.Fatal(readErr)
		}
		for _, needle := range needles {
			if bytes.Contains(content, needle) {
				t.Fatalf("%s holds %q", filepath.Base(file), needle)
			}
		}
	}
	harness.server.flushCounts(t.Context())
	logs := harness.logs.String()
	for _, needle := range []string{ip4, ip6, "curl/8.17.0", "Macintosh"} {
		if strings.Contains(logs, needle) {
			t.Fatalf("the log holds %q of a visitor", needle)
		}
	}
}

// A stop counts the pages that still wait as unconfirmed and sets the counts of the running hour and day aside,
// where they are no rows of a chart, and the next start takes them back: a restart loses no count and writes each
// once (spec §12).
func TestARestartKeepsTheRunningCounts(t *testing.T) {
	harness := newAdminHarness(t)
	if status := harness.enroll(harness.openEnrollment(), newSoftwarePasskey(t)); status != http.StatusCreated {
		t.Fatalf("enrollment: %d", status)
	}
	harness.visit(api.VisitDocument, api.PageHome, "203.0.113.20", browserAgent, true)
	harness.visit(api.VisitPerson, api.PageTerms, "203.0.113.21", browserAgent, false)
	settings := baseSettings(pepperA, nil)
	settings.PublicURL = adminOrigin
	settings.EdgeReports = true
	harness.reopen(settings)
	now := time.Now()
	for _, resolution := range []adminstore.Resolution{adminstore.Hourly, adminstore.Daily} {
		rows, err := harness.store.Counts(t.Context(), resolution, now.Add(-48*time.Hour), now.Add(48*time.Hour))
		if err != nil || len(rows) != 0 {
			t.Fatalf("a stopped server left counts of a running period among the charts' rows: %v, %v", rows, err)
		}
	}
	if err := harness.server.StartCounting(t.Context()); err != nil {
		t.Fatal(err)
	}
	counted := harness.dailyCounts()
	if counted[visits.MetricPeople] != 1 || counted[visits.MetricVisitors] != 1 ||
		counted[visits.MetricUnconfirmed] != 1 {
		t.Fatalf("after a restart the counts are %v", counted)
	}
	if again := harness.dailyCounts(); again[visits.MetricPeople] != 1 || again[visits.MetricVisitors] != 1 {
		t.Fatalf("a count was written twice: %v", again)
	}
}

// A page that shows a message promises that it counts no visit, so the server must not count one for it either.
func TestVisitSignalsForMessagePagesAreDropped(t *testing.T) {
	harness := newAdminHarness(t)
	if status := harness.enroll(harness.openEnrollment(), newSoftwarePasskey(t)); status != http.StatusCreated {
		t.Fatalf("enrollment: %d", status)
	}
	for _, page := range []string{"read", "r", "m", "admin", ""} {
		harness.visit(api.VisitPerson, page, "203.0.113.5", browserAgent, false)
	}
	response, _ := harness.do(&testRequest{
		method:  http.MethodGet,
		path:    "/r",
		address: "203.0.113.5",
		headers: map[string]string{
			"User-Agent":     browserAgent,
			"Sec-Fetch-Mode": "navigate",
			"Sec-Fetch-Dest": "document",
		},
	})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("the reader page: %d", response.StatusCode)
	}
	harness.server.visits.Stop(time.Now())
	if stored := harness.dailyCounts(); len(stored) != 0 {
		t.Fatalf("a message page was counted: %v", stored)
	}
}

func (h *adminHarness) dailyCounts() map[string]int64 {
	h.t.Helper()
	now := time.Now()
	h.server.flushStatistics(h.t.Context(), now.Add(48*time.Hour))
	stored, err := h.store.Counts(h.t.Context(), adminstore.Daily, now.Add(-24*time.Hour), now.Add(24*time.Hour))
	if err != nil {
		h.t.Fatal(err)
	}
	counted := map[string]int64{}
	for _, count := range stored {
		counted[count.Metric] += count.Value
	}
	return counted
}

// Only the party that served a page reports its document (spec §6.7): a client cannot report one on the public
// route, the report route answers a peer that is no trusted proxy, and any peer of a server that was not told its
// proxy is the nylzo frontend, as an address the API does not have; a page the server serves itself is counted.
func TestDocumentReportsComeOnlyFromWhoeverServedThePage(t *testing.T) {
	report := func(harness *adminHarness, path string) int {
		body, _ := json.Marshal(api.Visit{Signal: api.VisitDocument, Page: api.PageHome, Navigation: true})
		response, _ := harness.do(&testRequest{
			method:  http.MethodPost,
			path:    path,
			body:    body,
			headers: map[string]string{"User-Agent": "curl/8.17.0"},
		})
		return response.StatusCode
	}
	cases := []struct {
		name    string
		change  func(*config.Server)
		path    string
		status  int
		counted int64
	}{
		{"the frontend", func(*config.Server) {}, api.DocumentVisitsPath, http.StatusOK, 1},
		{"a client on the public route", func(*config.Server) {}, api.VisitsPath, http.StatusOK, 0},
		{
			"a peer that is no trusted proxy",
			func(settings *config.Server) { settings.TrustedProxies = nil },
			api.DocumentVisitsPath, http.StatusNotFound, 0,
		},
		{
			"a proxy that is not the frontend",
			func(settings *config.Server) { settings.EdgeReports = false },
			api.DocumentVisitsPath, http.StatusNotFound, 0,
		},
	}
	for _, test := range cases {
		harness := newAdminHarness(t, test.change)
		if status := harness.enroll(harness.openEnrollment(), newSoftwarePasskey(t)); status != http.StatusCreated {
			t.Fatalf("%s: enrollment %d", test.name, status)
		}
		if status := report(harness, test.path); status != test.status {
			t.Errorf("%s got %d, want %d", test.name, status, test.status)
		}
		if counted := harness.dailyCounts()[visits.MetricRobots]; counted != test.counted {
			t.Errorf("%s counted %d robots, want %d", test.name, counted, test.counted)
		}
	}
	harness := newAdminHarness(t)
	if status := harness.enroll(harness.openEnrollment(), newSoftwarePasskey(t)); status != http.StatusCreated {
		t.Fatalf("enrollment: %d", status)
	}
	response, _ := harness.do(&testRequest{
		method:  http.MethodGet,
		path:    "/terms",
		headers: map[string]string{"User-Agent": "Mozilla/5.0 (compatible; Googlebot/2.1)"},
	})
	if counted := harness.dailyCounts(); response.StatusCode != http.StatusOK ||
		counted[visits.RobotPrefix+"search"] != 1 {
		t.Fatalf("a page the server served itself: %d, %v", response.StatusCode, counted)
	}
}

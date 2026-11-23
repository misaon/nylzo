package adminapi

import (
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/core/signing"
	"github.com/misaon/nylzo/internal/server/adminstore"
)

func TestStateTellsWhetherAPasskeyCanSignIn(t *testing.T) {
	t.Parallel()
	area := newTestArea(t, testOrigin)
	state := func() answer { return area.serve(httptest.NewRequest(http.MethodGet, api.AdminPath, http.NoBody)) }
	if got := state(); got.status != http.StatusOK || got.fields["enrolled"] != false {
		t.Fatalf("an empty admin area: %d %v", got.status, got.fields)
	}
	area.store.held = []adminstore.Passkey{newSoftwarePasskey(t).stored(t, time.Time{})}
	if got := state(); got.status != http.StatusOK || got.fields["enrolled"] != true {
		t.Fatalf("an admin area with a passkey: %d %v", got.status, got.fields)
	}
	area.store.failing["CountPasskeys"] = true
	if got := state(); got.status != http.StatusInternalServerError || got.fields["error"] != api.ErrorInternal {
		t.Fatalf("a failing store: %d %v", got.status, got.fields)
	}
	if !strings.Contains(area.events.String(), errStoreBroken.Error()) {
		t.Fatalf("the failure was not logged: %s", area.events.String())
	}
}

func TestAdminRefusesBodiesThatAreNotItsJSON(t *testing.T) {
	t.Parallel()
	area := newTestArea(t, testOrigin)
	for _, path := range []string{"/enrollment/options", "/enrollment", "/session/options", "/session"} {
		wantStatus(t, path, area.postRaw(path, testRelyingParty, "{"), http.StatusBadRequest)
	}
	signedIn := area.openSession(time.Time{})
	request := httptest.NewRequest(http.MethodDelete, api.AdminPath+"/session", strings.NewReader("body"))
	request.Header.Set("Authorization", signing.SignRequest(
		signedIn.seed, signing.PurposeAdmin, http.MethodDelete, api.AdminPath+"/session",
		[signing.ChallengeLen]byte{1}, nil, signing.Components{},
	).Authorization())
	wantStatus(t, "a signed request with a body", area.serve(request), http.StatusBadRequest)
	if _, live := area.area.sessions.touch(signedIn.key, time.Now()); !live {
		t.Fatal("a refused sign-out ended the session")
	}
}

func TestTheRelyingPartyIsThePublicURLOrALoopbackHost(t *testing.T) {
	t.Parallel()
	key := signing.PublicKey(core.NewSecret())
	options := map[string]string{"key": core.Encode(key[:])}
	unnamed := newTestArea(t, "")
	encoded := `{"key":"` + options["key"] + `"}`
	loopback := unnamed.postRaw("/session/options", "127.0.0.1:8080", encoded)
	publicKey, _ := loopback.fields["public_key"].(map[string]any)
	if loopback.status != http.StatusOK || publicKey["rpId"] != "127.0.0.1" {
		t.Fatalf("a loopback host without a public URL: %d %v", loopback.status, loopback.fields)
	}
	for _, path := range []string{"/enrollment/options", "/enrollment", "/session/options", "/session"} {
		wantStatus(t, "a forged host "+path, unnamed.postRaw(path, "nylzo.example", encoded), http.StatusBadRequest)
	}
	if len(unnamed.refused) != 0 {
		t.Fatalf("a request without a relying party counted as a refused sign-in: %v", unnamed.refused)
	}
	area := newTestArea(t, testOrigin)
	wantStatus(t, "an invalid session key", area.post("/session/options", map[string]string{"key": "AA"}),
		http.StatusBadRequest)
	withPort := newTestArea(t, testOrigin+":443")
	relyingParty, known := withPort.area.relyingParty(httptest.NewRequest(http.MethodPost, "/", http.NoBody))
	if !known || relyingParty.ID != testRelyingParty || relyingParty.Origin != testOrigin {
		t.Fatalf("a public URL with the default port: %v %+v", known, relyingParty)
	}
}

func TestEnrollmentRefusesWhatItCannotVerify(t *testing.T) {
	t.Parallel()
	token := core.Encode([]byte("an enrollment token"))
	area := newTestArea(t, testOrigin)
	area.store.failing["EnrollmentOpen"] = true
	wantStatus(t, "a failing enrollment lookup",
		area.post("/enrollment/options", map[string]string{"token": token}), http.StatusInternalServerError)
	area.store.failing["EnrollmentOpen"] = false
	area.store.open = true
	area.store.failing["Passkeys"] = true
	wantStatus(t, "a failing passkey list",
		area.post("/enrollment/options", map[string]string{"token": token}), http.StatusInternalServerError)
	area.store.failing["Passkeys"] = false

	key := newSoftwarePasskey(t)
	incomplete := area.registration(key, token, area.enrollmentChallenge(token))
	delete(incomplete, "public_key")
	wantStatus(t, "an incomplete registration", area.post("/enrollment", incomplete), http.StatusUnauthorized)
	unreadable := area.registration(key, token, area.enrollmentChallenge(token))
	unreadable["client_data"] = core.Encode([]byte("not json"))
	wantStatus(t, "unreadable client data", area.post("/enrollment", unreadable), http.StatusUnauthorized)
	unknown := area.registration(key, token, make([]byte, ceremonyChallenge))
	wantStatus(t, "a ceremony the server never began", area.post("/enrollment", unknown), http.StatusUnauthorized)
	if len(area.refused) != 3 {
		t.Fatalf("refused enrollments were not noted: %v", area.refused)
	}

	area.store.failing["Enroll"] = true
	wantStatus(t, "a failing enrollment",
		area.post("/enrollment", area.registration(key, token, area.enrollmentChallenge(token))),
		http.StatusInternalServerError)
	area.store.failing["Enroll"] = false
	challenge := area.enrollmentChallenge(token)
	area.store.failing["Passkeys"] = true
	if enrolled := area.post("/enrollment", area.registration(key, token, challenge)); enrolled.status !=
		http.StatusCreated || enrolled.fields["enrolled"] != true {
		t.Fatalf("an enrollment whose sync failed: %d %v", enrolled.status, enrolled.fields)
	}
	if !strings.Contains(area.events.String(), "admin-store") {
		t.Fatalf("the failed sync was not logged: %s", area.events.String())
	}
}

func TestSignInRefusesWhatItCannotVerify(t *testing.T) {
	t.Parallel()
	area := newTestArea(t, testOrigin)
	key := newSoftwarePasskey(t)
	area.store.held = []adminstore.Passkey{key.stored(t, time.Time{})}

	incomplete := area.signInRequest(key).request
	delete(incomplete, "signature")
	wantStatus(t, "an incomplete assertion", area.post("/session", incomplete), http.StatusUnauthorized)
	forged := area.signInRequest(key).request
	forged["signature"] = area.signInRequest(newSoftwarePasskey(t)).request["signature"]
	wantStatus(t, "a signature of another key", area.post("/session", forged), http.StatusUnauthorized)
	if len(area.refused) != 2 {
		t.Fatalf("refused sign-ins were not noted: %v", area.refused)
	}

	for _, method := range []string{"Passkey", "Passkeys", "RecordUse"} {
		area.store.failing[method] = true
		wantStatus(t, "a failing "+method, area.post("/session", area.signInRequest(key).request),
			http.StatusInternalServerError)
		area.store.failing[method] = false
	}
	area.store.notRecorded = true
	wantStatus(t, "a use another sign-in recorded first", area.post("/session", area.signInRequest(key).request),
		http.StatusUnauthorized)
	area.store.notRecorded = false

	raced := area.signInRequest(key)
	area.store.beforeRecord = func() {
		area.area.sessions.open(raced.key, key.id, raced.challenge, time.Time{}, time.Now())
	}
	wantStatus(t, "a challenge another request used meanwhile", area.post("/session", raced.request),
		http.StatusUnauthorized)
	area.store.beforeRecord = nil

	lastUsed := time.Unix(1_700_000_000, 0)
	area.store.held = []adminstore.Passkey{
		key.stored(t, lastUsed.Add(-time.Hour)),
		newSoftwarePasskey(t).stored(t, lastUsed),
	}
	area.challenges.exhausted = true
	accepted := area.signInRequest(key)
	signedIn := area.post("/session", accepted.request)
	if _, hasChallenge := signedIn.fields["challenge"]; signedIn.status != http.StatusOK || hasChallenge {
		t.Fatalf("a sign-in while no challenge can be issued: %d %v", signedIn.status, signedIn.fields)
	}
	if held, live := area.area.sessions.touch(accepted.key, time.Now()); !live || !held.previous.Equal(lastUsed) {
		t.Fatalf("the session does not name the latest earlier sign-in: %v %v", live, held.previous)
	}
}

func TestOverviewAnswersOnlyAKnownRangeItCouldRead(t *testing.T) {
	t.Parallel()
	area := newTestArea(t, testOrigin)
	previous := time.Unix(1_700_000_000, 0)
	signedIn := area.openSession(previous)
	overview := func(query string) answer {
		return area.signed(signedIn, http.MethodGet, api.AdminPath+"/overview"+query)
	}
	wantStatus(t, "an unknown range", overview("?range=1y"), http.StatusBadRequest)
	daily := overview("?range=30d")
	security, _ := daily.fields["security"].(map[string]any)
	if daily.status != http.StatusOK || daily.fields["resolution_s"] != float64(adminstore.Daily) ||
		security["last_sign_in"] != float64(previous.Unix()) || daily.fields["previous"] == nil {
		t.Fatalf("thirty days: %d %v", daily.status, daily.fields)
	}
	area.stored = errStoreBroken
	wantStatus(t, "a failing message count", overview("?range=24h"), http.StatusInternalServerError)
	area.stored = nil
	area.store.failing["Passkeys"] = true
	wantStatus(t, "a failing passkey list", overview("?range=24h"), http.StatusInternalServerError)
	area.store.failing["Passkeys"] = false
	area.counts.Close()
	wantStatus(t, "a failing count store", overview("?range=12m"), http.StatusInternalServerError)
}

func TestLiveSendsOnlyTheSamplesAfterSince(t *testing.T) {
	t.Parallel()
	area := newTestArea(t, testOrigin)
	signedIn := area.openSession(time.Time{})
	live := func(query string) answer { return area.signed(signedIn, http.MethodGet, api.AdminPath+"/live"+query) }
	wantStatus(t, "a negative since", live("?since=-1"), http.StatusBadRequest)
	all := live("")
	usage, _ := all.fields["usage"].([]any)
	if all.status != http.StatusOK || len(usage) != 1 {
		t.Fatalf("the first live answer: %d %v", all.status, all.fields)
	}
	sample, _ := usage[0].(map[string]any)
	at, _ := sample["at"].(float64)
	after := live("?since=" + strconv.FormatInt(int64(at), 10))
	if later, _ := after.fields["usage"].([]any); after.status != http.StatusOK || len(later) != 0 {
		t.Fatalf("a sample the page holds was sent again: %d %v", after.status, after.fields)
	}
}

func TestSyncEndsTheSessionsOfARemovedPasskeyAndCeremoniesThatRanOut(t *testing.T) {
	t.Parallel()
	area := newTestArea(t, testOrigin)
	signedIn := area.openSession(time.Time{})
	area.store.held = []adminstore.Passkey{{ID: []byte("credential")}}
	if err := area.area.Sync(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, live := area.area.sessions.touch(signedIn.key, time.Now()); !live {
		t.Fatal("a sync ended the session of a passkey the instance holds")
	}
	token := adminstore.HashToken([]byte("token"))
	area.area.sessions.beginEnrollment(token, time.Now().Add(-ceremonyLife))
	area.area.sessions.beginEnrollment(token, time.Now())
	area.store.held = nil
	if err := area.area.Sync(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if held := len(area.area.sessions.enrollments); held != 1 {
		t.Fatalf("a sync kept %d enrollment ceremonies, want only the one that has not run out", held)
	}
	if _, live := area.area.sessions.touch(signedIn.key, time.Now()); live {
		t.Fatal("a session outlived its removed passkey")
	}
	area.store.failing["Passkeys"] = true
	if err := area.area.Sync(t.Context(), time.Now()); err == nil {
		t.Fatal("a failing store was not reported")
	}
}

func TestAnAnswerThatCannotBeEncodedIsAnInternalError(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	writeJSON(recorder, http.StatusOK, math.NaN())
	if recorder.Code != http.StatusInternalServerError || recorder.Body.String() != `{"error":"internal"}` {
		t.Fatalf("got %d %q", recorder.Code, recorder.Body.String())
	}
}

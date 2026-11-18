package adminapi

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"maps"
	"net/http"
	"sync"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/core/signing"
	"github.com/misaon/nylzo/internal/server/adminstore"
	"github.com/misaon/nylzo/internal/server/passkey"
	"github.com/misaon/nylzo/internal/server/ratelimit"
)

const (
	// sessionIdle ends a session that sent no request for this long.
	sessionIdle = 15 * time.Minute
	// sessionLife ends a session however busy it is.
	sessionLife = 12 * time.Hour
	// ceremonyLife is the timeout the WebAuthn recommendation gives a ceremony.
	ceremonyLife = 5 * time.Minute
	// clockSkew is how far past its lifetime a sign-in challenge may still lie ahead, as after a small clock step.
	clockSkew          = 5 * time.Second
	ceremonyChallenge  = 32
	maxSessions        = 4
	maxEnrollments     = 16
	signInNonceLength  = 12
	signInSigned       = signInNonceLength + 4
	signInLabel        = "nylzo/admin/sign-in/"
	ceremonyTimeoutMs  = int64(ceremonyLife / time.Millisecond)
	userVerification   = "required"
	publicKeyType      = "public-key"
	relyingPartyName   = "Nylzo"
	operatorName       = "operator"
	operatorTitle      = "Nylzo operator"
	userHandleLabel    = "nylzo/admin/user/"
	userHandleLength   = 16
	residentKeyRequire = "required"
)

type sessionKey [signing.PublicKeyLen]byte

type session struct {
	createdAt  time.Time
	lastSeen   time.Time
	credential string
	// previous is the sign-in before the one that opened this session, so the operator can tell a sign-in of
	// someone else from their own.
	previous time.Time
}

func (s *session) live(now time.Time) bool {
	return now.Sub(s.lastSeen) < sessionIdle && now.Sub(s.createdAt) < sessionLife
}

// endsAt is when the session ends however busy it is; every request moves the idle limit, so an answer that
// named it would always say fifteen minutes from now.
func (s *session) endsAt() time.Time {
	return s.createdAt.Add(sessionLife)
}

// An enrollment is the WebAuthn challenge of a ceremony that adds a passkey for a token.
type enrollment struct {
	expiresAt time.Time
	token     adminstore.TokenHash
}

type signInNonce [signInNonceLength]byte

// sessions holds no state for a sign-in that is under way: its challenge carries its own expiry and a tag that
// binds it to the session key, so requests that only ask for challenges cannot push the operator's own one out.
type sessions struct {
	mutex       sync.Mutex
	secret      [sha256.Size]byte
	live        map[sessionKey]*session
	enrollments map[[ceremonyChallenge]byte]enrollment
	signedIn    map[signInNonce]time.Time
}

func newSessions() *sessions {
	opened := &sessions{
		live:        make(map[sessionKey]*session),
		enrollments: make(map[[ceremonyChallenge]byte]enrollment),
		signedIn:    make(map[signInNonce]time.Time),
	}
	rand.Read(opened.secret[:])
	return opened
}

// beginEnrollment hands out the challenge of a ceremony that only the holder of an open token can start; the
// oldest ceremony makes room when the process holds as many as it may.
func (s *sessions) beginEnrollment(token adminstore.TokenHash, now time.Time) []byte {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if len(s.enrollments) >= maxEnrollments {
		var oldest [ceremonyChallenge]byte
		for challenge, out := range s.enrollments {
			if first, ok := s.enrollments[oldest]; !ok || out.expiresAt.Before(first.expiresAt) {
				oldest = challenge
			}
		}
		delete(s.enrollments, oldest)
	}
	var challenge [ceremonyChallenge]byte
	rand.Read(challenge[:])
	s.enrollments[challenge] = enrollment{expiresAt: now.Add(ceremonyLife), token: token}
	return challenge[:]
}

// finishEnrollment uses the ceremony up, whatever comes of it.
func (s *sessions) finishEnrollment(challenge []byte, now time.Time) (enrollment, bool) {
	if len(challenge) != ceremonyChallenge {
		return enrollment{}, false
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	key := [ceremonyChallenge]byte(challenge)
	found, ok := s.enrollments[key]
	delete(s.enrollments, key)
	return found, ok && now.Before(found.expiresAt)
}

// signInChallenge is a nonce, the second it runs out and a tag over both and the session key.
func (s *sessions) signInChallenge(key sessionKey, now time.Time) []byte {
	challenge := make([]byte, signInNonceLength, ceremonyChallenge)
	rand.Read(challenge)
	//nolint:gosec // G115: Unix seconds fit 32 bits until 2106.
	challenge = binary.BigEndian.AppendUint32(challenge, uint32(now.Add(ceremonyLife).Unix()))
	return append(challenge, s.signInTag(challenge, key)...)
}

func (s *sessions) signInTag(signed []byte, key sessionKey) []byte {
	mac := hmac.New(sha256.New, s.secret[:])
	mac.Write([]byte(signInLabel))
	mac.Write(signed)
	mac.Write(key[:])
	return mac.Sum(nil)[:ceremonyChallenge-signInSigned]
}

// signInOpen tells whether this process issued the challenge for this session key and it has neither run out nor
// opened a session yet.
func (s *sessions) signInOpen(challenge []byte, key sessionKey, now time.Time) bool {
	if len(challenge) != ceremonyChallenge ||
		!hmac.Equal(challenge[signInSigned:], s.signInTag(challenge[:signInSigned], key)) {
		return false
	}
	expires := signInExpiry(challenge)
	s.mutex.Lock()
	defer s.mutex.Unlock()
	_, used := s.signedIn[signInNonce(challenge[:signInNonceLength])]
	return !used && now.Before(expires) && expires.Sub(now) <= ceremonyLife+clockSkew
}

// signInExpiry is the wall-clock second a sign-in challenge runs out.
func signInExpiry(challenge []byte) time.Time {
	return time.Unix(int64(binary.BigEndian.Uint32(challenge[signInNonceLength:signInSigned])), 0)
}

// open starts the session of a key for the passkey that signed its challenge, which then opens no other session;
// the oldest session makes room when the process holds as many as it may.
func (s *sessions) open(
	key sessionKey,
	credentialID, challenge []byte,
	previous, now time.Time,
) (*session, bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	nonce := signInNonce(challenge[:signInNonceLength])
	if _, used := s.signedIn[nonce]; used {
		return nil, false
	}
	s.signedIn[nonce] = now.Add(ceremonyLife + clockSkew)
	maps.DeleteFunc(s.live, func(_ sessionKey, held *session) bool { return !held.live(now) })
	if _, held := s.live[key]; !held && len(s.live) >= maxSessions {
		var oldest sessionKey
		for candidate, held := range s.live {
			if first, ok := s.live[oldest]; !ok || held.createdAt.Before(first.createdAt) {
				oldest = candidate
			}
		}
		delete(s.live, oldest)
	}
	opened := &session{createdAt: now, lastSeen: now, credential: string(credentialID), previous: previous}
	s.live[key] = opened
	return opened, true
}

func (s *sessions) touch(key sessionKey, now time.Time) (signedSession, bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	held, ok := s.live[key]
	if !ok || !held.live(now) {
		delete(s.live, key)
		return signedSession{}, false
	}
	held.lastSeen = now
	return signedSession{key: key, expiresAt: held.endsAt(), previous: held.previous}, true
}

func (s *sessions) close(key sessionKey) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	delete(s.live, key)
}

// keep ends the sessions that ran out and those of a passkey that the instance no longer holds.
func (s *sessions) keep(enrolled func(credentialID string) bool, now time.Time) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.live = ratelimit.Rebuilt(s.live, func(held *session) bool { return held.live(now) && enrolled(held.credential) })
	s.enrollments = ratelimit.Rebuilt(s.enrollments, func(out enrollment) bool { return now.Before(out.expiresAt) })
	s.signedIn = ratelimit.Rebuilt(s.signedIn, now.Before)
}

type sessionContextKey struct{}

type signedSession struct {
	key       sessionKey
	expiresAt time.Time
	previous  time.Time
}

// signed lets a request through when a live session signed it over a challenge that the server issued and that
// this request used up (spec section 6.8).
func (a *API) signed(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// The signature covers an empty body, so a request that carries one is not the request that was signed.
		if r.ContentLength != 0 {
			writeError(w, http.StatusBadRequest, api.ErrorBadRequest)
			return
		}
		signature, err := signing.ParseAuthorization(r.Header.Get("Authorization"))
		if err != nil || !a.Spent(r, signature.Challenge) ||
			!signature.Verify(signing.PurposeAdmin, r.Method, r.URL.RequestURI(), nil, signing.Components{}) {
			writeError(w, http.StatusUnauthorized, api.ErrorUnauthorized)
			return
		}
		signedIn, ok := a.sessions.touch(signature.PublicKey, time.Now())
		if !ok {
			writeError(w, http.StatusUnauthorized, api.ErrorUnauthorized)
			return
		}
		next(w, withSession(r, &signedIn))
	}
}

type sessionOptionsRequest struct {
	Key string `json:"key"`
}

type requestOptions struct {
	Challenge        string                 `json:"challenge"`
	RelyingPartyID   string                 `json:"rpId"`
	Timeout          int64                  `json:"timeout"`
	UserVerification string                 `json:"userVerification"`
	AllowCredentials []credentialDescriptor `json:"allowCredentials"`
}

type ceremonyResponse[Options any] struct {
	PublicKey Options `json:"public_key"`
}

func (a *API) handleSessionOptions(w http.ResponseWriter, r *http.Request) {
	var request sessionOptionsRequest
	if !readJSON(w, r, &request) {
		return
	}
	var key sessionKey
	relyingParty, known := a.relyingParty(r)
	if !known || !core.DecodeInto(key[:], request.Key) || !signing.ValidPublicKey(key[:]) {
		writeError(w, http.StatusBadRequest, api.ErrorBadRequest)
		return
	}
	challenge := a.sessions.signInChallenge(key, time.Now())
	writeJSON(w, http.StatusOK, ceremonyResponse[requestOptions]{PublicKey: requestOptions{
		Challenge:        core.Encode(challenge),
		RelyingPartyID:   relyingParty.ID,
		Timeout:          ceremonyTimeoutMs,
		UserVerification: userVerification,
		AllowCredentials: []credentialDescriptor{},
	}})
}

type sessionRequest struct {
	Key               string `json:"key"`
	CredentialID      string `json:"credential_id"`
	ClientData        string `json:"client_data"`
	AuthenticatorData string `json:"authenticator_data"`
	Signature         string `json:"signature"`
}

type sessionResponse struct {
	Challenge string `json:"challenge,omitempty"`
	ExpiresAt int64  `json:"expires_at"`
}

func (a *API) handleSession(w http.ResponseWriter, r *http.Request) {
	var request sessionRequest
	if !readJSON(w, r, &request) {
		return
	}
	assertion, complete := decodeAssertion(&request)
	relyingParty, known := a.relyingParty(r)
	var key sessionKey
	if !known {
		writeError(w, http.StatusBadRequest, api.ErrorBadRequest)
		return
	}
	if !complete || !core.DecodeInto(key[:], request.Key) {
		a.refuseSignIn(w, r)
		return
	}
	now := time.Now()
	challenge, err := passkey.ChallengeOf(assertion.ClientDataJSON)
	if err != nil || !a.sessions.signInOpen(challenge, key, now) {
		a.refuseSignIn(w, r)
		return
	}
	stored, found, err := a.Store.Passkey(r.Context(), assertion.CredentialID)
	if err != nil {
		a.writeInternal(w, r, err)
		return
	}
	if !found {
		a.refuseSignIn(w, r)
		return
	}
	verified, err := relyingParty.VerifyAssertion(assertion, challenge, &stored.Credential)
	if err != nil {
		a.refuseSignIn(w, r)
		return
	}
	previous, err := a.lastSignIn(r.Context())
	if err != nil {
		a.writeInternal(w, r, err)
		return
	}
	recorded, err := a.Store.RecordUse(r.Context(), &verified, now)
	if err != nil {
		a.writeInternal(w, r, err)
		return
	}
	if !recorded {
		a.refuseSignIn(w, r)
		return
	}
	opened, first := a.sessions.open(key, verified.ID, challenge, previous, now)
	if !first {
		a.refuseSignIn(w, r)
		return
	}
	a.Events.InfoContext(r.Context(), "admin-signed-in")
	writeJSON(w, http.StatusOK, sessionResponse{
		Challenge: a.nextChallenge(now),
		ExpiresAt: opened.endsAt().Unix(),
	})
}

// lastSignIn is the latest moment any passkey of the instance signed in.
func (a *API) lastSignIn(ctx context.Context) (time.Time, error) {
	held, err := a.Store.Passkeys(ctx)
	var latest time.Time
	for index := range held {
		if used := held[index].LastUsedAt; used.After(latest) {
			latest = used
		}
	}
	return latest, err
}

func decodeAssertion(request *sessionRequest) (*passkey.Assertion, bool) {
	var (
		assertion passkey.Assertion
		complete  [4]bool
	)
	assertion.CredentialID, complete[0] = decode(request.CredentialID)
	assertion.ClientDataJSON, complete[1] = decode(request.ClientData)
	assertion.AuthenticatorData, complete[2] = decode(request.AuthenticatorData)
	assertion.Signature, complete[3] = decode(request.Signature)
	return &assertion, complete == [4]bool{true, true, true, true}
}

func (a *API) handleSignOut(w http.ResponseWriter, r *http.Request) {
	a.sessions.close(sessionOf(r).key)
	writeJSON(w, http.StatusOK, struct{}{})
}

func sameToken(a, b adminstore.TokenHash) bool {
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}

// Package adminapi serves the operator's admin area: enrolling a passkey with a one-time token, signing in with
// it, and the figures of the dashboard. A session is an Ed25519 key that the browser keeps in the memory of one
// tab; every request is signed with it over a challenge that works once, so the area needs no cookie.
package adminapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/core/signing"
	"github.com/misaon/nylzo/internal/loopback"
	"github.com/misaon/nylzo/internal/server/adminstore"
	"github.com/misaon/nylzo/internal/server/country"
	"github.com/misaon/nylzo/internal/server/hostusage"
	"github.com/misaon/nylzo/internal/server/logging"
	"github.com/misaon/nylzo/internal/server/passkey"
	"github.com/misaon/nylzo/internal/server/ratelimit"
	"github.com/misaon/nylzo/internal/server/statistics"
	"github.com/misaon/nylzo/internal/server/visits"
)

var defaultPorts = map[string]string{"https": "443", "http": "80"}

const (
	maxBodyBytes = 32 << 10
	// reasonSignIn names a refused enrollment or sign-in among the refused clients.
	reasonSignIn = "admin"
)

type Store interface {
	EnrollmentOpen(ctx context.Context, token adminstore.TokenHash, now time.Time) (bool, error)
	Enroll(ctx context.Context, token adminstore.TokenHash, credential *passkey.Credential, now time.Time) error
	Passkey(ctx context.Context, credentialID []byte) (adminstore.Passkey, bool, error)
	Passkeys(ctx context.Context) ([]adminstore.Passkey, error)
	CountPasskeys(ctx context.Context) (int, error)
	RecordUse(ctx context.Context, credential *passkey.Credential, usedAt time.Time) (bool, error)
}

type Countries interface {
	Lookup(address netip.Addr) country.Code
	Source() country.Source
}

type Challenges interface {
	Issue(now time.Time) ([signing.ChallengeLen]byte, time.Time, bool)
}

type Config struct {
	Store      Store
	PublicURL  string
	Challenges Challenges
	// Spent tells whether the request presented this challenge and the server used it up for it.
	Spent     func(r *http.Request, challenge [signing.ChallengeLen]byte) bool
	Counts    *statistics.Counts
	Visits    *visits.Tracker
	Usage     *hostusage.Sampler
	Refusals  *ratelimit.Refusals
	Countries Countries
	// Stored counts the messages the server holds.
	Stored        func(ctx context.Context) (int, error)
	DataDirectory string
	ReserveBytes  int64
	Version       string
	Events        *slog.Logger
	// Refuse puts the client of a refused request on the list of refused clients.
	Refuse func(r *http.Request, reason string)
}

type API struct {
	Config

	sessions *sessions
}

func New(config *Config) *API {
	return &API{Config: *config, sessions: newSessions()}
}

func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET "+api.AdminPath, a.handleState)
	mux.HandleFunc("POST "+api.AdminPath+"/enrollment/options", a.handleEnrollmentOptions)
	mux.HandleFunc("POST "+api.AdminPath+"/enrollment", a.handleEnrollment)
	mux.HandleFunc("POST "+api.AdminPath+"/session/options", a.handleSessionOptions)
	mux.HandleFunc("POST "+api.AdminPath+"/session", a.handleSession)
	mux.HandleFunc("DELETE "+api.AdminPath+"/session", a.signed(a.handleSignOut))
	mux.HandleFunc("GET "+api.AdminPath+"/overview", a.signed(a.handleOverview))
	mux.HandleFunc("GET "+api.AdminPath+"/live", a.signed(a.handleLive))
}

// Sync counts visits and messages only while a passkey can sign in to see them, and ends the sessions that ran out
// and those of a passkey that was removed, so a reset on the server host ends every session within a minute.
func (a *API) Sync(ctx context.Context, now time.Time) error {
	held, err := a.Store.Passkeys(ctx)
	if err != nil {
		return err
	}
	enrolled := make(map[string]struct{}, len(held))
	for index := range held {
		enrolled[string(held[index].ID)] = struct{}{}
	}
	a.sessions.keep(func(credentialID string) bool {
		_, ok := enrolled[credentialID]
		return ok
	}, now)
	a.Counts.Enable(len(held) > 0)
	return nil
}

type stateResponse struct {
	Enrolled bool `json:"enrolled"`
}

func (a *API) handleState(w http.ResponseWriter, r *http.Request) {
	held, err := a.Store.CountPasskeys(r.Context())
	if err != nil {
		a.writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, stateResponse{Enrolled: held > 0})
}

// relyingParty is the public URL of the instance; without one, only a loopback host may stand in for it, so a
// forged Host header never names the relying party.
func (a *API) relyingParty(r *http.Request) (passkey.RelyingParty, bool) {
	origin := a.PublicURL
	if origin == "" {
		origin = "http://" + r.Host
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Hostname() == "" || (a.PublicURL == "" && !loopback.Host(parsed.Hostname())) {
		return passkey.RelyingParty{}, false
	}
	// A browser leaves the default port of a scheme out of the origin it signs.
	host := parsed.Host
	if port := parsed.Port(); port == defaultPorts[parsed.Scheme] {
		host = strings.TrimSuffix(host, ":"+port)
	}
	return passkey.RelyingParty{ID: parsed.Hostname(), Origin: parsed.Scheme + "://" + host}, true
}

func (a *API) nextChallenge(now time.Time) string {
	challenge, _, issued := a.Challenges.Issue(now)
	if !issued {
		return ""
	}
	return core.Encode(challenge[:])
}

func (a *API) refuseSignIn(w http.ResponseWriter, r *http.Request) {
	a.Events.WarnContext(r.Context(), "admin-sign-in-refused")
	a.refuse(w, r)
}

func (a *API) refuseEnrollment(w http.ResponseWriter, r *http.Request) {
	a.Events.WarnContext(r.Context(), "admin-enrollment-refused")
	a.refuse(w, r)
}

func (a *API) refuse(w http.ResponseWriter, r *http.Request) {
	a.Refuse(r, reasonSignIn)
	writeError(w, http.StatusUnauthorized, api.ErrorUnauthorized)
}

func (a *API) writeInternal(w http.ResponseWriter, r *http.Request, err error) {
	a.Events.ErrorContext(r.Context(), "admin", "err", logging.ErrorText(err))
	writeError(w, http.StatusInternalServerError, api.ErrorInternal)
}

func readJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil || json.Unmarshal(body, target) != nil {
		writeError(w, http.StatusBadRequest, api.ErrorBadRequest)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		body, status = []byte(`{"error":"internal"}`), http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	w.Write(body)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, api.ErrorResponse{Error: code})
}

var canonicalBase64URL = base64.RawURLEncoding.Strict()

func decode(encoded string) ([]byte, bool) {
	decoded, err := canonicalBase64URL.DecodeString(encoded)
	return decoded, err == nil && len(decoded) > 0
}

package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/core/keys"
	"github.com/misaon/nylzo/internal/core/opaque"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/server/logging"
	"github.com/misaon/nylzo/internal/server/proof"
	"github.com/misaon/nylzo/internal/store"
)

// handleHandshake answers KE1 with KE2 and counts a passphrase attempt until a matching KE3 takes it back (spec
// section 6.4). Every message runs it, also one without a passphrase.
func (s *Server) handleHandshake(w http.ResponseWriter, r *http.Request) {
	if !validPathID(w, r) || !s.allowRead(w, r) {
		return
	}
	ke1, ok := parseKE1(w, r)
	if !ok {
		return
	}
	granted, ok := s.authorizedLive(w, r, seal.Read)
	defer granted.forget()
	if !ok {
		return
	}
	record := &granted.record
	now := time.Now()
	if !s.resumedLease(w, r, record, now) {
		return
	}
	expiryShare, ok := s.shareOf(w, r, record)
	defer clear(expiryShare[:])
	if !ok {
		return
	}
	if !s.mayCount(w, r, record) {
		return
	}
	registration, ok := s.openRegistration(w, r, record)
	if !ok {
		return
	}
	defer clear(registration)
	challenge, expires, issued := s.challenges.IssueLogin(now)
	if !issued {
		setRetryAfter(w, challengesFullRetry)
		writeError(w, http.StatusServiceUnavailable, errUnavailable)
		return
	}
	attempt, status, err := store.CountAttempt(r.Context(), s.store, record, time.Now)
	if err != nil {
		s.challenges.Forget(challenge)
		s.writeClaimFailure(w, r, err)
		return
	}
	ke2, login, err := s.generateKE2(granted.id, &expiryShare, record.Message, registration, ke1)
	if err != nil {
		s.challenges.Forget(challenge)
		s.takeBackUnanswered(r, record, attempt)
		s.writeInternal(w, r, err)
		return
	}
	if !s.challenges.Attach(
		challenge,
		&proof.Login{ID: granted.id, Locator: record.Row.Locator, Server: login, Attempt: attempt},
	) {
		login.Wipe()
		s.takeBackUnanswered(r, record, attempt)
		setRetryAfter(w, challengesFullRetry)
		writeError(w, http.StatusServiceUnavailable, errUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, api.Handshake{
		KE2:          core.Encode(ke2[:]),
		Challenge:    core.Encode(challenge[:]),
		ExpiresAt:    formatTime(expires),
		AttemptsLeft: status.AttemptsLeft,
		RetryAfter:   ceilSeconds(status.RetryAfter),
	})
}

// mayCount answers, before the blob is read, a handshake that could not count: one for a message whose registration
// pepper is not configured, which counts nothing until it is, and one that CountAttempt would refuse.
func (s *Server) mayCount(w http.ResponseWriter, r *http.Request, record *store.Record) bool {
	if !s.ring.Holds(record.Message.RegistrationPepperID) {
		s.events.ErrorContext(r.Context(), "registration-pepper-missing")
		setRetryAfter(w, challengesFullRetry)
		writeError(w, http.StatusServiceUnavailable, errUnavailable)
		return false
	}
	if err := store.RefuseAttempt(record, time.Now()); err != nil {
		s.writeClaimFailure(w, r, err)
		return false
	}
	return true
}

// resumedLease checks the lease that a reader sends when it resumes a claimed open, so a lease that is no longer active
// answers as a download with it would before the handshake counts an attempt.
func (s *Server) resumedLease(w http.ResponseWriter, r *http.Request, record *store.Record, now time.Time) bool {
	if r.Header.Get(api.LeaseHeaderName) == "" {
		return true
	}
	if _, active := s.findLease(r, record, now); !active {
		s.writeCurrentGone(w, r, record)
		return false
	}
	return true
}

// openRegistration reads the erase salt from the blob, answering a blob that is gone as a claim does, and opens the
// registration that the salt seals.
func (s *Server) openRegistration(w http.ResponseWriter, r *http.Request, record *store.Record) ([]byte, bool) {
	var salt seal.EraseSalt
	defer clear(salt[:])
	if !s.readEraseSalt(w, r, record, &salt) {
		return nil, false
	}
	registration, err := seal.UnwrapRegistration(&salt, record.Row.Locator, record.Message.Registration)
	if err != nil {
		s.writeInternal(w, r, err)
		return nil, false
	}
	return registration, true
}

// generateKE2 uses the OPAQUE keys of the pepper that made the registration, so a rotation keeps live messages open,
// and the credential identifier with the expiry share.
func (s *Server) generateKE2(
	id core.ID,
	expiryShare *[core.ShareLen]byte,
	message *store.Message,
	registration, ke1 []byte,
) ([opaque.KE2Len]byte, opaque.ServerLogin, error) {
	serverKeys, _, err := s.ring.OPAQUEKeys(id[:], message.RegistrationPepperID)
	defer serverKeys.Wipe()
	if err != nil {
		return [opaque.KE2Len]byte{}, opaque.ServerLogin{}, err
	}
	config, err := keys.OPAQUEConfig(id, core.KDF(message.KDF))
	if err != nil {
		return [opaque.KE2Len]byte{}, opaque.ServerLogin{}, err
	}
	credential := keys.CredentialIdentifier(id, expiryShare)
	defer clear(credential)
	return opaque.GenerateKE2(&serverKeys, registration, credential, ke1, &config)
}

// finishHandshake checks KE3 against the login state of the request's challenge, which a handshake of the same
// message must have issued, and takes the counted attempt back in a transaction of its own.
func (s *Server) finishHandshake(w http.ResponseWriter, r *http.Request, record *store.Record, ke3 []byte) bool {
	login := takeLogin(r)
	if login == nil || login.Locator != record.Row.Locator {
		writeNotFound(w)
		return false
	}
	now := time.Now()
	if login.Server.ServerFinish(ke3) != nil {
		status := store.PassphraseStatus(record.Message, now)
		writePassphraseRefusal(w, &status)
		return false
	}
	if err := store.TakeBack(r.Context(), s.store, record, login.Attempt, now); err != nil {
		s.writeClaimFailure(w, r, err)
		return false
	}
	return true
}

// takeBackUnanswered takes back the attempt of a handshake that sends no KE2, since no passphrase could be tested.
func (s *Server) takeBackUnanswered(r *http.Request, record *store.Record, attempt store.Attempt) {
	ctx := context.WithoutCancel(r.Context())
	if err := store.TakeBack(ctx, s.store, record, attempt, time.Now()); err != nil {
		s.events.ErrorContext(ctx, "take-back", "err", logging.ErrorText(err))
	}
}

// takeBackUnchecked runs when a request that presents a handshake's challenge ends without the check of its KE3, such
// as a claim refused for load before its handler runs: a matching KE3 still takes the attempt back (spec section 6.4).
func (s *Server) takeBackUnchecked(r *http.Request) {
	login := takeLogin(r)
	if login == nil {
		return
	}
	ke3, err := parseOPAQUEHeader(r, api.KE3HeaderName, opaque.KE3Len)
	if err != nil || login.Server.ServerFinish(ke3) != nil {
		return
	}
	ctx := context.WithoutCancel(r.Context())
	granted, ok, err := s.authorize(ctx, r, login.ID.String(), seal.Read, nil)
	defer granted.forget()
	if err == nil && ok && granted.record.Row.Locator == login.Locator {
		err = store.TakeBack(ctx, s.store, &granted.record, login.Attempt, time.Now())
	}
	if err != nil {
		s.events.ErrorContext(ctx, "take-back", "err", logging.ErrorText(err))
	}
}

var errOPAQUEHeader = errors.New("server: malformed OPAQUE header")

func parseKE1(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	ke1, err := parseOPAQUEHeader(r, api.KE1HeaderName, opaque.KE1Len)
	if err == nil {
		_, blindedErr := opaque.DeserializeElement(ke1[:opaque.ElementLen])
		_, keyshareErr := opaque.DeserializeElement(ke1[opaque.KE1Len-opaque.ElementLen:])
		err = errors.Join(blindedErr, keyshareErr)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return nil, false
	}
	return ke1, true
}

// parseKE3 answers a client of protocol v2, which sends X-Nylzo-Verifier instead, with client_outdated.
func parseKE3(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	if len(r.Header.Values(api.VerifierHeaderName)) > 0 {
		writeError(w, http.StatusBadRequest, errClientOutdated)
		return nil, false
	}
	ke3, err := parseOPAQUEHeader(r, api.KE3HeaderName, opaque.KE3Len)
	if err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return nil, false
	}
	return ke3, true
}

// parseOptionalKE3 reads the KE3 that a download may carry, nil without one; a malformed or repeated one answers 400.
func parseOptionalKE3(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	if len(r.Header.Values(api.KE3HeaderName)) == 0 {
		return nil, true
	}
	ke3, err := parseOPAQUEHeader(r, api.KE3HeaderName, opaque.KE3Len)
	if err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return nil, false
	}
	return ke3, true
}

func parseOPAQUEHeader(r *http.Request, name string, length int) ([]byte, error) {
	values := r.Header.Values(name)
	if len(values) != 1 {
		return nil, errOPAQUEHeader
	}
	decoded, err := core.DecodeExact(values[0], length)
	if err != nil {
		return nil, errOPAQUEHeader
	}
	return decoded, nil
}

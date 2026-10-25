package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/server/logging"
	"github.com/misaon/nylzo/internal/store"
)

const (
	leaseHeaderName    = "X-Nylzo-Lease"
	policyHeaderName   = "X-Nylzo-Policy"
	verifierHeaderName = "X-Nylzo-Verifier"
	octetStream        = "application/octet-stream"
)

var errBlobSize = errors.New("server: blob size differs from the stored message size")

type metadataResponse struct {
	State              string `json:"state"`
	CreatedAt          string `json:"created_at,omitempty"`
	ExpiresAt          string `json:"expires_at,omitempty"`
	OpensMax           int    `json:"opens_max"`
	OpensRemaining     int    `json:"opens_remaining"`
	TTLSeconds         int    `json:"ttl_s"`
	PassphraseRequired bool   `json:"passphrase_required"`
	KDFID              int    `json:"kdf_id"`
	Size               int64  `json:"size"`
	FirstOpenedAt      string `json:"first_opened_at,omitempty"`
	EndedAt            string `json:"ended_at,omitempty"`
}

type policyHeader struct {
	OpensMax   int `json:"opens_max"`
	TTLSeconds int `json:"ttl_s"`
	KDFID      int `json:"kdf_id"`
	Flags      int `json:"flags"`
}

func (s *Server) handleMetadata(w http.ResponseWriter, r *http.Request) {
	if !validPathID(w, r) || !s.allowRead(w, r) {
		return
	}
	granted, ok := s.authorized(w, r, pepper.Read)
	if !ok {
		return
	}
	if tombstone := granted.record.Tombstone; tombstone != nil {
		writeJSON(w, http.StatusOK, metadataResponse{
			State:         string(tombstone.State),
			FirstOpenedAt: formatOptionalTime(tombstone.FirstOpenedAt),
			EndedAt:       formatTime(tombstone.EndedAt),
		})
		return
	}
	message := granted.record.Message
	writeJSON(w, http.StatusOK, metadataResponse{
		State:              string(liveState(message, s.clock())),
		CreatedAt:          formatTime(message.CreatedAt),
		ExpiresAt:          formatTime(message.ExpiresAt),
		OpensMax:           int(message.OpensMax),
		OpensRemaining:     int(message.OpensRemaining),
		TTLSeconds:         int(message.TTLSeconds),
		PassphraseRequired: message.PassphraseRequired(),
		KDFID:              int(message.KDF),
		Size:               message.Size,
		FirstOpenedAt:      formatOptionalTime(message.FirstOpenedAt),
	})
}

// startStream admits a claim or a download; the stream slot is taken before the signature is checked (D24).
func (s *Server) startStream(w http.ResponseWriter, r *http.Request) (func(), bool) {
	if !validPathID(w, r) || !s.allowRead(w, r) {
		return nil, false
	}
	return s.takeStream(w, r)
}

func (s *Server) handleClaim(w http.ResponseWriter, r *http.Request) {
	release, ok := s.startStream(w, r)
	if !ok {
		return
	}
	defer release()
	verifier, verifierSent, ok := parseVerifier(w, r)
	if !ok {
		return
	}
	granted, ok := s.authorizedLive(w, r, pepper.Read)
	if !ok {
		return
	}
	verifier.Challenge = granted.signature.Challenge
	verifierKeyOK := core.ValidPublicKey(verifier.PublicKey[:])
	if !verifierKeyOK {
		verifier.PublicKey = dummyPublicKey
	}
	verifierSigned := verifier.Verify(core.PurposeVerifier, r.Method, r.URL.EscapedPath(), nil) && verifierKeyOK
	blob, size, ok := s.openBlob(w, r, granted.record.Message)
	if !ok {
		return
	}
	defer blob.Close()
	lease := core.NewSecret()
	leaseTag := s.ring.MAC(pepper.Lease, lease[:])
	result, err := s.store.Claim(r.Context(), store.ClaimParameters{
		ID:           granted.id.String(),
		Clock:        s.clock,
		VerifierSent: verifierSent,
		VerifyMAC: func(pepperID byte, stored []byte) bool {
			macOK := s.ring.Verify(pepper.Verifier, verifier.PublicKey[:], stored, pepperID)
			return macOK && verifierSigned
		},
		LeaseMAC:      leaseTag.MAC[:],
		LeasePepperID: leaseTag.PepperID,
	})
	if err != nil {
		s.writeClaimFailure(w, r, err)
		return
	}
	if result.Message.OpensRemaining == 0 {
		s.releaseAtLeaseEnd(granted.id.String(), result.Lease.ExpiresAt)
	}
	s.serveBlob(w, r, result.Message, core.Encode(lease[:]), blob, size)
}

func parseVerifier(w http.ResponseWriter, r *http.Request) (core.Signature, bool, bool) {
	header := r.Header.Get(verifierHeaderName)
	if header == "" {
		return core.Signature{PublicKey: dummyPublicKey}, false, true
	}
	verifier, err := core.ParseVerifierHeader(header, [core.ChallengeLen]byte{})
	if err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return verifier, true, false
	}
	return verifier, true, true
}

func (s *Server) writeClaimFailure(w http.ResponseWriter, r *http.Request, err error) {
	if goneErr, isGone := errors.AsType[*store.GoneError](err); isGone {
		writeGone(w, string(goneErr.State))
		return
	}
	if passphraseErr, isPassphrase := errors.AsType[*store.PassphraseError](err); isPassphrase {
		writeJSON(w, http.StatusForbidden, passphraseRejected{
			Error:        "passphrase",
			RetryAfter:   ceilSeconds(passphraseErr.RetryAfter),
			AttemptsLeft: passphraseErr.AttemptsLeft,
		})
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		writeNotFound(w)
		return
	}
	s.writeInternal(w, r, err)
}

func (s *Server) releaseAtLeaseEnd(id string, leaseExpires time.Time) {
	s.after(leaseExpires.Add(store.LeaseTimerSlack).Sub(s.clock()), func() {
		if _, err := store.Release(context.Background(), s.store, s.blobs, id, s.clock()); err != nil {
			s.logger.Error("lease-timer", "err", logging.ErrorText(err))
		}
	})
}

func (s *Server) handleBlob(w http.ResponseWriter, r *http.Request) {
	release, ok := s.startStream(w, r)
	if !ok {
		return
	}
	defer release()
	leased, ok := s.activeLease(w, r)
	if !ok {
		return
	}
	if err := s.store.RecordDownload(r.Context(), &leased.lease, leased.now); err != nil {
		s.writeLeaseFailure(w, r, &leased, err)
		return
	}
	blob, size, ok := s.openBlob(w, r, leased.message)
	if !ok {
		return
	}
	defer blob.Close()
	s.serveBlob(w, r, leased.message, "", blob, size)
}

func (s *Server) handleAck(w http.ResponseWriter, r *http.Request) {
	leased, ok := s.activeLease(w, r)
	if !ok {
		return
	}
	if err := store.AckLease(r.Context(), s.store, s.blobs, &leased.lease, leased.now); err != nil {
		s.writeLeaseFailure(w, r, &leased, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) writeLeaseFailure(w http.ResponseWriter, r *http.Request, leased *leaseAccess, err error) {
	if goneErr, isGone := errors.AsType[*store.GoneError](err); isGone {
		s.finishGone(w, r, leased.id, goneErr.State, leased.now)
		return
	}
	switch {
	case errors.Is(err, store.ErrLeaseLimit):
		writeRateLimited(w, leased.lease.ExpiresAt.Sub(leased.now))
	case errors.Is(err, store.ErrLeaseInvalid):
		s.writeCurrentGone(w, r, leased.id)
	default:
		s.writeInternal(w, r, err)
	}
}

type leaseAccess struct {
	message *store.Message
	id      string
	lease   store.Lease
	now     time.Time
}

func (s *Server) activeLease(w http.ResponseWriter, r *http.Request) (leaseAccess, bool) {
	granted, ok := s.authorizedLive(w, r, pepper.Read)
	if !ok {
		return leaseAccess{}, false
	}
	leased := leaseAccess{message: granted.record.Message, id: granted.id.String(), now: s.clock()}
	lease, active, err := s.findLease(r.Context(), r, &granted, leased.now)
	switch {
	case err != nil:
		s.writeInternal(w, r, err)
		return leased, false
	case !active:
		s.writeCurrentGone(w, r, leased.id)
		return leased, false
	}
	leased.lease = lease
	return leased, true
}

func (s *Server) findLease(
	ctx context.Context,
	r *http.Request,
	granted *access,
	now time.Time,
) (store.Lease, bool, error) {
	raw, ok := leaseToken(r)
	if !ok {
		return store.Lease{}, false, nil
	}
	leases := granted.leases
	if leases == nil {
		var err error
		if leases, err = s.store.Leases(ctx, granted.id.String()); err != nil {
			return store.Lease{}, false, err
		}
	}
	for _, lease := range leases {
		if s.ring.Verify(pepper.Lease, raw, lease.MAC, lease.PepperID) {
			active := lease.Active(now)
			return lease, active, nil
		}
	}
	return store.Lease{}, false, nil
}

func leaseToken(r *http.Request) ([]byte, bool) {
	raw, err := core.DecodeExact(r.Header.Get(leaseHeaderName), core.SeedLen)
	return raw, err == nil
}

func (s *Server) openBlob(w http.ResponseWriter, r *http.Request, message *store.Message) (io.ReadCloser, int64, bool) {
	if message.BlobRef == "" {
		writeGone(w, string(message.State))
		return nil, 0, false
	}
	blob, size, err := s.blobs.Open(r.Context(), message.BlobRef)
	if errors.Is(err, store.ErrBlobMissing) {
		var state store.State
		if state, err = s.finishMissingBlob(r.Context(), message.ID); err == nil {
			writeGone(w, string(state))
			return nil, 0, false
		}
	}
	if err == nil && size != message.Size {
		blob.Close()
		err = errBlobSize
	}
	if err != nil {
		s.writeInternal(w, r, err)
		return nil, 0, false
	}
	return blob, size, true
}

// serveBlob gives the response as long as a lease, so a recipient on a 128 kbit/s link receives the largest object.
func (s *Server) serveBlob(
	w http.ResponseWriter,
	r *http.Request,
	message *store.Message,
	lease string,
	blob io.Reader,
	size int64,
) {
	s.setWriteDeadline(w, time.Now().Add(max(writeTimeout, store.TransferTime(size))))
	header := w.Header()
	if lease != "" {
		header.Set(leaseHeaderName, lease)
	}
	header.Set(policyHeaderName, encodePolicy(message))
	header.Set("Content-Type", octetStream)
	header.Set("Content-Length", strconv.FormatInt(size, 10))
	w.WriteHeader(http.StatusOK)
	if copied, err := io.Copy(w, blob); (err != nil || copied != size) && r.Context().Err() == nil {
		s.logger.WarnContext(r.Context(), "blob-stream")
	}
}

func encodePolicy(message *store.Message) string {
	data, _ := json.Marshal(policyHeader{
		OpensMax:   int(message.OpensMax),
		TTLSeconds: int(message.TTLSeconds),
		KDFID:      int(message.KDF),
		Flags:      int(message.Flags),
	})
	return core.Encode(data)
}

// finishMissingBlob ends a message whose blob a concurrent ending already deleted; on a live message it is a fault.
func (s *Server) finishMissingBlob(ctx context.Context, id string) (store.State, error) {
	record, err := s.store.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if record.Tombstone != nil {
		return record.Tombstone.State, nil
	}
	now := s.clock()
	if !ended(record.Message, now) {
		return "", store.ErrBlobMissing
	}
	state, err := store.EndMessage(ctx, s.store, s.blobs, id, store.StateExpired, now)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return s.endedState(ctx, id)
	case err != nil:
		s.logger.ErrorContext(ctx, "finish-ending", "err", logging.ErrorText(err))
		return liveState(record.Message, now), nil
	default:
		return state, nil
	}
}

func (s *Server) endedState(ctx context.Context, id string) (store.State, error) {
	record, err := s.store.Get(ctx, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return store.StateDeleted, nil
	case err != nil:
		return "", err
	case record.Tombstone != nil:
		return record.Tombstone.State, nil
	default:
		return liveState(record.Message, s.clock()), nil
	}
}

func (s *Server) writeCurrentGone(w http.ResponseWriter, r *http.Request, id string) {
	state, err := s.endedState(r.Context(), id)
	if err != nil {
		s.writeInternal(w, r, err)
		return
	}
	writeGone(w, string(state))
}

func (s *Server) finishGone(w http.ResponseWriter, r *http.Request, id string, state store.State, now time.Time) {
	_, err := store.EndMessage(r.Context(), s.store, s.blobs, id, state, now)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.logger.ErrorContext(r.Context(), "finish-ending", "err", logging.ErrorText(err))
	}
	writeGone(w, string(state))
}

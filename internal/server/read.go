package server

import (
	"context"
	"crypto/cipher"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/seal"
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
	granted, ok := s.authorized(w, r, seal.Read)
	if !ok {
		return
	}
	now := s.clock()
	record := &granted.record
	message := record.Message
	if !record.HasBlob() {
		writeJSON(w, http.StatusOK, metadataResponse{
			State:         string(record.State(now)),
			FirstOpenedAt: formatOptionalTime(message.FirstOpenedAt),
			EndedAt:       formatEnd(record.EndedAt(now)),
		})
		return
	}
	writeJSON(w, http.StatusOK, metadataResponse{
		State:              string(record.State(now)),
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

func formatEnd(end time.Time) string {
	if end.IsZero() {
		return ""
	}
	return formatTime(end)
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
	granted, ok := s.authorizedLive(w, r, seal.Read)
	if !ok {
		return
	}
	verifier.Challenge = granted.signature.Challenge
	verifierKeyOK := core.ValidPublicKey(verifier.PublicKey[:])
	if !verifierKeyOK {
		verifier.PublicKey = dummyPublicKey
	}
	verifierSigned := verifier.Verify(core.PurposeVerifier, r.Method, r.URL.EscapedPath(), nil) && verifierKeyOK
	blob, ok := s.openBlob(w, r, &granted.record)
	if !ok {
		return
	}
	defer blob.Close()
	lease := core.NewSecret()
	leaseTag := s.ring.MAC(pepper.Lease, lease[:])
	result, err := store.Claim(r.Context(), s.store, &granted.record, store.ClaimParameters{
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
		s.releaseAtLeaseEnd(&granted.record, result.Lease.ExpiresAt)
	}
	s.serveBlob(w, r, result.Message, core.Encode(lease[:]), blob)
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

// releaseAtLeaseEnd keeps the record key in memory until the last lease ends, so the blob goes at once.
func (s *Server) releaseAtLeaseEnd(record *store.Record, leaseExpires time.Time) {
	locator, key := record.Row.Locator, record.Key
	s.after(leaseExpires.Add(store.LeaseTimerSlack).Sub(s.clock()), func() {
		ctx := context.Background()
		kept := store.Record{Row: store.Row{Locator: locator}, Key: key}
		err := s.reload(ctx, &kept)
		if err == nil {
			_, err = store.Finish(ctx, s.store, s.blobs, &kept, s.clock())
		}
		if err != nil && !errors.Is(err, store.ErrNotFound) {
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
	err := store.RecordDownload(r.Context(), s.store, &leased.record, leased.lease.MAC, leased.now)
	if err != nil {
		s.writeLeaseFailure(w, r, &leased, err)
		return
	}
	blob, ok := s.openBlob(w, r, &leased.record)
	if !ok {
		return
	}
	defer blob.Close()
	s.serveBlob(w, r, leased.record.Message, "", blob)
}

func (s *Server) handleAck(w http.ResponseWriter, r *http.Request) {
	leased, ok := s.activeLease(w, r)
	if !ok {
		return
	}
	err := store.Ack(r.Context(), s.store, s.blobs, &leased.record, leased.lease.MAC, leased.now)
	if err != nil {
		s.writeLeaseFailure(w, r, &leased, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) writeLeaseFailure(w http.ResponseWriter, r *http.Request, leased *leaseAccess, err error) {
	if goneErr, isGone := errors.AsType[*store.GoneError](err); isGone {
		s.finishGone(w, r, &leased.record, goneErr.State)
		return
	}
	switch {
	case errors.Is(err, store.ErrLeaseLimit):
		writeRateLimited(w, leased.lease.ExpiresAt.Sub(leased.now))
	case errors.Is(err, store.ErrLeaseInvalid):
		s.writeCurrentGone(w, r, &leased.record)
	default:
		s.writeInternal(w, r, err)
	}
}

type leaseAccess struct {
	record store.Record
	lease  store.Lease
	now    time.Time
}

func (s *Server) activeLease(w http.ResponseWriter, r *http.Request) (leaseAccess, bool) {
	granted, ok := s.authorizedLive(w, r, seal.Read)
	if !ok {
		return leaseAccess{}, false
	}
	leased := leaseAccess{record: granted.record, now: s.clock()}
	lease, active := s.findLease(r, &leased.record, leased.now)
	if !active {
		s.writeCurrentGone(w, r, &leased.record)
		return leased, false
	}
	leased.lease = lease
	return leased, true
}

func (s *Server) findLease(r *http.Request, record *store.Record, now time.Time) (store.Lease, bool) {
	raw, ok := leaseToken(r)
	if !ok {
		return store.Lease{}, false
	}
	for _, lease := range record.Message.Leases {
		if s.ring.Verify(pepper.Lease, raw, lease.MAC, lease.PepperID) {
			active := lease.Active(now)
			return lease, active
		}
	}
	return store.Lease{}, false
}

func leaseToken(r *http.Request) ([]byte, bool) {
	raw, err := core.DecodeExact(r.Header.Get(leaseHeaderName), core.SeedLen)
	return raw, err == nil
}

type decryptedBlob struct {
	io.Reader
	io.Closer
}

// openBlob decrypts the stored blob and drops its padding, so the reader gets exactly the object.
func (s *Server) openBlob(w http.ResponseWriter, r *http.Request, record *store.Record) (io.ReadCloser, bool) {
	key, err := record.BlobKey()
	if errors.Is(err, store.ErrBlobMissing) {
		writeGone(w, string(record.State(s.clock())))
		return nil, false
	}
	var file io.ReadCloser
	var size int64
	if err == nil {
		file, size, err = s.blobs.Open(r.Context(), record.Row.BlobRef)
	}
	if errors.Is(err, store.ErrBlobMissing) {
		var state store.State
		if state, err = s.finishMissingBlob(r.Context(), record); err == nil {
			writeGone(w, string(state))
			return nil, false
		}
	}
	if err == nil && size != seal.PaddedSize(record.Message.Size) {
		file.Close()
		err = errBlobSize
	}
	if err != nil {
		s.writeInternal(w, r, err)
		return nil, false
	}
	plain := cipher.StreamReader{S: seal.BlobStream(key), R: file}
	return decryptedBlob{Reader: io.LimitReader(plain, record.Message.Size), Closer: file}, true
}

// serveBlob gives the response as long as a lease, so a recipient on a 128 kbit/s link receives the largest object.
func (s *Server) serveBlob(
	w http.ResponseWriter,
	r *http.Request,
	message *store.Message,
	lease string,
	blob io.Reader,
) {
	size := message.Size
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
func (s *Server) finishMissingBlob(ctx context.Context, record *store.Record) (store.State, error) {
	if err := s.reload(ctx, record); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.StateDeleted, nil
		}
		return "", err
	}
	now := s.clock()
	if !record.Ended(now) {
		return "", store.ErrBlobMissing
	}
	if _, err := store.Finish(ctx, s.store, s.blobs, record, now); err != nil {
		s.logger.ErrorContext(ctx, "finish-ending", "err", logging.ErrorText(err))
	}
	return record.State(now), nil
}

func (s *Server) writeCurrentGone(w http.ResponseWriter, r *http.Request, record *store.Record) {
	err := s.reload(r.Context(), record)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeGone(w, string(store.StateDeleted))
	case err != nil:
		s.writeInternal(w, r, err)
	default:
		writeGone(w, string(record.State(s.clock())))
	}
}

func (s *Server) finishGone(w http.ResponseWriter, r *http.Request, record *store.Record, state store.State) {
	if err := s.reload(r.Context(), record); err == nil {
		_, err = store.Finish(r.Context(), s.store, s.blobs, record, s.clock())
		if err != nil {
			s.logger.ErrorContext(r.Context(), "finish-ending", "err", logging.ErrorText(err))
		}
	}
	writeGone(w, string(state))
}

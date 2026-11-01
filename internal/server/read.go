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
	"github.com/misaon/nylzo/internal/core/share"
	"github.com/misaon/nylzo/internal/core/signing"
	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/secretmode"
	"github.com/misaon/nylzo/internal/server/epoch"
	"github.com/misaon/nylzo/internal/server/logging"
	"github.com/misaon/nylzo/internal/store"
)

const (
	leaseHeaderName    = "X-Nylzo-Lease"
	policyHeaderName   = "X-Nylzo-Policy"
	verifierHeaderName = "X-Nylzo-Verifier"
	shareKeyHeaderName = "X-Nylzo-Share-Key"
	ttlHeaderName      = "X-Nylzo-Ttl"
	shareHeaderName    = "X-Nylzo-Share"
	octetStream        = "application/octet-stream"
)

var errBlobSize = errors.New("server: blob size differs from the stored message size")

type metadataResponse struct {
	State          string `json:"state"`
	CreatedAt      string `json:"created_at,omitempty"`
	ExpiresAt      string `json:"expires_at,omitempty"`
	OpensMax       int    `json:"opens_max"`
	OpensRemaining int    `json:"opens_remaining"`
	TTLSeconds     int    `json:"ttl_s"`
	KDFID          int    `json:"kdf_id"`
	Size           int64  `json:"size"`
	FirstOpenedAt  string `json:"first_opened_at,omitempty"`
	EndedAt        string `json:"ended_at,omitempty"`
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
	defer granted.forget()
	if !ok {
		return
	}
	now := time.Now()
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
		State:          string(record.State(now)),
		CreatedAt:      formatTime(message.CreatedAt),
		ExpiresAt:      formatTime(message.ExpiresAt),
		OpensMax:       int(message.OpensMax),
		OpensRemaining: int(message.OpensRemaining),
		TTLSeconds:     int(message.TTLSeconds),
		KDFID:          int(message.KDF),
		Size:           message.Size,
		FirstOpenedAt:  formatOptionalTime(message.FirstOpenedAt),
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
	defer granted.forget()
	if !ok {
		return
	}
	verifier.Challenge = granted.signature.Challenge
	verifierKeyOK := signing.ValidPublicKey(verifier.PublicKey[:])
	if !verifierKeyOK {
		verifier.PublicKey = dummyPublicKey
	}
	verifierSigned := verifier.Verify(signing.PurposeVerifier, r.Method, r.URL.EscapedPath(), nil) && verifierKeyOK
	wrappedShare, ok := s.wrappedShare(w, r, &granted.record)
	if !ok {
		return
	}
	blob, ok := s.openBlob(w, r, &granted.record)
	if !ok {
		return
	}
	defer blob.content.Close()
	defer clear(blob.salt[:])
	lease := core.NewSecret()
	leaseTag := s.ring.MAC(pepper.Lease, lease[:])
	result, err := store.Claim(r.Context(), s.store, &granted.record, store.ClaimParameters{
		Clock:        time.Now,
		VerifierSent: verifierSent,
		VerifyMAC: func(pepperID byte, stored []byte) bool {
			input := verifierInput(blob.salt, verifier.PublicKey[:])
			macOK := s.ring.Verify(pepper.Verifier, input, stored, pepperID)
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
	s.serveBlob(w, r, result.Message, servedHeaders{lease: core.Encode(lease[:]), share: wrappedShare}, blob.content)
}

// wrappedShare recomputes E for a message and wraps it to the client's one-time key (spec section 5.6).
func (s *Server) wrappedShare(w http.ResponseWriter, r *http.Request, record *store.Record) (string, bool) {
	message := record.Message
	clientKey, err := share.ParseKey(r.Header.Get(shareKeyHeaderName))
	if err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return "", false
	}
	now := time.Now()
	expiryShare, err := s.epochs.Share(epoch.RatchetID(message.RatchetID), message.Epoch, record.Row.Locator, now)
	if errors.Is(err, epoch.ErrShareGone) {
		s.endLostShare(w, r, record, now)
		return "", false
	}
	if err != nil {
		s.writeInternal(w, r, err)
		return "", false
	}
	defer clear(expiryShare[:])
	wrapped, err := share.Wrap(&expiryShare, clientKey)
	if err != nil {
		s.writeInternal(w, r, err)
		return "", false
	}
	return core.Encode(wrapped[:]), true
}

// endLostShare ends a message nobody can open any more, because the seed that made its share is gone.
func (s *Server) endLostShare(w http.ResponseWriter, r *http.Request, record *store.Record, now time.Time) {
	final, err := store.End(r.Context(), s.store, s.blobs, record, store.StateDeleted, now)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "share-lost", "err", logging.ErrorText(err))
		final = store.StateDeleted
	}
	writeGone(w, string(final))
}

func parseVerifier(w http.ResponseWriter, r *http.Request) (signing.Signature, bool, bool) {
	header := r.Header.Get(verifierHeaderName)
	if header == "" {
		return signing.Signature{PublicKey: dummyPublicKey}, false, true
	}
	verifier, err := signing.ParseVerifierHeader(header, [signing.ChallengeLen]byte{})
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
	if busyErr, isBusy := errors.AsType[*store.BusyError](err); isBusy {
		setRetryAfter(w, busyErr.RetryAfter)
		writeError(w, http.StatusServiceUnavailable, errUnavailable)
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
	locator := record.Row.Locator
	delay := time.Until(leaseExpires.Add(store.LeaseTimerSlack))
	s.leaseTimers.schedule(locator, record.Key, delay, func(key seal.Key) {
		ctx := context.Background()
		kept := store.Record{Row: store.Row{Locator: locator}, Key: key}
		defer clear(kept.Key[:])
		err := s.reload(ctx, &kept)
		if err == nil {
			_, err = store.Finish(ctx, s.store, s.blobs, &kept, time.Now())
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
	defer clear(leased.record.Key[:])
	if !ok {
		return
	}
	wrapped, ok := s.wrappedShare(w, r, &leased.record)
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
	defer blob.content.Close()
	clear(blob.salt[:])
	s.serveBlob(w, r, leased.record.Message, servedHeaders{share: wrapped}, blob.content)
}

func (s *Server) handleAck(w http.ResponseWriter, r *http.Request) {
	if !validPathID(w, r) || !s.allowRead(w, r) {
		return
	}
	leased, ok := s.activeLease(w, r)
	defer clear(leased.record.Key[:])
	if !ok {
		return
	}
	err := store.Ack(r.Context(), s.store, s.blobs, &leased.record, leased.lease.MAC, leased.now)
	if err != nil {
		s.writeLeaseFailure(w, r, &leased, err)
		return
	}
	if !leased.record.HasBlob() {
		s.leaseTimers.cancel(leased.record.Row.Locator)
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
	defer granted.forget()
	if !ok {
		return leaseAccess{}, false
	}
	leased := leaseAccess{record: granted.record, now: time.Now()}
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

type openedBlob struct {
	content io.ReadCloser
	salt    seal.EraseSalt
}

// openBlob decrypts the stored blob and drops its padding, so the reader gets exactly the object.
func (s *Server) openBlob(w http.ResponseWriter, r *http.Request, record *store.Record) (openedBlob, bool) {
	var key seal.Key
	var err error
	secretmode.Do(func() { key, err = record.BlobKey() })
	defer clear(key[:])
	if errors.Is(err, store.ErrBlobMissing) {
		writeGone(w, string(record.State(time.Now())))
		return openedBlob{}, false
	}
	var file io.ReadCloser
	var salt seal.EraseSalt
	if err == nil {
		file, salt, err = s.openStoredBlob(r.Context(), record)
	}
	if errors.Is(err, store.ErrBlobMissing) {
		var state store.State
		if state, err = s.finishMissingBlob(r.Context(), record); err == nil {
			writeGone(w, string(state))
			return openedBlob{}, false
		}
	}
	if err != nil {
		s.writeInternal(w, r, err)
		return openedBlob{}, false
	}
	var stream cipher.Stream
	secretmode.Do(func() { stream = seal.BlobStream(key, salt) })
	plain := cipher.StreamReader{S: stream, R: file}
	content := decryptedBlob{Reader: io.LimitReader(plain, record.Message.Size), Closer: file}
	return openedBlob{content: content, salt: salt}, true
}

// openStoredBlob reads the erase block; a block that a concurrent deletion has overwritten counts as a missing blob.
func (s *Server) openStoredBlob(ctx context.Context, record *store.Record) (io.ReadCloser, seal.EraseSalt, error) {
	file, size, err := s.blobs.Open(ctx, record.Row.BlobRef)
	if err != nil {
		return nil, seal.EraseSalt{}, err
	}
	if size != seal.StoredBlobSize(record.Message.Size) {
		file.Close()
		return nil, seal.EraseSalt{}, errBlobSize
	}
	salt, err := seal.ReadEraseBlock(file)
	switch {
	case errors.Is(err, seal.ErrErased):
		file.Close()
		return nil, salt, store.ErrBlobMissing
	case err != nil:
		file.Close()
		return nil, salt, err
	default:
		return file, salt, nil
	}
}

type servedHeaders struct {
	lease string
	share string
}

// serveBlob gives the response as long as a lease, so a recipient on a 128 kbit/s link receives the largest object.
func (s *Server) serveBlob(
	w http.ResponseWriter,
	r *http.Request,
	message *store.Message,
	served servedHeaders,
	blob io.Reader,
) {
	size := message.Size
	overall := time.Now().Add(max(writeTimeout, store.TransferTime(size)))
	s.setWriteDeadline(w, overall)
	header := w.Header()
	if served.lease != "" {
		header.Set(leaseHeaderName, served.lease)
	}
	if served.share != "" {
		header.Set(shareHeaderName, served.share)
	}
	header.Set(policyHeaderName, encodePolicy(message))
	header.Set("Content-Type", octetStream)
	header.Set("Content-Length", strconv.FormatInt(size, 10))
	w.WriteHeader(http.StatusOK)
	paced := &pacedWriter{writer: w, controller: http.NewResponseController(w), pace: s.pace, overall: overall}
	if copied, err := io.Copy(paced, blob); (err != nil || copied != size) && r.Context().Err() == nil {
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
	now := time.Now()
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
		writeGone(w, string(record.State(time.Now())))
	}
}

func (s *Server) finishGone(w http.ResponseWriter, r *http.Request, record *store.Record, state store.State) {
	if err := s.reload(r.Context(), record); err == nil {
		_, err = store.Finish(r.Context(), s.store, s.blobs, record, time.Now())
		if err != nil {
			s.logger.ErrorContext(r.Context(), "finish-ending", "err", logging.ErrorText(err))
		}
	}
	writeGone(w, string(state))
}

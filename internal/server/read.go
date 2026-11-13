package server

import (
	"context"
	"crypto/cipher"
	"encoding/json"
	"errors"
	"hash"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/core/share"
	"github.com/misaon/nylzo/internal/epoch"
	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/secretmode"
	"github.com/misaon/nylzo/internal/server/logging"
	"github.com/misaon/nylzo/internal/store"
)

const octetStream = "application/octet-stream"

var errBlobSize = errors.New("server: blob size differs from the stored message size")

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
		writeJSON(w, http.StatusOK, api.Metadata{
			State:         string(record.State(now)),
			FirstOpenedAt: formatOptionalTime(message.FirstOpenedAt),
			EndedAt:       formatEnd(record.EndedAt(now)),
		})
		return
	}
	writeJSON(w, http.StatusOK, api.Metadata{
		State:          string(record.State(now)),
		CreatedAt:      formatTime(message.CreatedAt),
		ExpiresAt:      formatTime(message.ExpiresAt),
		OpensMax:       int(message.OpensMax),
		OpensRemaining: int(message.OpensRemaining),
		TTLSeconds:     int64(message.TTLSeconds),
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

// admitStream checks what costs no lookup before a claim or a download takes a stream slot, which it does before the
// signature is checked (D24), so a malformed request never holds one.
func (s *Server) admitStream(w http.ResponseWriter, r *http.Request) bool {
	return validPathID(w, r) && s.allowRead(w, r) && emptyBody(w, r)
}

func (s *Server) handleClaim(w http.ResponseWriter, r *http.Request) {
	if !s.admitStream(w, r) {
		return
	}
	verifier, verifierSent, ok := parseVerifier(w, r)
	if !ok {
		return
	}
	release, ok := s.takeStream(w, r)
	if !ok {
		return
	}
	defer release()
	granted, ok := s.authorizedLive(w, r, seal.Read)
	defer granted.forget()
	if !ok || !s.allowReadBytes(w, r, granted.record.Message.Size) {
		return
	}
	wrappedShare, ok := s.wrappedShare(w, r, &granted.record)
	if !ok {
		return
	}
	if err := store.RefuseClaim(&granted.record, time.Now()); err != nil {
		s.writeClaimFailure(w, r, err)
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
		Clock:         time.Now,
		Verifier:      s.verifierProof(r, &verifier, verifierSent, granted.signature.Challenge, &blob.salt),
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
	clientKey, err := share.ParseKey(r.Header.Get(api.ShareKeyHeaderName))
	if errors.Is(err, share.ErrOutdated) {
		writeError(w, http.StatusBadRequest, errClientOutdated)
		return "", false
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return "", false
	}
	now := time.Now()
	var expiryShare [core.ShareLen]byte
	secretmode.Do(func() {
		expiryShare, err = s.epochs.Share(message.Generation, message.Epoch, record.Row.Locator, now)
	})
	defer clear(expiryShare[:])
	if errors.Is(err, epoch.ErrShareGone) {
		s.endLostShare(w, r, record, now)
		return "", false
	}
	if err != nil {
		s.writeInternal(w, r, err)
		return "", false
	}
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
		s.events.ErrorContext(r.Context(), "share-lost", "err", logging.ErrorText(err))
		final = store.StateDeleted
	}
	writeGone(w, string(final))
}

func (s *Server) writeClaimFailure(w http.ResponseWriter, r *http.Request, err error) {
	if goneErr, isGone := errors.AsType[*store.GoneError](err); isGone {
		writeGone(w, string(goneErr.State))
		return
	}
	if refusal, refused := errors.AsType[*store.PassphraseError](err); refused {
		writePassphraseRefusal(w, refusal)
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
// releaseAtLeaseEnd runs after the request has ended and needs none of its values, so it starts from Background.
func (s *Server) releaseAtLeaseEnd(record *store.Record, leaseExpires time.Time) {
	locator := record.Row.Locator
	delay := time.Until(leaseExpires.Add(store.LeaseTimerSlack))
	s.leaseTimers.Schedule(locator, record.Key, delay, func(key seal.Key) {
		ctx := context.Background()
		kept := store.Record{Row: store.Row{Locator: locator}, Key: key}
		defer clear(kept.Key[:])
		err := s.reload(ctx, &kept)
		if err == nil {
			_, err = store.Finish(ctx, s.store, s.blobs, &kept, time.Now())
		}
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			s.events.ErrorContext(ctx, "lease-timer", "err", logging.ErrorText(err))
		}
	})
}

func (s *Server) handleBlob(w http.ResponseWriter, r *http.Request) {
	if !s.admitStream(w, r) {
		return
	}
	release, ok := s.takeStream(w, r)
	if !ok {
		return
	}
	defer release()
	leased, ok := s.activeLease(w, r)
	defer clear(leased.record.Key[:])
	if !ok || !s.allowReadBytes(w, r, leased.record.Message.Size) {
		return
	}
	wrapped, ok := s.wrappedShare(w, r, &leased.record)
	if !ok {
		return
	}
	if leased.lease.Downloads >= store.MaxLeaseDownload {
		s.writeLeaseFailure(w, r, &leased, store.ErrLeaseLimit)
		return
	}
	blob, ok := s.openBlob(w, r, &leased.record)
	if !ok {
		return
	}
	defer blob.content.Close()
	clear(blob.salt[:])
	leased.now = time.Now()
	err := store.RecordDownload(r.Context(), s.store, &leased.record, leased.lease.MAC, leased.now)
	if err != nil {
		s.writeLeaseFailure(w, r, &leased, err)
		return
	}
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
		s.leaseTimers.Cancel(leased.record.Row.Locator)
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
	raw, err := core.DecodeExact(r.Header.Get(api.LeaseHeaderName), core.SeedLen)
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
	var key seal.BlobKey
	defer key.Clear()
	file, salt, ok := s.openCheckedBlob(w, r, record, &key)
	if !ok {
		clear(salt[:])
		return openedBlob{}, false
	}
	var stream cipher.Stream
	secretmode.Do(func() { stream = seal.BlobStream(key.Key, salt) })
	plain := cipher.StreamReader{S: stream, R: file}
	content := decryptedBlob{Reader: io.LimitReader(plain, record.Message.Size), Closer: file}
	return openedBlob{content: content, salt: salt}, true
}

// openCheckedBlob checks the stored blob against the tag in its wrap before a claim or a download spends anything, and
// fills the caller's key.
func (s *Server) openCheckedBlob(
	w http.ResponseWriter,
	r *http.Request,
	record *store.Record,
	key *seal.BlobKey,
) (io.ReadSeekCloser, seal.EraseSalt, bool) {
	if !s.readBlobKey(w, r, record, key) {
		return nil, seal.EraseSalt{}, false
	}
	file, salt, ok := s.openOrFinishBlob(w, r, record)
	if !ok {
		return nil, salt, false
	}
	if err := checkBlob(file, key, salt, seal.PaddedSize(record.Message.Size)); err != nil {
		file.Close()
		if state, finishErr := s.finishMissingBlob(r.Context(), record); finishErr == nil {
			writeGone(w, string(state))
			return nil, salt, false
		}
		s.writeBlobFailure(w, r, err)
		return nil, salt, false
	}
	return file, salt, true
}

// writeBlobFailure counts every change to a live message's stored blob as blob-changed, which serve itself never
// causes, so no other storage error hides it.
func (s *Server) writeBlobFailure(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, seal.ErrBlobChanged) || errors.Is(err, errBlobSize) || errors.Is(err, store.ErrBlobMissing) {
		s.events.ErrorContext(r.Context(), "blob-changed")
		writeError(w, http.StatusInternalServerError, errInternal)
		return
	}
	s.writeInternal(w, r, err)
}

// readBlobKey fills the caller's key, so the only copy is the one the caller clears.
func (s *Server) readBlobKey(w http.ResponseWriter, r *http.Request, record *store.Record, key *seal.BlobKey) bool {
	var err error
	secretmode.Do(func() { *key, err = record.BlobKey() })
	switch {
	case errors.Is(err, store.ErrBlobMissing):
		writeGone(w, string(record.State(time.Now())))
		return false
	case err != nil:
		s.writeInternal(w, r, err)
		return false
	default:
		return true
	}
}

// openOrFinishBlob ends a message whose blob a concurrent deletion already removed and answers it as gone.
func (s *Server) openOrFinishBlob(
	w http.ResponseWriter,
	r *http.Request,
	record *store.Record,
) (io.ReadSeekCloser, seal.EraseSalt, bool) {
	file, salt, err := s.openStoredBlob(r.Context(), record)
	if errors.Is(err, store.ErrBlobMissing) {
		state, finishErr := s.finishMissingBlob(r.Context(), record)
		if finishErr == nil {
			writeGone(w, string(state))
			return nil, salt, false
		}
		if !errors.Is(finishErr, store.ErrBlobMissing) {
			err = finishErr
		}
	}
	if err != nil {
		s.writeBlobFailure(w, r, err)
		return nil, salt, false
	}
	return file, salt, true
}

// openStoredBlob reads the erase block; a block that a concurrent deletion has overwritten counts as a missing blob.
func (s *Server) openStoredBlob(ctx context.Context, record *store.Record) (io.ReadSeekCloser, seal.EraseSalt, error) {
	file, size, err := s.blobs.Open(ctx, record.Row.BlobReference)
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
		return nil, salt, errors.Join(store.ErrBlobMissing, err)
	case err != nil:
		file.Close()
		return nil, salt, err
	default:
		return file, salt, nil
	}
}

// checkBlob tags the encrypted bytes after the erase block and seeks back, so the response streams from the same open
// file.
func checkBlob(file io.ReadSeeker, key *seal.BlobKey, salt seal.EraseSalt, encryptedLen int64) error {
	if !key.Tagged {
		return nil
	}
	start, err := file.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	var mac hash.Hash
	secretmode.Do(func() { mac = seal.BlobMAC(key.Key, salt) })
	buffer := borrowStreamBuffer()
	_, err = io.CopyBuffer(mac, io.LimitReader(file, encryptedLen), buffer[:])
	giveBackStreamBuffer(buffer)
	if err == nil {
		err = seal.CheckBlobTag(mac, &key.Tag)
	}
	if err == nil {
		_, err = file.Seek(start, io.SeekStart)
	}
	return err
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
		header.Set(api.LeaseHeaderName, served.lease)
	}
	if served.share != "" {
		header.Set(api.ShareHeaderName, served.share)
	}
	header.Set(api.PolicyHeaderName, encodePolicy(message))
	header.Set("Content-Type", octetStream)
	header.Set("Content-Length", strconv.FormatInt(size, 10))
	w.WriteHeader(http.StatusOK)
	paced := s.pace.Writer(w, http.NewResponseController(w), overall)
	buffer := borrowStreamBuffer()
	defer giveBackStreamBuffer(buffer)
	if copied, err := io.CopyBuffer(
		paced,
		blob,
		buffer[:],
	); (err != nil || copied != size) &&
		r.Context().Err() == nil {
		s.events.WarnContext(r.Context(), "blob-stream")
	}
}

const streamBufferLength = 32 << 10

// streamBuffers spares each check and download a buffer of its own, since neither the blob file, the decrypting
// reader nor the paced writer lets io.Copy skip one.
var streamBuffers = sync.Pool{New: func() any { return new([streamBufferLength]byte) }}

func borrowStreamBuffer() *[streamBufferLength]byte {
	buffer, _ := streamBuffers.Get().(*[streamBufferLength]byte)
	return buffer
}

func giveBackStreamBuffer(buffer *[streamBufferLength]byte) {
	clear(buffer[:])
	streamBuffers.Put(buffer)
}

func encodePolicy(message *store.Message) string {
	data, _ := json.Marshal(api.PolicyHeader{
		OpensMax:   new(int(message.OpensMax)),
		TTLSeconds: new(int(message.TTLSeconds)),
		KDFID:      new(int(message.KDF)),
		Flags:      new(int(message.Flags)),
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
		s.events.ErrorContext(ctx, "finish-ending", "err", logging.ErrorText(err))
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
			s.events.ErrorContext(r.Context(), "finish-ending", "err", logging.ErrorText(err))
		}
	}
	writeGone(w, string(state))
}

package server

import (
	"bytes"
	"context"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"math"
	"net/http"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/core/envelope"
	"github.com/misaon/nylzo/internal/core/signing"
	"github.com/misaon/nylzo/internal/epoch"
	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/secretmode"
	"github.com/misaon/nylzo/internal/server/config"
	"github.com/misaon/nylzo/internal/server/logging"
	"github.com/misaon/nylzo/internal/server/proof"
	"github.com/misaon/nylzo/internal/server/ratelimit"
	"github.com/misaon/nylzo/internal/store"
)

const (
	maxCreateHeader = 1024
	diskFullRetry   = 60 * time.Second
	workBusyRetry   = 5 * time.Second
	poolEmptyRetry  = 5 * time.Second
	capPendingRetry = time.Minute
)

type createRequest struct {
	header      api.CreateHeader
	id          core.ID
	readKey     []byte
	verifierKey []byte
	manageKey   []byte
	opensMax    uint16
	ttlSeconds  uint32
	bodyBy      time.Time
	outdated    bool
	generation  epoch.GenerationID
}

// The create checks run in the order of spec §6.2, each before the body is read.
func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	request, ok := s.admitCreate(w, r)
	if !ok {
		return
	}
	releaseDisk, ok := s.disk.Reserve(request.header.Size)
	if !ok {
		setRetryAfter(w, diskFullRetry)
		rejectCreate(w, r, http.StatusServiceUnavailable, errUnavailable)
		return
	}
	defer releaseDisk()
	now := time.Now()
	releaseSlot, ok := s.reserveCreate(w, r, &request, now)
	if !ok {
		return
	}
	stored := false
	defer releaseUnlessStored(r.Context(), releaseSlot, &stored)
	if !s.idAvailable(w, r, request.id) {
		return
	}
	blob := newStoredBlob()
	defer blob.clear()
	envelopeHeader, uploaded, ok := s.receiveBlob(w, r, &request, &blob)
	if !ok {
		return
	}
	stored = s.storeMessage(w, r, &request, envelopeHeader, &blob, uploaded, now)
}

func (s *Server) admitCreate(w http.ResponseWriter, r *http.Request) (createRequest, bool) {
	request, ok := parseCreateHeader(r.Header.Get(api.CreateHeaderName), s.settings.Limits)
	size := request.header.Size
	tokenOK := false
	if ok {
		request.generation, tokenOK = s.idTokens.Valid(
			request.id,
			*request.header.Epoch,
			request.header.IDToken,
			time.Now(),
		)
	}
	held, heldErr := false, error(nil)
	if tokenOK {
		held, heldErr = s.holdsGeneration(request.generation, time.Now())
	}
	switch {
	case request.outdated:
		rejectCreate(w, r, http.StatusBadRequest, errClientOutdated)
	case !ok:
		rejectCreate(w, r, http.StatusBadRequest, errBadRequest)
	case !tokenOK:
		rejectCreate(w, r, http.StatusForbidden, errIDToken)
	case heldErr != nil:
		closeAfterResponse(w, r)
		s.writeInternal(w, r, heldErr)
	case !held:
		rejectCreate(w, r, http.StatusConflict, errShareLost)
	case r.ContentLength < 0 || r.Header.Get("Content-Length") == "":
		rejectCreate(w, r, http.StatusLengthRequired, errLengthRequired)
	case r.ContentLength != size:
		rejectCreate(w, r, http.StatusBadRequest, errBadRequest)
	case size > s.settings.Limits.MaxObjectBytes:
		rejectCreate(w, r, http.StatusRequestEntityTooLarge, errTooLarge)
	default:
		request.bodyBy = time.Now().Add(max(createBodyTimeout, store.TransferTime(size)))
		s.setCreateDeadlines(w, request.bodyBy)
		return request, true
	}
	return request, false
}

func parseCreateHeader(raw string, limits config.Limits) (createRequest, bool) {
	var request createRequest
	if raw == "" || len(raw) > maxCreateHeader {
		return request, false
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	if err != nil || !decodeCreateHeader(data, &request.header) {
		return request, false
	}
	header := &request.header
	if header.Epoch == nil {
		request.outdated = true
		return request, false
	}
	if request.id, err = core.ParseID(header.ID); err != nil {
		return request, false
	}
	if !request.decodeKeys() {
		return request, false
	}
	ok := request.checkPolicy(limits)
	return request, ok
}

// checkPolicy refuses a policy outside the instance limits; the server never clamps one.
func (c *createRequest) checkPolicy(limits config.Limits) bool {
	opens, ttl := c.header.OpensMax, c.header.TTLSeconds
	if opens < 1 || opens > limits.MaxOpens || opens > math.MaxUint16 {
		return false
	}
	if ttl < envelope.MinTTLSeconds || ttl > int64(limits.MaxTTL/time.Second) || ttl > math.MaxUint32 {
		return false
	}
	c.opensMax, c.ttlSeconds = uint16(opens), uint32(ttl)
	return c.header.Size >= envelope.Pad && c.header.Size%envelope.Pad == 0
}

// decodeCreateHeader relies on json/v2 matching member names case-sensitively and refusing repeated ones.
func decodeCreateHeader(data []byte, header *api.CreateHeader) bool {
	return jsontext.Value(data).Kind() == jsontext.KindBeginObject &&
		json.Unmarshal(data, header, json.RejectUnknownMembers(true)) == nil
}

// decodeKeys refuses a key that is not a canonical point of full order and two equal keys (D18).
func (c *createRequest) decodeKeys() bool {
	var err error
	if c.readKey, err = core.DecodeExact(c.header.ReadKey, signing.PublicKeyLen); err != nil {
		return false
	}
	if c.manageKey, err = core.DecodeExact(c.header.ManageKey, signing.PublicKeyLen); err != nil {
		return false
	}
	if c.header.KDFID < 0 || c.header.KDFID > math.MaxUint8 {
		return false
	}
	kdf := core.KDF(c.header.KDFID)
	keysOK := signing.ValidPublicKey(c.readKey) && signing.ValidPublicKey(c.manageKey) &&
		!bytes.Equal(c.readKey, c.manageKey)
	if !kdf.Valid() || !keysOK || c.header.VerifierKey == nil {
		return false
	}
	if c.verifierKey, err = core.DecodeExact(*c.header.VerifierKey, signing.PublicKeyLen); err != nil {
		return false
	}
	return signing.ValidPublicKey(c.verifierKey) && !bytes.Equal(c.verifierKey, c.readKey) &&
		!bytes.Equal(c.verifierKey, c.manageKey)
}

func releaseUnlessStored(ctx context.Context, release func(context.Context), stored *bool) {
	if !*stored {
		release(ctx)
	}
}

// reserveCreate counts the attempt against the address limits and reserves a slot in the global cap.
func (s *Server) reserveCreate(
	w http.ResponseWriter,
	r *http.Request,
	request *createRequest,
	now time.Time,
) (func(context.Context), bool) {
	if retry := s.limitCreate(now, s.bucketsOf(r), request.header.Size); retry > 0 {
		setRetryAfter(w, retry)
		rejectCreate(
			w,
			r,
			http.StatusTooManyRequests,
			api.RateLimitedResponse{Error: api.ErrorRateLimited, RetryAfter: retrySeconds(retry)},
		)
		return nil, false
	}
	gate, err := s.globalGate(r.Context(), now)
	if err != nil {
		s.writeInternal(w, r, err)
		return nil, false
	}
	if gate.full {
		s.rejectCapped(w, r, now)
		return nil, false
	}
	if gate.workNeeded && !s.verifyWork(w, r, request.id, now) {
		return nil, false
	}
	slot, err := s.reserveGlobalCreate(r.Context(), now)
	if err != nil {
		s.writeInternal(w, r, err)
		return nil, false
	}
	if !slot.reserved {
		s.rejectCapped(w, r, now)
		return nil, false
	}
	release := func(ctx context.Context) { s.releaseGlobalCreate(ctx, now) }
	if !gate.workNeeded && s.workNeededAfter(slot.number-1) {
		rejectCreate(w, r, http.StatusForbidden, errWorkRequired)
		release(r.Context())
		return nil, false
	}
	return release, true
}

func (s *Server) verifyWork(w http.ResponseWriter, r *http.Request, id core.ID, now time.Time) bool {
	err := s.work.Verify(r.Context(), r.Header.Get(api.WorkHeaderName), id, now)
	switch {
	case err == nil:
		return true
	case errors.Is(err, proof.ErrWorkBusy):
		setRetryAfter(w, workBusyRetry)
		rejectCreate(w, r, http.StatusServiceUnavailable, errUnavailable)
	case errors.Is(err, proof.ErrWork):
		rejectCreate(w, r, http.StatusForbidden, errWorkRequired)
	default:
		s.writeInternal(w, r, err)
	}
	return false
}

func (s *Server) rejectCapped(w http.ResponseWriter, r *http.Request, now time.Time) {
	retry := capPendingRetry
	day := ratelimit.Day(now)
	stored, err := s.store.RateGet(r.Context(), globalStoredKey, day.Start)
	switch {
	case err != nil:
		entry := failure(r.Context(), err)
		s.events.Log(r.Context(), entry.level, "global-cap", "err", entry.text)
	case stored.Count >= s.settings.Rates.GlobalDailyCreates:
		retry = day.End.Sub(now) + time.Second
	default:
	}
	setRetryAfter(w, retry)
	rejectCreate(w, r, http.StatusServiceUnavailable, errUnavailable)
}

// idAvailable answers the same 409 for a live message and an ended one (AK21).
func (s *Server) idAvailable(w http.ResponseWriter, r *http.Request, id core.ID) bool {
	_, err := s.store.Load(r.Context(), seal.LocatorOf(id[:]))
	switch {
	case err == nil:
		rejectCreate(w, r, http.StatusConflict, errConflict)
	case !errors.Is(err, store.ErrNotFound):
		s.writeInternal(w, r, err)
	default:
		return true
	}
	return false
}

type trackingReader struct {
	reader io.Reader
	err    error
}

func (t *trackingReader) Read(buffer []byte) (int, error) {
	count, err := t.reader.Read(buffer)
	if err != nil && !errors.Is(err, io.EOF) {
		t.err = err
	}
	return count, err
}

type storedBlob struct {
	reference string
	key       seal.Key
	salt      seal.EraseSalt
}

func newStoredBlob() storedBlob {
	reference := core.NewID()
	return storedBlob{reference: core.Encode(reference[:]), key: seal.NewKey(), salt: seal.NewEraseSalt()}
}

// sealed puts the erase block first and pads the object with Padmé, so the stored length shows only a size class.
func (b *storedBlob) sealed(object io.Reader, size int64) io.Reader {
	plain := io.MultiReader(object, io.LimitReader(zeros{}, seal.PaddedSize(size)-size))
	encrypted := cipher.StreamReader{S: seal.BlobStream(b.key, b.salt), R: plain}
	return io.MultiReader(bytes.NewReader(seal.EraseBlock(b.salt)), encrypted)
}

func (b *storedBlob) clear() {
	clear(b.key[:])
	clear(b.salt[:])
}

type zeros struct{}

func (zeros) Read(buffer []byte) (int, error) {
	clear(buffer)
	return len(buffer), nil
}

func (s *Server) receiveBlob(
	w http.ResponseWriter,
	r *http.Request,
	request *createRequest,
	blob *storedBlob,
) (envelope.Header, time.Time, bool) {
	size := request.header.Size
	body := &trackingReader{
		reader: s.pace.Reader(http.MaxBytesReader(w, r.Body, size), http.NewResponseController(w), request.bodyBy),
	}
	var raw [envelope.HeaderLen]byte
	if _, err := io.ReadFull(body, raw[:]); err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return envelope.Header{}, time.Time{}, false
	}
	envelopeHeader, err := envelope.ParseHeader(raw[:])
	if raw[0] < envelope.Version {
		writeError(w, http.StatusBadRequest, errClientOutdated)
		return envelope.Header{}, time.Time{}, false
	}
	if err != nil || int(envelopeHeader.KDF) != request.header.KDFID {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return envelope.Header{}, time.Time{}, false
	}
	uploaded, err := s.blobs.PutExpiring(
		r.Context(),
		blob.reference,
		blob.sealed(io.MultiReader(bytes.NewReader(raw[:]), body), size),
		seal.StoredBlobSize(size),
		request.ttl(),
	)
	s.setWriteDeadline(w, time.Now().Add(writeTimeout))
	if err == nil {
		return envelopeHeader, uploaded, true
	}
	switch {
	case errors.Is(err, store.ErrConflict):
		rejectCreate(w, r, http.StatusConflict, errConflict)
	case errors.Is(err, store.ErrPoolEmpty):
		setRetryAfter(w, poolEmptyRetry)
		rejectCreate(w, r, http.StatusServiceUnavailable, errUnavailable)
	case isMaxBytesError(body.err):
		writeError(w, http.StatusRequestEntityTooLarge, errTooLarge)
	case body.err != nil || errors.Is(err, io.ErrUnexpectedEOF):
		writeError(w, http.StatusBadRequest, errBadRequest)
	default:
		s.writeInternal(w, r, err)
	}
	return envelope.Header{}, time.Time{}, false
}

func isMaxBytesError(err error) bool {
	_, ok := errors.AsType[*http.MaxBytesError](err)
	return ok
}

func (s *Server) storeMessage(
	w http.ResponseWriter,
	r *http.Request,
	request *createRequest,
	envelopeHeader envelope.Header,
	blob *storedBlob,
	uploaded, now time.Time,
) bool {
	message := s.newMessage(request, envelopeHeader, uploaded, blob.salt)
	err := s.persistMessage(r.Context(), request, message, blob, now)
	if err == nil {
		writeJSON(
			w,
			http.StatusCreated,
			map[string]string{"id": request.id.String(), "expires_at": formatTime(message.ExpiresAt)},
		)
		return true
	}
	if deleteErr := s.blobs.Delete(context.WithoutCancel(r.Context()), blob.reference); deleteErr != nil {
		s.events.ErrorContext(r.Context(), "create-cleanup", "err", logging.ErrorText(deleteErr))
	}
	switch {
	case errors.Is(err, store.ErrConflict):
		rejectCreate(w, r, http.StatusConflict, errConflict)
	case errors.Is(err, errShareEpoch):
		rejectCreate(w, r, http.StatusForbidden, errIDToken)
	case errors.Is(err, errGenerationGone):
		rejectCreate(w, r, http.StatusConflict, errShareLost)
	default:
		s.writeInternal(w, r, err)
	}
	return false
}

func (s *Server) persistMessage(
	ctx context.Context,
	request *createRequest,
	message *store.Message,
	blob *storedBlob,
	now time.Time,
) error {
	held, err := s.holdsGeneration(request.generation, now)
	switch {
	case err != nil:
		return err
	case !held:
		return errGenerationGone
	case !shareOutlives(message.Epoch, message.ExpiresAt):
		return errShareEpoch
	case !s.epochs.Covers(request.generation, message.Epoch):
		return errGenerationGone
	}
	locator := seal.LocatorOf(request.id[:])
	var record store.Record
	secretmode.Do(func() {
		record = store.NewRecord(
			s.recordsKey,
			locator,
			message,
			request.readKey,
			request.manageKey,
			blob.reference,
			blob.key,
		)
	})
	defer clear(record.Key[:])
	return s.store.Create(ctx, &record.Row, s.storedCreateCounters(now)...)
}

var (
	errShareEpoch     = errors.New("server: the share epoch does not cover the expiry")
	errGenerationGone = errors.New("server: the share of the id came from a seed that is gone")
)

// shareOutlives keeps every message inside the life of its share, and every share at most an hour past its message.
func shareOutlives(shareEpoch uint32, expiresAt time.Time) bool {
	expiryEpoch := epoch.Hour(expiresAt)
	return expiryEpoch <= shareEpoch && shareEpoch <= expiryEpoch+1
}

// newMessage starts the TTL when the upload has been stored, so a slow upload does not use it up.
func (s *Server) newMessage(
	request *createRequest,
	envelopeHeader envelope.Header,
	uploaded time.Time,
	salt seal.EraseSalt,
) *store.Message {
	message := &store.Message{
		Version:        envelopeHeader.Version,
		KDF:            uint8(envelopeHeader.KDF),
		Flags:          envelopeHeader.Flags,
		CreatedAt:      uploaded.UTC().Truncate(time.Minute),
		ExpiresAt:      store.ExpiresAt(uploaded, request.ttl()),
		TTLSeconds:     request.ttlSeconds,
		OpensMax:       request.opensMax,
		OpensRemaining: request.opensMax,
		Size:           request.header.Size,
		Epoch:          *request.header.Epoch,
		Generation:     request.generation,
	}
	if envelopeHeader.KDF.RequiresPassphrase() {
		verifier := s.ring.MAC(pepper.Verifier, verifierInput(salt, request.verifierKey))
		message.VerifierMAC = verifier.MAC[:]
		message.VerifierPepperID = verifier.PepperID
	}
	return message
}

func (c *createRequest) ttl() time.Duration {
	return time.Duration(c.ttlSeconds) * time.Second
}

func rejectCreate(w http.ResponseWriter, r *http.Request, status int, body any) {
	closeAfterResponse(w, r)
	writeJSON(w, status, body)
}

// closeAfterResponse ends an HTTP/1 connection whose create body was never read.
func closeAfterResponse(w http.ResponseWriter, r *http.Request) {
	if r.ProtoMajor == 1 {
		w.Header().Set("Connection", "close")
	}
}

// holdsGeneration runs in secret mode, because looking for a generation may read the seeds again.
func (s *Server) holdsGeneration(generation epoch.GenerationID, now time.Time) (bool, error) {
	var (
		held bool
		err  error
	)
	secretmode.Do(func() { held, err = s.epochs.Holds(generation, now) })
	return held, err
}

func (s *Server) setCreateDeadlines(w http.ResponseWriter, bodyBy time.Time) {
	if err := http.NewResponseController(w).SetReadDeadline(bodyBy); err != nil {
		s.events.Error("read-deadline", "err", logging.ErrorText(err))
	}
	s.setWriteDeadline(w, bodyBy.Add(writeTimeout))
}

func (s *Server) setWriteDeadline(w http.ResponseWriter, by time.Time) {
	if err := http.NewResponseController(w).SetWriteDeadline(by); err != nil {
		s.events.Error("write-deadline", "err", logging.ErrorText(err))
	}
}

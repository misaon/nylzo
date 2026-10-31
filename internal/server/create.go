package server

import (
	"bytes"
	"context"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"time"

	"github.com/misaon/nylzo/internal/buildinfo"
	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/envelope"
	"github.com/misaon/nylzo/internal/core/signing"
	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/server/abuse"
	"github.com/misaon/nylzo/internal/server/config"
	"github.com/misaon/nylzo/internal/server/logging"
	"github.com/misaon/nylzo/internal/server/proof"
	"github.com/misaon/nylzo/internal/server/ratelimit"
	"github.com/misaon/nylzo/internal/store"
)

const (
	createHeaderName = "X-Nylzo-Create"
	workHeaderName   = "X-Nylzo-Pow"
	maxCreateHeader  = 1024
	diskFullRetry    = 60 * time.Second
	workBusyRetry    = 5 * time.Second
	capPendingRetry  = time.Minute
)

type createHeader struct {
	ID          string  `json:"id"`
	IDToken     string  `json:"id_token"`
	ReadKey     string  `json:"read_key"`
	VerifierKey *string `json:"verifier_key"`
	ManageKey   string  `json:"manage_key"`
	KDFID       int     `json:"kdf_id"`
	OpensMax    int     `json:"opens_max"`
	TTLSeconds  int64   `json:"ttl_s"`
	Size        int64   `json:"size"`
}

var createHeaderKeys = map[string]bool{
	"id":           true,
	"id_token":     true,
	"read_key":     true,
	"verifier_key": true,
	"manage_key":   true,
	"kdf_id":       true,
	"opens_max":    true,
	"ttl_s":        true,
	"size":         true,
}

type createRequest struct {
	header      createHeader
	id          core.ID
	readKey     []byte
	verifierKey []byte
	manageKey   []byte
	opensMax    uint16
	ttlSeconds  uint32
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
	defer func() {
		if !stored {
			releaseSlot()
		}
	}()
	if !s.idAvailable(w, r, request.id) {
		return
	}
	blob := newStoredBlob()
	envelopeHeader, ok := s.receiveBlob(w, r, &request, &blob)
	if !ok {
		return
	}
	stored = s.storeMessage(w, r, &request, envelopeHeader, &blob, now)
}

func (s *Server) admitCreate(w http.ResponseWriter, r *http.Request) (createRequest, bool) {
	request, ok := parseCreateHeader(r.Header.Get(createHeaderName), s.settings.Limits)
	size := request.header.Size
	switch {
	case !ok:
		rejectCreate(w, r, http.StatusBadRequest, errBadRequest)
	case !s.idTokens.Valid(request.id, request.header.IDToken, time.Now()):
		rejectCreate(w, r, http.StatusForbidden, errIDToken)
	case r.ContentLength < 0 || r.Header.Get("Content-Length") == "":
		rejectCreate(w, r, http.StatusLengthRequired, errLengthRequired)
	case r.ContentLength != size:
		rejectCreate(w, r, http.StatusBadRequest, errBadRequest)
	case size > s.settings.Limits.MaxObjectBytes:
		rejectCreate(w, r, http.StatusRequestEntityTooLarge, errTooLarge)
	default:
		s.setCreateDeadlines(w, time.Now().Add(max(readTimeout, store.TransferTime(size))))
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
	if err != nil || !onlyCreateHeaderKeys(data) || json.Unmarshal(data, &request.header) != nil {
		return request, false
	}
	header := &request.header
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
	return c.header.Size >= envelope.HeaderLen+envelope.TagLen+envelope.MetaLenPrefix
}

func onlyCreateHeaderKeys(data []byte) bool {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return false
	}
	for key := range fields {
		if !createHeaderKeys[key] {
			return false
		}
	}
	return true
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
	if !kdf.Valid() || !keysOK || kdf.RequiresPassphrase() != (c.header.VerifierKey != nil) {
		return false
	}
	if c.header.VerifierKey == nil {
		return true
	}
	if c.verifierKey, err = core.DecodeExact(*c.header.VerifierKey, signing.PublicKeyLen); err != nil {
		return false
	}
	return signing.ValidPublicKey(c.verifierKey) && !bytes.Equal(c.verifierKey, c.readKey) &&
		!bytes.Equal(c.verifierKey, c.manageKey)
}

// reserveCreate counts the attempt against the address limits and reserves a slot in the global cap.
func (s *Server) reserveCreate(
	w http.ResponseWriter,
	r *http.Request,
	request *createRequest,
	now time.Time,
) (func(), bool) {
	if retry := s.limitCreate(now, s.clientAddress(r), request.header.Size); retry > 0 {
		setRetryAfter(w, retry)
		rejectCreate(
			w,
			r,
			http.StatusTooManyRequests,
			rateLimited{Error: "rate_limited", RetryAfter: retrySeconds(retry)},
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
	release := func() { s.releaseGlobalCreate(r.Context(), now) }
	if !gate.workNeeded && s.workNeededAfter(slot.number-1) {
		rejectCreate(w, r, http.StatusForbidden, errWorkRequired)
		release()
		return nil, false
	}
	return release, true
}

func (s *Server) verifyWork(w http.ResponseWriter, r *http.Request, id core.ID, now time.Time) bool {
	err := s.work.Verify(r.Context(), r.Header.Get(workHeaderName), id, now)
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
		s.logger.Log(r.Context(), entry.level, "global-cap", "err", entry.text)
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
	ref string
	key seal.Key
}

func newStoredBlob() storedBlob {
	ref := core.NewID()
	return storedBlob{ref: core.Encode(ref[:]), key: seal.NewKey()}
}

// sealed encrypts the object with the blob key and pads it with Padmé, so the stored length shows only a size class.
func (b *storedBlob) sealed(object io.Reader, size, padded int64) io.Reader {
	plain := io.MultiReader(object, io.LimitReader(zeros{}, padded-size))
	return cipher.StreamReader{S: seal.BlobStream(b.key), R: plain}
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
) (envelope.Header, bool) {
	size := request.header.Size
	body := &trackingReader{reader: http.MaxBytesReader(w, r.Body, size)}
	var raw [envelope.HeaderLen]byte
	if _, err := io.ReadFull(body, raw[:]); err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return envelope.Header{}, false
	}
	envelopeHeader, err := envelope.ParseHeader(raw[:])
	if err != nil || int(envelopeHeader.KDF) != request.header.KDFID {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return envelope.Header{}, false
	}
	padded := seal.PaddedSize(size)
	err = s.blobs.Put(
		r.Context(),
		blob.ref,
		blob.sealed(io.MultiReader(bytes.NewReader(raw[:]), body), size, padded),
		padded,
	)
	s.setWriteDeadline(w, time.Now().Add(writeTimeout))
	if err == nil {
		return envelopeHeader, true
	}
	switch {
	case errors.Is(err, store.ErrConflict):
		rejectCreate(w, r, http.StatusConflict, errConflict)
	case isMaxBytesError(body.err):
		writeError(w, http.StatusRequestEntityTooLarge, errTooLarge)
	case body.err != nil || errors.Is(err, io.ErrUnexpectedEOF):
		writeError(w, http.StatusBadRequest, errBadRequest)
	default:
		s.writeInternal(w, r, err)
	}
	return envelope.Header{}, false
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
	now time.Time,
) bool {
	message := s.newMessage(request, envelopeHeader, time.Now())
	locator := seal.LocatorOf(request.id[:])
	record := store.NewRecord(locator, message, request.readKey, request.manageKey, blob.ref, blob.key)
	err := s.store.Create(r.Context(), &record.Row, s.storedCreateCounters(now)...)
	if err == nil {
		writeJSON(
			w,
			http.StatusCreated,
			map[string]string{"id": request.id.String(), "expires_at": formatTime(message.ExpiresAt)},
		)
		return true
	}
	if deleteErr := s.blobs.Delete(context.WithoutCancel(r.Context()), blob.ref); deleteErr != nil {
		s.logger.ErrorContext(r.Context(), "create-cleanup", "err", logging.ErrorText(deleteErr))
	}
	if errors.Is(err, store.ErrConflict) {
		rejectCreate(w, r, http.StatusConflict, errConflict)
		return false
	}
	s.writeInternal(w, r, err)
	return false
}

// newMessage starts the TTL when the upload has been stored, so a slow upload does not use it up.
func (s *Server) newMessage(request *createRequest, envelopeHeader envelope.Header, uploaded time.Time) *store.Message {
	message := &store.Message{
		Version:        envelopeHeader.Version,
		KDF:            uint8(envelopeHeader.KDF),
		Flags:          envelopeHeader.Flags,
		CreatedAt:      uploaded.UTC().Truncate(time.Minute),
		ExpiresAt:      store.CeilMinute(uploaded.UTC().Add(time.Duration(request.ttlSeconds) * time.Second)),
		TTLSeconds:     request.ttlSeconds,
		OpensMax:       request.opensMax,
		OpensRemaining: request.opensMax,
		Size:           request.header.Size,
	}
	if request.verifierKey != nil {
		verifier := s.ring.MAC(pepper.Verifier, request.verifierKey)
		message.VerifierMAC = verifier.MAC[:]
		message.VerifierPepperID = verifier.PepperID
	}
	return message
}

func rejectCreate(w http.ResponseWriter, r *http.Request, status int, body any) {
	if r.ProtoMajor == 1 {
		w.Header().Set("Connection", "close")
	}
	writeJSON(w, status, body)
}

func (s *Server) setCreateDeadlines(w http.ResponseWriter, bodyBy time.Time) {
	if err := http.NewResponseController(w).SetReadDeadline(bodyBy); err != nil {
		s.logger.Error("read-deadline", "err", logging.ErrorText(err))
	}
	s.setWriteDeadline(w, bodyBy.Add(writeTimeout))
}

func (s *Server) setWriteDeadline(w http.ResponseWriter, by time.Time) {
	if err := http.NewResponseController(w).SetWriteDeadline(by); err != nil {
		s.logger.Error("write-deadline", "err", logging.ErrorText(err))
	}
}

const TransparencyPath = "/.well-known/nylzo/transparency.json"

type instanceResponse struct {
	Limits          instanceLimits   `json:"limits"`
	Work            instanceWork     `json:"pow"`
	Create          instanceCreate   `json:"create"`
	Versions        instanceVersions `json:"versions"`
	Build           buildinfo.Info   `json:"build"`
	FileCompression string           `json:"file_compression"`
}

type instanceLimits struct {
	MaxFileBytes   int64 `json:"max_file_bytes"`
	MaxTextBytes   int64 `json:"max_text_bytes"`
	MaxObjectBytes int64 `json:"max_object_bytes"`
	MaxTTL         int64 `json:"max_ttl_s"`
	DefaultTTL     int64 `json:"default_ttl_s"`
	MaxOpens       int   `json:"max_opens"`
}

type instanceWork struct {
	Required   bool   `json:"required"`
	Difficulty int    `json:"difficulty"`
	Challenge  string `json:"challenge,omitempty"`
}

type instanceCreate struct {
	ID        string `json:"id"`
	IDToken   string `json:"id_token"`
	ExpiresAt string `json:"expires_at"`
}

type instanceVersions struct {
	API      int    `json:"api"`
	Envelope int    `json:"envelope"`
	Fragment int    `json:"fragment"`
	Server   string `json:"server"`
}

type challengeResponse struct {
	Challenge string `json:"challenge"`
	ExpiresAt string `json:"expires_at"`
}

type transparencyResponse struct {
	Build     buildinfo.Info        `json:"build"`
	Server    string                `json:"server"`
	Operator  *config.Operator      `json:"operator,omitempty"`
	Limits    instanceLimits        `json:"limits"`
	Rates     transparencyRates     `json:"rates"`
	Retention transparencyRetention `json:"retention"`
}

type transparencyRates struct {
	CreatePerHour      int64 `json:"create_per_hour"`
	CreatePerDay       int64 `json:"create_per_day"`
	CreateBytesPerDay  int64 `json:"create_bytes_per_day"`
	ReadPerHour        int64 `json:"read_per_hour"`
	AbusePerHour       int64 `json:"abuse_per_hour"`
	CoarseMultiplier   int64 `json:"coarse_multiplier"`
	GlobalDailyCreates int64 `json:"global_daily_creates"`
	PoWDifficulty      int   `json:"pow_difficulty"`
}

type transparencyRetention struct {
	TombstoneSeconds    int64 `json:"tombstone_s"`
	AbuseReportSeconds  int64 `json:"abuse_report_s"`
	DailyCounterSeconds int64 `json:"daily_counter_s"`
}

func (s *Server) handleInstance(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	id := core.NewID()
	token, expires := s.idTokens.Issue(id, now)
	response := instanceResponse{
		FileCompression: "gzip",
		Limits:          s.instanceLimits(),
		Build:           s.buildInformation(),
		Work:            instanceWork{Difficulty: s.settings.PoWDifficulty},
		Create:          instanceCreate{ID: id.String(), IDToken: token, ExpiresAt: formatTime(expires)},
		Versions: instanceVersions{
			API:      1,
			Envelope: envelope.Version,
			Fragment: core.FragmentVersion,
			Server:   s.settings.Version,
		},
	}
	gate, err := s.globalGate(r.Context(), now)
	if err != nil {
		s.writeInternal(w, r, err)
		return
	}
	if gate.workNeeded {
		response.Work.Required = true
		response.Work.Challenge = s.work.Challenge(now)
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleChallenge(w http.ResponseWriter, _ *http.Request) {
	challenge, expires := s.challenges.Issue(time.Now())
	writeJSON(w, http.StatusOK, challengeResponse{Challenge: core.Encode(challenge[:]), ExpiresAt: formatTime(expires)})
}

func (s *Server) handleTransparency(w http.ResponseWriter, _ *http.Request) {
	rates := s.settings.Rates
	day := ratelimit.Day(time.Unix(0, 0))
	response := transparencyResponse{
		Server: s.settings.Version,
		Limits: s.instanceLimits(),
		Build:  s.buildInformation(),
		Rates: transparencyRates{
			CreatePerHour:      rates.CreatePerHour,
			CreatePerDay:       rates.CreatePerDay,
			CreateBytesPerDay:  rates.CreateBytesPerDay,
			ReadPerHour:        rates.ReadPerHour,
			AbusePerHour:       rates.AbusePerHour,
			CoarseMultiplier:   rates.CoarseMultiplier,
			GlobalDailyCreates: rates.GlobalDailyCreates,
			PoWDifficulty:      s.settings.PoWDifficulty,
		},
		Retention: transparencyRetention{
			TombstoneSeconds:    int64(store.TombstoneTTL / time.Second),
			AbuseReportSeconds:  int64(abuse.Retention / time.Second),
			DailyCounterSeconds: int64(day.PurgeAt().Sub(day.End) / time.Second),
		},
	}
	if operator := s.settings.Operator; operator != (config.Operator{}) {
		response.Operator = &operator
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) instanceLimits() instanceLimits {
	limits := s.settings.Limits
	return instanceLimits{
		MaxFileBytes:   limits.MaxFileBytes,
		MaxTextBytes:   envelope.MaxTextBytes,
		MaxObjectBytes: limits.MaxObjectBytes,
		MaxTTL:         int64(limits.MaxTTL / time.Second),
		DefaultTTL:     int64(limits.DefaultTTL / time.Second),
		MaxOpens:       limits.MaxOpens,
	}
}

func (s *Server) buildInformation() buildinfo.Info {
	information := s.settings.Build
	information.Version = s.settings.Version
	return information
}

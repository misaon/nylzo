package server

import (
	"bytes"
	"context"
	"crypto/cipher"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/misaon/nylzo/internal/buildinfo"
	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/envelope"
	"github.com/misaon/nylzo/internal/core/share"
	"github.com/misaon/nylzo/internal/core/signing"
	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/secretmode"
	"github.com/misaon/nylzo/internal/server/abuse"
	"github.com/misaon/nylzo/internal/server/config"
	"github.com/misaon/nylzo/internal/server/epoch"
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
	// storeSlack covers rounding expires_at up to the minute and writing the arrived upload (spec section 5.6).
	storeSlack = 2 * time.Minute
)

type createHeader struct {
	ID          string  `json:"id"`
	IDToken     string  `json:"id_token"`
	Epoch       *uint32 `json:"epoch"`
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
	"epoch":        true,
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
	bodyBy      time.Time
	outdated    bool
	ratchetID   epoch.RatchetID
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
	defer blob.clear()
	envelopeHeader, ok := s.receiveBlob(w, r, &request, &blob)
	if !ok {
		return
	}
	stored = s.storeMessage(w, r, &request, envelopeHeader, &blob, now)
}

func (s *Server) admitCreate(w http.ResponseWriter, r *http.Request) (createRequest, bool) {
	request, ok := parseCreateHeader(r.Header.Get(createHeaderName), s.settings.Limits)
	size := request.header.Size
	tokenOK := false
	if ok {
		var ratchetID [proof.RatchetIDLen]byte
		ratchetID, tokenOK = s.idTokens.Valid(request.id, *request.header.Epoch, request.header.IDToken, time.Now())
		request.ratchetID = epoch.RatchetID(ratchetID)
	}
	switch {
	case request.outdated:
		rejectCreate(w, r, http.StatusBadRequest, errClientOutdated)
	case !ok:
		rejectCreate(w, r, http.StatusBadRequest, errBadRequest)
	case !tokenOK:
		rejectCreate(w, r, http.StatusForbidden, errIDToken)
	case request.ratchetID != s.epochs.ID():
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
	if err != nil || !onlyCreateHeaderKeys(data) || json.Unmarshal(data, &request.header) != nil {
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
	if !kdf.Valid() || !keysOK || c.header.VerifierKey == nil {
		return false
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
	ref  string
	key  seal.Key
	salt seal.EraseSalt
}

func newStoredBlob() storedBlob {
	ref := core.NewID()
	return storedBlob{ref: core.Encode(ref[:]), key: seal.NewKey(), salt: seal.NewEraseSalt()}
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
) (envelope.Header, bool) {
	size := request.header.Size
	body := &trackingReader{reader: &pacedReader{
		reader:     http.MaxBytesReader(w, r.Body, size),
		controller: http.NewResponseController(w),
		pace:       s.pace,
		overall:    request.bodyBy,
	}}
	var raw [envelope.HeaderLen]byte
	if _, err := io.ReadFull(body, raw[:]); err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return envelope.Header{}, false
	}
	envelopeHeader, err := envelope.ParseHeader(raw[:])
	if raw[0] < envelope.Version {
		writeError(w, http.StatusBadRequest, errClientOutdated)
		return envelope.Header{}, false
	}
	if err != nil || int(envelopeHeader.KDF) != request.header.KDFID {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return envelope.Header{}, false
	}
	err = s.blobs.Put(
		r.Context(),
		blob.ref,
		blob.sealed(io.MultiReader(bytes.NewReader(raw[:]), body), size),
		seal.StoredBlobSize(size),
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
	message := s.newMessage(request, envelopeHeader, time.Now(), blob.salt)
	locator := seal.LocatorOf(request.id[:])
	err := errShareEpoch
	if request.ratchetID != s.epochs.ID() {
		err = errRatchetChanged
	} else if shareOutlives(message.Epoch, message.ExpiresAt) {
		var record store.Record
		secretmode.Do(func() {
			record = store.NewRecord(locator, message, request.readKey, request.manageKey, blob.ref, blob.key)
		})
		defer clear(record.Key[:])
		err = s.store.Create(r.Context(), &record.Row, s.storedCreateCounters(now)...)
	}
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
	switch {
	case errors.Is(err, store.ErrConflict):
		rejectCreate(w, r, http.StatusConflict, errConflict)
	case errors.Is(err, errShareEpoch):
		rejectCreate(w, r, http.StatusForbidden, errIDToken)
	case errors.Is(err, errRatchetChanged):
		rejectCreate(w, r, http.StatusConflict, errShareLost)
	default:
		s.writeInternal(w, r, err)
	}
	return false
}

var (
	errShareEpoch     = errors.New("server: the share epoch does not cover the expiry")
	errRatchetChanged = errors.New("server: the share of the id came from an earlier seed")
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
		ExpiresAt:      store.CeilMinute(uploaded.UTC().Add(time.Duration(request.ttlSeconds) * time.Second)),
		TTLSeconds:     request.ttlSeconds,
		OpensMax:       request.opensMax,
		OpensRemaining: request.opensMax,
		Size:           request.header.Size,
		Epoch:          *request.header.Epoch,
		RatchetID:      request.ratchetID,
	}
	if envelopeHeader.KDF.RequiresPassphrase() {
		verifier := s.ring.MAC(pepper.Verifier, verifierInput(salt, request.verifierKey))
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
	Limits   instanceLimits   `json:"limits"`
	Work     instanceWork     `json:"pow"`
	Create   *instanceCreate  `json:"create,omitempty"`
	Versions instanceVersions `json:"versions"`
	Build    buildinfo.Info   `json:"build"`
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
	Epoch     uint32 `json:"epoch"`
	Share     string `json:"share"`
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
	creation, ok := s.parseCreation(r.Header)
	if !ok {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	response := instanceResponse{
		Limits: s.instanceLimits(),
		Build:  s.buildInformation(),
		Work:   instanceWork{Difficulty: s.settings.PoWDifficulty},
		Versions: instanceVersions{
			API:      1,
			Envelope: int(envelope.Version),
			Fragment: int(core.FragmentVersion),
			Server:   s.settings.Version,
		},
	}
	if creation.requested {
		created, err := s.issueID(&creation, now)
		if err != nil {
			s.writeInternal(w, r, err)
			return
		}
		response.Create = &created
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

type creationRequest struct {
	requested bool
	ttl       time.Duration
	shareKey  *ecdh.PublicKey
}

// parseCreation reads the TTL and share key headers, which a client sends together when it is about to create a message.
func (s *Server) parseCreation(header http.Header) (creationRequest, bool) {
	ttlValues, keyValues := header.Values(ttlHeaderName), header.Values(shareKeyHeaderName)
	if len(ttlValues) == 0 && len(keyValues) == 0 {
		return creationRequest{}, true
	}
	if len(ttlValues) != 1 || len(keyValues) != 1 {
		return creationRequest{}, false
	}
	seconds, err := strconv.ParseInt(ttlValues[0], 10, 64)
	if err != nil || seconds < envelope.MinTTLSeconds || seconds > int64(s.settings.Limits.MaxTTL/time.Second) ||
		strconv.FormatInt(seconds, 10) != ttlValues[0] {
		return creationRequest{}, false
	}
	key, err := share.ParseKey(keyValues[0])
	if err != nil {
		return creationRequest{}, false
	}
	return creationRequest{requested: true, ttl: time.Duration(seconds) * time.Second, shareKey: key}, true
}

// issueID hands out an id with the share of the hour in which a message made with it can expire at the latest.
func (s *Server) issueID(creation *creationRequest, now time.Time) (instanceCreate, error) {
	id := core.NewID()
	upload := max(createBodyTimeout, store.TransferTime(s.settings.Limits.MaxObjectBytes))
	latestExpiry := now.Add(proof.IDTokenTTL() + upload + storeSlack + creation.ttl)
	shareEpoch := epoch.Hour(latestExpiry)
	ratchetID := s.epochs.ID()
	token, expires := s.idTokens.Issue(id, shareEpoch, ratchetID, now)
	expiryShare, err := s.epochs.Share(ratchetID, shareEpoch, seal.LocatorOf(id[:]), now)
	if err != nil {
		return instanceCreate{}, err
	}
	defer clear(expiryShare[:])
	wrapped, err := share.Wrap(&expiryShare, creation.shareKey)
	if err != nil {
		return instanceCreate{}, err
	}
	return instanceCreate{
		ID:        id.String(),
		IDToken:   token,
		ExpiresAt: formatTime(expires),
		Epoch:     shareEpoch,
		Share:     core.Encode(wrapped[:]),
	}, nil
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

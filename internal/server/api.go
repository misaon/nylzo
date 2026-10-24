package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/store"
)

const (
	headerCreate    = "X-Nylzo-Create"
	headerPoW       = "X-Nylzo-Pow"
	headerLease     = "X-Nylzo-Lease"
	headerPolicy    = "X-Nylzo-Policy"
	headerVerifier  = "X-Nylzo-Verifier"
	maxCreateHeader = 1024
	maxJSONBody     = 4096
	timeFormat      = time.RFC3339
	octetStream     = "application/octet-stream"
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

var createHeaderKeys = map[string]bool{"id": true, "id_token": true, "read_key": true, "verifier_key": true, "manage_key": true, "kdf_id": true, "opens_max": true, "ttl_s": true, "size": true}

type policyHeader struct {
	OpensMax int `json:"opens_max"`
	TTL      int `json:"ttl_s"`
	KDFID    int `json:"kdf_id"`
	Flags    int `json:"flags"`
}

type metadataResponse struct {
	State              string `json:"state"`
	CreatedAt          string `json:"created_at,omitempty"`
	ExpiresAt          string `json:"expires_at,omitempty"`
	OpensMax           int    `json:"opens_max"`
	OpensRemaining     int    `json:"opens_remaining"`
	TTL                int    `json:"ttl_s"`
	PassphraseRequired bool   `json:"passphrase_required"`
	KDFID              int    `json:"kdf_id"`
	Size               int64  `json:"size"`
	FirstOpenedAt      string `json:"first_opened_at,omitempty"`
	EndedAt            string `json:"ended_at,omitempty"`
}

type statusResponse struct {
	State                    string `json:"state"`
	OpensUsed                int    `json:"opens_used"`
	OpensMax                 int    `json:"opens_max"`
	CreatedAt                string `json:"created_at,omitempty"`
	ExpiresAt                string `json:"expires_at,omitempty"`
	FirstOpenedAt            string `json:"first_opened_at,omitempty"`
	EndedAt                  string `json:"ended_at,omitempty"`
	FailedPassphraseAttempts int    `json:"failed_passphrase_attempts"`
}

type instanceResponse struct {
	Limits   instanceLimits   `json:"limits"`
	PoW      instancePoW      `json:"pow"`
	Create   instanceCreate   `json:"create"`
	Versions instanceVersions `json:"versions"`
}

type instanceCreate struct {
	ID        string `json:"id"`
	IDToken   string `json:"id_token"`
	ExpiresAt string `json:"expires_at"`
}

type instanceLimits struct {
	MaxFileBytes   int64 `json:"max_file_bytes"`
	MaxTextBytes   int64 `json:"max_text_bytes"`
	MaxObjectBytes int64 `json:"max_object_bytes"`
	MaxTTL         int64 `json:"max_ttl_s"`
	DefaultTTL     int64 `json:"default_ttl_s"`
	MaxOpens       int   `json:"max_opens"`
	DefaultKDF     int   `json:"default_kdf_id"`
}

type instancePoW struct {
	Required   bool   `json:"required"`
	Difficulty int    `json:"difficulty"`
	Challenge  string `json:"challenge,omitempty"`
}

type instanceVersions struct {
	API      int    `json:"api"`
	Envelope int    `json:"envelope"`
	Fragment int    `json:"fragment"`
	Server   string `json:"server"`
}

func formatTime(t time.Time) string {
	return t.UTC().Format(timeFormat)
}

func formatOptTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return formatTime(*t)
}

func clientGone(ctx context.Context, err error) bool {
	return ctx.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded))
}

func failure(ctx context.Context, err error) (slog.Level, string) {
	if clientGone(ctx, err) {
		return slog.LevelInfo, "client went away"
	}
	return slog.LevelError, ErrorText(err)
}

func (s *Server) writeInternal(w http.ResponseWriter, r *http.Request, err error) {
	level, text := failure(r.Context(), err)
	s.log.Log(r.Context(), level, "handler", "route", r.Pattern, "err", text)
	if level == slog.LevelInfo {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	writeError(w, http.StatusInternalServerError, errInternal)
}

func (s *Server) takeStream(w http.ResponseWriter, r *http.Request) (func(), bool) {
	addr := s.clientAddr(r)
	source, ranges := s.addrKey.bucket(addr, false), s.addrKey.bucket(addr, true)
	if !s.streams.take(source, ranges) {
		setRetryAfter(w, streamBusyRetry)
		writeError(w, http.StatusServiceUnavailable, errUnavailable)
		return nil, false
	}
	return func() { s.streams.release(source, ranges) }, true
}

type access struct {
	rec    store.Record
	id     core.ID
	leases []store.Lease
	sig    core.Signature
}

var dummyPublicKey = core.PublicKey(new([core.TokenLen]byte))

func signedPurpose(p pepper.Purpose) string {
	if p == pepper.Manage {
		return core.PurposeManage
	}
	return core.PurposeRead
}

func (s *Server) authorize(ctx context.Context, r *http.Request, idStr string, purpose pepper.Purpose, body []byte) (access, bool, error) {
	id, idErr := core.ParseID(idStr)
	sig, sigErr := core.ParseAuthorization(r.Header.Get("Authorization"))
	if sigErr != nil {
		sig = core.Signature{PublicKey: dummyPublicKey}
	}
	fresh := spentBy(r, sig.Challenge)
	a := access{id: id, sig: sig}
	mac, pepperID, found := s.ring.Dummy(), s.ring.CurrentID(), false
	if idErr == nil {
		rec, err := s.store.Get(ctx, id.String())
		switch {
		case errors.Is(err, store.ErrNotFound):
		case err != nil:
			return a, false, err
		default:
			a.rec, found = rec, true
			mac, pepperID = rec.ReadMAC(), rec.PepperID()
			if purpose == pepper.Manage {
				mac = rec.ManageMAC()
			}
		}
	}
	keyOK := core.ValidPublicKey(sig.PublicKey[:])
	if !keyOK {
		sig.PublicKey = dummyPublicKey
	}
	macOK := s.ring.Verify(purpose, sig.PublicKey[:], mac, pepperID)
	sigOK := sig.Verify(signedPurpose(purpose), r.Method, r.URL.EscapedPath(), body)
	if !found || sigErr != nil || !fresh || !keyOK || !macOK || !sigOK {
		return a, false, nil
	}
	if settled, leases, ok := s.settle(ctx, id.String(), a.rec.Message, s.clock()); ok {
		a.rec = settled
	} else {
		a.leases = leases
	}
	return a, true, nil
}

func (s *Server) settle(ctx context.Context, id string, m *store.Message, now time.Time) (store.Record, []store.Lease, bool) {
	if m == nil {
		return store.Record{}, nil, false
	}
	var err error
	switch {
	case ended(m, now):
		_, err = store.EndMessage(ctx, s.store, s.blobs, id, store.StateExpired, now)
	case m.OpensRemaining == 0:
		leases, lerr := s.store.Leases(ctx, id)
		if lerr != nil {
			level, text := failure(ctx, lerr)
			s.log.Log(ctx, level, "settle", "err", text)
			return store.Record{}, nil, false
		}
		if slices.ContainsFunc(leases, func(l store.Lease) bool { return l.Active(now) }) {
			return store.Record{}, leases, false
		}
		_, err = store.Release(ctx, s.store, s.blobs, id, now)
	default:
		return store.Record{}, nil, false
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		level, text := failure(ctx, err)
		s.log.Log(ctx, level, "settle", "err", text)
		return store.Record{}, nil, false
	}
	rec, err := s.store.Get(ctx, id)
	if err != nil {
		level, text := failure(ctx, err)
		s.log.Log(ctx, level, "settle", "err", text)
		return store.Record{}, nil, false
	}
	return rec, nil, true
}

func ended(m *store.Message, now time.Time) bool {
	return m.Ending != "" || !now.Before(m.ExpiresAt)
}

func liveState(m *store.Message, now time.Time) store.State {
	switch {
	case m.Ending != "":
		return m.Ending
	case !now.Before(m.ExpiresAt) && m.OpensRemaining == 0:
		return store.StateOpened
	case !now.Before(m.ExpiresAt):
		return store.StateExpired
	}
	return m.State
}

func (s *Server) handleInstance(w http.ResponseWriter, r *http.Request) {
	resp := instanceResponse{
		Limits:   s.instanceLimits(),
		PoW:      instancePoW{Difficulty: s.cfg.PoWDifficulty},
		Versions: instanceVersions{API: 1, Envelope: core.Version, Fragment: core.FragmentVersion, Server: s.cfg.Version},
	}
	now := s.clock()
	resp.Create = s.issueID(now)
	gate, err := s.globalGate(r.Context(), now)
	if err != nil {
		s.writeInternal(w, r, err)
		return
	}
	if gate.powNeeded {
		resp.PoW.Required = true
		resp.PoW.Challenge = s.powChallenge(now)
	}
	writeJSON(w, http.StatusOK, resp)
}

type createRequest struct {
	header      createHeader
	id          core.ID
	readKey     []byte
	verifierKey []byte
	manageKey   []byte
}

func (s *Server) parseCreateHeader(r *http.Request) (createRequest, bool) {
	var req createRequest
	raw := r.Header.Get(headerCreate)
	if raw == "" || len(raw) > maxCreateHeader {
		return req, false
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	if err != nil {
		return req, false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return req, false
	}
	for key := range fields {
		if !createHeaderKeys[key] {
			return req, false
		}
	}
	h := &req.header
	if err := json.Unmarshal(data, h); err != nil {
		return req, false
	}
	if req.id, err = core.ParseID(h.ID); err != nil {
		return req, false
	}
	if req.readKey, err = core.DecodeExact(h.ReadKey, core.PublicKeyLen); err != nil {
		return req, false
	}
	if req.manageKey, err = core.DecodeExact(h.ManageKey, core.PublicKeyLen); err != nil {
		return req, false
	}
	kdf := core.KDF(h.KDFID)
	kdfOK := h.KDFID >= 0 && h.KDFID <= 255 && kdf.Valid()
	keysOK := core.ValidPublicKey(req.readKey) && core.ValidPublicKey(req.manageKey) && !bytes.Equal(req.readKey, req.manageKey)
	if !kdfOK || !keysOK || kdf.RequiresPassphrase() != (h.VerifierKey != nil) {
		return req, false
	}
	if h.VerifierKey != nil {
		if req.verifierKey, err = core.DecodeExact(*h.VerifierKey, core.PublicKeyLen); err != nil {
			return req, false
		}
		verifierOK := core.ValidPublicKey(req.verifierKey) && !bytes.Equal(req.verifierKey, req.readKey) && !bytes.Equal(req.verifierKey, req.manageKey)
		if !verifierOK {
			return req, false
		}
	}
	l := s.cfg.Limits
	opensOK := h.OpensMax >= 1 && h.OpensMax <= l.MaxOpens
	ttlOK := h.TTLSeconds >= core.MinTTLSeconds && h.TTLSeconds <= int64(l.MaxTTL/time.Second)
	sizeOK := h.Size >= core.HeaderLen+core.TagLen+core.MetaLenPrefix
	return req, opensOK && ttlOK && sizeOK
}

func (s *Server) rejectCreate(w http.ResponseWriter, r *http.Request, status int, v any) {
	if r.ProtoMajor == 1 {
		w.Header().Set("Connection", "close")
	}
	writeJSON(w, status, v)
}

type trackingReader struct {
	r   io.Reader
	err error
}

func (t *trackingReader) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		t.err = err
	}
	return n, err
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	req, ok := s.parseCreateHeader(r)
	h, id := req.header, req.id
	if !ok {
		s.rejectCreate(w, r, http.StatusBadRequest, errBadRequest)
		return
	}
	if !s.validIDToken(id, h.IDToken, s.clock()) {
		s.rejectCreate(w, r, http.StatusForbidden, errIDToken)
		return
	}
	if r.ContentLength < 0 || r.Header.Get("Content-Length") == "" {
		s.rejectCreate(w, r, http.StatusLengthRequired, errLengthRequired)
		return
	}
	if r.ContentLength != h.Size {
		s.rejectCreate(w, r, http.StatusBadRequest, errBadRequest)
		return
	}
	if h.Size > s.cfg.Limits.MaxObjectBytes {
		s.rejectCreate(w, r, http.StatusRequestEntityTooLarge, errTooLarge)
		return
	}
	s.setCreateDeadlines(w, time.Now().Add(max(readTimeout, store.TransferTime(h.Size))))
	releaseDisk, ok := s.reserveDisk(h.Size)
	if !ok {
		setRetryAfter(w, diskFullRetry)
		s.rejectCreate(w, r, http.StatusServiceUnavailable, errUnavailable)
		return
	}
	defer releaseDisk()
	now := s.clock()
	if retry := s.limitCreate(now, s.clientAddr(r), h.Size); retry > 0 {
		setRetryAfter(w, retry)
		s.rejectCreate(w, r, http.StatusTooManyRequests, rateLimited{Error: "rate_limited", RetryAfter: retrySeconds(retry)})
		return
	}
	gate, err := s.globalGate(ctx, now)
	if err != nil {
		s.writeInternal(w, r, err)
		return
	}
	if gate.full {
		s.rejectCapped(w, r, now)
		return
	}
	if gate.powNeeded {
		err := s.powVerify(ctx, r.Header.Get(headerPoW), id, now)
		switch {
		case errors.Is(err, errPoWBusy):
			setRetryAfter(w, powBusyRetry)
			s.rejectCreate(w, r, http.StatusServiceUnavailable, errUnavailable)
			return
		case errors.Is(err, ErrPoW):
			s.rejectCreate(w, r, http.StatusForbidden, errPoWRequired)
			return
		case err != nil:
			s.writeInternal(w, r, err)
			return
		}
	}
	slot, reserved, err := s.reserveGlobalCreate(ctx, now)
	if err != nil {
		s.writeInternal(w, r, err)
		return
	}
	if !reserved {
		s.rejectCapped(w, r, now)
		return
	}
	stored := false
	defer func() {
		if !stored {
			s.releaseGlobalCreate(ctx, now)
		}
	}()
	if !gate.powNeeded && s.powNeededAfter(slot-1) {
		s.rejectCreate(w, r, http.StatusForbidden, errPoWRequired)
		return
	}
	if _, err := s.store.Get(ctx, id.String()); err == nil {
		s.rejectCreate(w, r, http.StatusConflict, errConflict)
		return
	} else if !errors.Is(err, store.ErrNotFound) {
		s.writeInternal(w, r, err)
		return
	}
	body := &trackingReader{r: http.MaxBytesReader(w, r.Body, h.Size)}
	var header [core.HeaderLen]byte
	if _, err := io.ReadFull(body, header[:]); err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	parsed, err := core.ParseHeader(header[:])
	if err != nil || int(parsed.KDF) != h.KDFID {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	err = s.blobs.Put(ctx, id.String(), io.MultiReader(bytes.NewReader(header[:]), body), h.Size)
	s.setWriteDeadline(w, time.Now().Add(writeTimeout))
	if err != nil {
		var tooLarge *http.MaxBytesError
		switch {
		case errors.Is(err, store.ErrConflict):
			s.rejectCreate(w, r, http.StatusConflict, errConflict)
		case errors.As(body.err, &tooLarge):
			writeError(w, http.StatusRequestEntityTooLarge, errTooLarge)
		case body.err != nil || errors.Is(err, io.ErrUnexpectedEOF):
			writeError(w, http.StatusBadRequest, errBadRequest)
		default:
			s.writeInternal(w, r, err)
		}
		return
	}
	readMAC, pepperID := s.ring.MAC(pepper.Read, req.readKey)
	manageMAC, _ := s.ring.MAC(pepper.Manage, req.manageKey)
	uploaded := s.clock()
	m := &store.Message{
		ID: id.String(), Version: parsed.Version, KDF: uint8(parsed.KDF), Flags: parsed.Flags, State: store.StateAvailable,
		CreatedAt: uploaded.UTC().Truncate(time.Minute), ExpiresAt: store.CeilMinute(uploaded.UTC().Add(time.Duration(h.TTLSeconds) * time.Second)), TTLSeconds: uint32(h.TTLSeconds),
		OpensMax: uint16(h.OpensMax), OpensRemaining: uint16(h.OpensMax), PepperID: pepperID,
		ReadMAC: readMAC[:], ManageMAC: manageMAC[:], Size: h.Size, BlobRef: id.String(),
	}
	if req.verifierKey != nil {
		verifierMAC, _ := s.ring.MAC(pepper.Verifier, req.verifierKey)
		m.VerifierMAC = verifierMAC[:]
	}
	if err := s.store.Create(ctx, m, s.storedCreateCounters(now)...); err != nil {
		if derr := s.blobs.Delete(context.WithoutCancel(ctx), id.String()); derr != nil {
			s.log.Error("create-cleanup", "err", ErrorText(derr))
		}
		if errors.Is(err, store.ErrConflict) {
			s.rejectCreate(w, r, http.StatusConflict, errConflict)
			return
		}
		s.writeInternal(w, r, err)
		return
	}
	stored = true
	writeJSON(w, http.StatusCreated, map[string]string{"id": id.String(), "expires_at": formatTime(m.ExpiresAt)})
}

func (s *Server) reserveDisk(size int64) (func(), bool) {
	if s.cfg.MinFreeBytes == 0 {
		return func() {}, true
	}
	free, err := s.freeSpace(s.cfg.DataDir)
	needed := uint64(s.cfg.MinFreeBytes) + uint64(s.diskReserved.Add(size))
	if err == nil && free >= needed {
		if s.diskLow.Swap(false) {
			s.log.Info("disk-free-again")
		}
		return func() { s.diskReserved.Add(-size) }, true
	}
	s.diskReserved.Add(-size)
	switch {
	case s.diskLow.Swap(true):
	case err != nil:
		s.log.Error("disk-free", "err", ErrorText(err))
	default:
		s.log.Error("disk-full")
	}
	return nil, false
}

func (s *Server) setCreateDeadlines(w http.ResponseWriter, bodyBy time.Time) {
	if err := http.NewResponseController(w).SetReadDeadline(bodyBy); err != nil {
		s.log.Error("read-deadline", "err", ErrorText(err))
	}
	s.setWriteDeadline(w, bodyBy.Add(writeTimeout))
}

func (s *Server) setWriteDeadline(w http.ResponseWriter, by time.Time) {
	if err := http.NewResponseController(w).SetWriteDeadline(by); err != nil {
		s.log.Error("write-deadline", "err", ErrorText(err))
	}
}

func (s *Server) rejectCapped(w http.ResponseWriter, r *http.Request, now time.Time) {
	retry := pendingRetry
	c, err := s.store.RateGet(r.Context(), globalStoredKey, dayWindow(now).start)
	switch {
	case err != nil:
		level, text := failure(r.Context(), err)
		s.log.Log(r.Context(), level, "global-cap", "err", text)
	case c.Count >= s.cfg.Rates.GlobalDailyCreates:
		retry = dayWindow(now).end.Sub(now) + time.Second
	}
	setRetryAfter(w, retry)
	s.rejectCreate(w, r, http.StatusServiceUnavailable, errUnavailable)
}

func (s *Server) allowRead(w http.ResponseWriter, r *http.Request) bool {
	return s.allowHourly(w, r, "read", s.cfg.Rates.ReadPerHour)
}

func (s *Server) allowHourly(w http.ResponseWriter, r *http.Request, kind string, limit int64) bool {
	if retry := s.limitHourly(s.clock(), s.clientAddr(r), kind, limit); retry > 0 {
		writeRateLimited(w, retry)
		return false
	}
	return true
}

func (s *Server) authorized(w http.ResponseWriter, r *http.Request, purpose pepper.Purpose) (access, bool) {
	if !emptyBody(w, r) {
		return access{}, false
	}
	a, ok, err := s.authorize(r.Context(), r, r.PathValue("id"), purpose, nil)
	if err != nil {
		s.writeInternal(w, r, err)
		return a, false
	}
	if !ok {
		writeNotFound(w)
		return a, false
	}
	return a, true
}

func (s *Server) authorizedLive(w http.ResponseWriter, r *http.Request, purpose pepper.Purpose) (*store.Message, access, bool) {
	a, ok := s.authorized(w, r, purpose)
	if !ok {
		return nil, a, false
	}
	if a.rec.Tombstone != nil {
		writeGone(w, string(a.rec.Tombstone.State))
		return nil, a, false
	}
	if now := s.clock(); ended(a.rec.Message, now) {
		writeGone(w, string(liveState(a.rec.Message, now)))
		return nil, a, false
	}
	return a.rec.Message, a, true
}

func emptyBody(w http.ResponseWriter, r *http.Request) bool {
	var one [1]byte
	if n, err := io.ReadFull(r.Body, one[:]); n != 0 || !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return false
	}
	return true
}

func validPathID(w http.ResponseWriter, r *http.Request) bool {
	if _, err := core.ParseID(r.PathValue("id")); err != nil {
		writeNotFound(w)
		return false
	}
	return true
}

func (s *Server) handleMetadata(w http.ResponseWriter, r *http.Request) {
	if !validPathID(w, r) || !s.allowRead(w, r) {
		return
	}
	a, ok := s.authorized(w, r, pepper.Read)
	if !ok {
		return
	}
	rec := a.rec
	if t := rec.Tombstone; t != nil {
		writeJSON(w, http.StatusOK, metadataResponse{State: string(t.State), FirstOpenedAt: formatOptTime(t.FirstOpenedAt), EndedAt: formatTime(t.EndedAt)})
		return
	}
	m := rec.Message
	writeJSON(w, http.StatusOK, metadataResponse{
		State: string(liveState(m, s.clock())), CreatedAt: formatTime(m.CreatedAt), ExpiresAt: formatTime(m.ExpiresAt),
		OpensMax: int(m.OpensMax), OpensRemaining: int(m.OpensRemaining), TTL: int(m.TTLSeconds),
		PassphraseRequired: m.PassphraseRequired(), KDFID: int(m.KDF), Size: m.Size, FirstOpenedAt: formatOptTime(m.FirstOpenedAt),
	})
}

func (s *Server) policyHeader(m *store.Message) string {
	data, _ := json.Marshal(policyHeader{OpensMax: int(m.OpensMax), TTL: int(m.TTLSeconds), KDFID: int(m.KDF), Flags: int(m.Flags)})
	return core.Encode(data)
}

func (s *Server) handleClaim(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !validPathID(w, r) || !s.allowRead(w, r) {
		return
	}
	release, ok := s.takeStream(w, r)
	if !ok {
		return
	}
	defer release()
	verifierHeader := r.Header.Get(headerVerifier)
	verifierSent := verifierHeader != ""
	verifier := core.Signature{PublicKey: dummyPublicKey}
	if verifierSent {
		var err error
		if verifier, err = core.ParseVerifierHeader(verifierHeader, [core.ChallengeLen]byte{}); err != nil {
			writeError(w, http.StatusBadRequest, errBadRequest)
			return
		}
	}
	m, a, ok := s.authorizedLive(w, r, pepper.Read)
	if !ok {
		return
	}
	id := a.id
	verifier.Challenge = a.sig.Challenge
	verifierKeyOK := core.ValidPublicKey(verifier.PublicKey[:])
	if !verifierKeyOK {
		verifier.PublicKey = dummyPublicKey
	}
	verifierSigned := verifier.Verify(core.PurposeVerifier, r.Method, r.URL.EscapedPath(), nil) && verifierKeyOK
	blob, size, ok := s.openBlob(w, r, m)
	if !ok {
		return
	}
	defer blob.Close()
	lease := core.NewSecret()
	leaseMAC, leasePepper := s.ring.MAC(pepper.Lease, lease[:])
	result, err := s.store.Claim(ctx, store.ClaimParams{
		ID: id.String(), Clock: s.clock, VerifierSent: verifierSent,
		VerifyMAC: func(pepperID byte, stored []byte) bool {
			macOK := s.ring.Verify(pepper.Verifier, verifier.PublicKey[:], stored, pepperID)
			return macOK && verifierSigned
		},
		LeaseMAC:      leaseMAC[:],
		LeasePepperID: leasePepper,
	})
	var goneErr *store.GoneError
	var passErr *store.PassphraseError
	switch {
	case errors.As(err, &goneErr):
		writeGone(w, string(goneErr.State))
		return
	case errors.As(err, &passErr):
		writeJSON(w, http.StatusForbidden, passphraseRejected{Error: "passphrase", RetryAfter: ceilSeconds(passErr.RetryAfter), AttemptsLeft: passErr.AttemptsLeft})
		return
	case errors.Is(err, store.ErrNotFound):
		writeNotFound(w)
		return
	case err != nil:
		s.writeInternal(w, r, err)
		return
	}
	if result.Message.OpensRemaining == 0 {
		msgID := id.String()
		s.afterFunc(result.Lease.ExpiresAt.Add(store.LeaseTimerSlack).Sub(s.clock()), func() {
			if _, err := store.Release(context.Background(), s.store, s.blobs, msgID, s.clock()); err != nil {
				s.log.Error("lease-timer", "err", ErrorText(err))
			}
		})
	}
	s.serveBlob(w, r, result.Message, core.Encode(lease[:]), blob, size)
}

func ceilSeconds(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	return int64((d + time.Second - 1) / time.Second)
}

func (s *Server) openBlob(w http.ResponseWriter, r *http.Request, m *store.Message) (io.ReadCloser, int64, bool) {
	if m.BlobRef == "" {
		writeGone(w, string(m.State))
		return nil, 0, false
	}
	f, size, err := s.blobs.Open(r.Context(), m.BlobRef)
	if errors.Is(err, store.ErrBlobMissing) {
		var state store.State
		if state, err = s.finishMissingBlob(r.Context(), m.ID); err == nil {
			writeGone(w, string(state))
			return nil, 0, false
		}
	}
	if err == nil && size != m.Size {
		f.Close()
		err = errBlobSize
	}
	if err != nil {
		s.writeInternal(w, r, err)
		return nil, 0, false
	}
	return f, size, true
}

func (s *Server) serveBlob(w http.ResponseWriter, r *http.Request, m *store.Message, lease string, f io.Reader, size int64) {
	s.setWriteDeadline(w, time.Now().Add(max(writeTimeout, store.TransferTime(size))))
	if lease != "" {
		w.Header().Set(headerLease, lease)
	}
	w.Header().Set(headerPolicy, s.policyHeader(m))
	w.Header().Set("Content-Type", octetStream)
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.WriteHeader(http.StatusOK)
	if n, err := io.Copy(w, f); (err != nil || n != size) && r.Context().Err() == nil {
		s.log.Warn("blob-stream")
	}
}

func (s *Server) finishMissingBlob(ctx context.Context, id string) (store.State, error) {
	rec, err := s.store.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if rec.Tombstone != nil {
		return rec.Tombstone.State, nil
	}
	now := s.clock()
	if !ended(rec.Message, now) {
		return "", store.ErrBlobMissing
	}
	state, err := store.EndMessage(ctx, s.store, s.blobs, id, store.StateExpired, now)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return s.endedState(ctx, id)
	case err != nil:
		s.log.Error("finish-ending", "err", ErrorText(err))
		return liveState(rec.Message, now), nil
	}
	return state, nil
}

func (s *Server) endedState(ctx context.Context, id string) (store.State, error) {
	rec, err := s.store.Get(ctx, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return store.StateDeleted, nil
	case err != nil:
		return "", err
	case rec.Tombstone != nil:
		return rec.Tombstone.State, nil
	}
	return liveState(rec.Message, s.clock()), nil
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
	if _, err := store.EndMessage(r.Context(), s.store, s.blobs, id, state, now); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.log.Error("finish-ending", "err", ErrorText(err))
	}
	writeGone(w, string(state))
}

func (s *Server) findLease(ctx context.Context, r *http.Request, a access, now time.Time) (store.Lease, bool, error) {
	raw, err := core.DecodeExact(r.Header.Get(headerLease), core.TokenLen)
	if err != nil {
		return store.Lease{}, false, nil
	}
	leases := a.leases
	if leases == nil {
		if leases, err = s.store.Leases(ctx, a.id.String()); err != nil {
			return store.Lease{}, false, err
		}
	}
	for _, l := range leases {
		if s.ring.Verify(pepper.Lease, raw, l.MAC, l.PepperID) {
			return l, l.Active(now), nil
		}
	}
	return store.Lease{}, false, nil
}

type leaseAccess struct {
	msg   *store.Message
	id    string
	lease store.Lease
	now   time.Time
}

func (s *Server) activeLease(w http.ResponseWriter, r *http.Request) (leaseAccess, bool) {
	m, a, ok := s.authorizedLive(w, r, pepper.Read)
	if !ok {
		return leaseAccess{}, false
	}
	la := leaseAccess{msg: m, id: a.id.String(), now: s.clock()}
	lease, active, err := s.findLease(r.Context(), r, a, la.now)
	switch {
	case err != nil:
		s.writeInternal(w, r, err)
		return la, false
	case !active:
		s.writeCurrentGone(w, r, la.id)
		return la, false
	}
	la.lease = lease
	return la, true
}

func (s *Server) handleBlob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !validPathID(w, r) || !s.allowRead(w, r) {
		return
	}
	release, ok := s.takeStream(w, r)
	if !ok {
		return
	}
	defer release()
	la, ok := s.activeLease(w, r)
	if !ok {
		return
	}
	m, id, lease, now := la.msg, la.id, la.lease, la.now
	if err := s.store.RecordDownload(ctx, lease, now); err != nil {
		var goneErr *store.GoneError
		switch {
		case errors.As(err, &goneErr):
			s.finishGone(w, r, id, goneErr.State, now)
		case errors.Is(err, store.ErrLeaseLimit):
			writeRateLimited(w, lease.ExpiresAt.Sub(now))
		case errors.Is(err, store.ErrLeaseInvalid):
			s.writeCurrentGone(w, r, id)
		default:
			s.writeInternal(w, r, err)
		}
		return
	}
	blob, size, ok := s.openBlob(w, r, m)
	if !ok {
		return
	}
	defer blob.Close()
	s.serveBlob(w, r, m, "", blob, size)
}

func (s *Server) handleAck(w http.ResponseWriter, r *http.Request) {
	la, ok := s.activeLease(w, r)
	if !ok {
		return
	}
	var goneErr *store.GoneError
	switch err := store.AckLease(r.Context(), s.store, s.blobs, la.lease, la.now); {
	case errors.As(err, &goneErr):
		s.finishGone(w, r, la.id, goneErr.State, la.now)
		return
	case errors.Is(err, store.ErrLeaseInvalid):
		s.writeCurrentGone(w, r, la.id)
		return
	case err != nil:
		s.writeInternal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) endWith(w http.ResponseWriter, r *http.Request, purpose pepper.Purpose, state store.State) {
	ctx := r.Context()
	_, a, ok := s.authorizedLive(w, r, purpose)
	if !ok {
		return
	}
	id := a.id
	final, err := store.EndMessage(ctx, s.store, s.blobs, id.String(), state, s.clock())
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.writeCurrentGone(w, r, id.String())
	case err != nil:
		s.writeInternal(w, r, err)
	case final != state:
		writeGone(w, string(final))
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleBurn(w http.ResponseWriter, r *http.Request) {
	s.endWith(w, r, pepper.Read, store.StateDeleted)
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	s.endWith(w, r, pepper.Manage, store.StateCancelled)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorized(w, r, pepper.Manage)
	if !ok {
		return
	}
	rec := a.rec
	if t := rec.Tombstone; t != nil {
		writeJSON(w, http.StatusOK, statusResponse{State: string(t.State), FirstOpenedAt: formatOptTime(t.FirstOpenedAt), EndedAt: formatTime(t.EndedAt)})
		return
	}
	m := rec.Message
	writeJSON(w, http.StatusOK, statusResponse{
		State: string(liveState(m, s.clock())), OpensUsed: int(m.OpensMax) - int(m.OpensRemaining), OpensMax: int(m.OpensMax),
		CreatedAt: formatTime(m.CreatedAt), ExpiresAt: formatTime(m.ExpiresAt), FirstOpenedAt: formatOptTime(m.FirstOpenedAt),
		FailedPassphraseAttempts: m.FailCountTotal,
	})
}

var errBlobSize = errors.New("server: blob size differs from the stored message size")

package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/store"
)

const (
	headerCreate         = "X-Nylzo-Create"
	headerPoW            = "X-Nylzo-Pow"
	headerLease          = "X-Nylzo-Lease"
	headerPolicy         = "X-Nylzo-Policy"
	maxCreateHeader      = 1024
	maxJSONBody          = 4096
	timeFormat           = time.RFC3339
	octetStream          = "application/octet-stream"
	abuseReasonRawBytes  = 1000
	abuseReasonJSONBytes = 1200
	abuseFileLimit       = 16 << 20
	abuseRetention       = MaxTTL + store.TombstoneTTL
	abusePerRange        = 3
	abuseDecodePasses    = 3
	maxJSONEscape        = len(`\u0000`)
)

type createHeader struct {
	ID           string  `json:"id"`
	IDToken      string  `json:"id_token"`
	ReadHash     string  `json:"read_hash"`
	VerifierHash *string `json:"verifier_hash"`
	ManageHash   string  `json:"manage_hash"`
	KDFID        int     `json:"kdf_id"`
	OpensMax     int     `json:"opens_max"`
	TTLSeconds   int64   `json:"ttl_s"`
	Size         int64   `json:"size"`
}

var createHeaderKeys = map[string]bool{"id": true, "id_token": true, "read_hash": true, "verifier_hash": true, "manage_hash": true, "kdf_id": true, "opens_max": true, "ttl_s": true, "size": true}

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

func bearer(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if len(auth) > 7 && strings.EqualFold(auth[:7], "Bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return ""
}

func (s *Server) internal(w http.ResponseWriter, r *http.Request, err error) {
	if r.Context().Err() != nil && errors.Is(err, context.Canceled) {
		s.log.Info("handler", "route", r.Pattern, "err", "client went away")
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	s.log.Error("handler", "route", r.Pattern, "err", err.Error())
	writeError(w, http.StatusInternalServerError, errInternal)
}

type access struct {
	rec    store.Record
	id     core.ID
	leases []store.Lease
}

func (s *Server) authorize(ctx context.Context, idStr, token string, purpose pepper.Purpose) (access, bool, error) {
	id, idErr := core.ParseID(idStr)
	raw, tokErr := core.DecodeExact(token, core.TokenLen)
	if idErr != nil || tokErr != nil {
		s.ring.Verify(purpose, s.ring.Dummy(), s.ring.Dummy(), s.ring.CurrentID())
		return access{id: id}, false, nil
	}
	now := s.clock()
	rec, err := s.store.Get(ctx, id.String())
	if errors.Is(err, store.ErrNotFound) {
		h := core.Hash(raw)
		s.ring.Verify(purpose, h[:], s.ring.Dummy(), s.ring.CurrentID())
		return access{id: id}, false, nil
	}
	if err != nil {
		return access{id: id}, false, err
	}
	a := access{rec: rec, id: id}
	if fresh, leases, ok := s.settle(ctx, id.String(), rec.Message, now); ok {
		a.rec = fresh
	} else {
		a.leases = leases
	}
	mac := a.rec.ReadMAC()
	if purpose == pepper.Manage {
		mac = a.rec.ManageMAC()
	}
	h := core.Hash(raw)
	ok := s.ring.Verify(purpose, h[:], mac, a.rec.PepperID())
	return a, ok, nil
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
			s.log.Error("settle", "err", lerr.Error())
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
		s.log.Error("settle", "err", err.Error())
		return store.Record{}, nil, false
	}
	rec, err := s.store.Get(ctx, id)
	if err != nil {
		s.log.Error("settle", "err", err.Error())
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
	l := s.cfg.Limits
	resp := instanceResponse{
		Limits:   instanceLimits{MaxFileBytes: l.MaxFileBytes, MaxTextBytes: core.MaxTextBytes, MaxObjectBytes: l.MaxObjectBytes, MaxTTL: int64(l.MaxTTL / time.Second), DefaultTTL: int64(l.DefaultTTL / time.Second), MaxOpens: l.MaxOpens, DefaultKDF: int(l.DefaultKDF)},
		PoW:      instancePoW{Difficulty: s.cfg.PoWDifficulty},
		Versions: instanceVersions{API: 1, Envelope: core.Version, Fragment: core.FragmentVersion, Server: s.cfg.Version},
	}
	now := s.clock()
	resp.Create = s.issueID(now)
	gate, err := s.globalGate(r.Context(), now)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if gate.powNeeded {
		resp.PoW.Required = true
		resp.PoW.Challenge = s.powChallenge(now)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) parseCreateHeader(r *http.Request) (createHeader, core.ID, []byte, []byte, []byte, bool) {
	var h createHeader
	raw := r.Header.Get(headerCreate)
	if raw == "" || len(raw) > maxCreateHeader {
		return h, core.ID{}, nil, nil, nil, false
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	if err != nil {
		return h, core.ID{}, nil, nil, nil, false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return h, core.ID{}, nil, nil, nil, false
	}
	for key := range fields {
		if !createHeaderKeys[key] {
			return h, core.ID{}, nil, nil, nil, false
		}
	}
	if err := json.Unmarshal(data, &h); err != nil {
		return h, core.ID{}, nil, nil, nil, false
	}
	id, err := core.ParseID(h.ID)
	if err != nil {
		return h, core.ID{}, nil, nil, nil, false
	}
	readHash, err := core.DecodeExact(h.ReadHash, 32)
	if err != nil {
		return h, id, nil, nil, nil, false
	}
	manageHash, err := core.DecodeExact(h.ManageHash, 32)
	if err != nil {
		return h, id, nil, nil, nil, false
	}
	kdf := core.KDF(h.KDFID)
	if h.KDFID < 0 || h.KDFID > 255 || !kdf.Valid() {
		return h, id, nil, nil, nil, false
	}
	var verifierHash []byte
	if kdf.RequiresPassphrase() != (h.VerifierHash != nil) {
		return h, id, nil, nil, nil, false
	}
	if h.VerifierHash != nil {
		if verifierHash, err = core.DecodeExact(*h.VerifierHash, 32); err != nil {
			return h, id, nil, nil, nil, false
		}
	}
	l := s.cfg.Limits
	if h.OpensMax < 1 || h.OpensMax > l.MaxOpens || h.TTLSeconds < core.MinTTLSeconds || h.TTLSeconds > int64(l.MaxTTL/time.Second) || h.Size < core.HeaderLen+core.TagLen+core.MetaLenPrefix {
		return h, id, nil, nil, nil, false
	}
	return h, id, readHash, verifierHash, manageHash, true
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
	h, id, readHash, verifierHash, manageHash, ok := s.parseCreateHeader(r)
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
	http.NewResponseController(w).SetReadDeadline(time.Now().Add(max(readTimeout, store.TransferTime(h.Size))))
	now := s.clock()
	retry, err := s.limitCreate(ctx, now, s.clientAddr(r), h.Size)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if retry > 0 {
		setRetryAfter(w, retry)
		s.rejectCreate(w, r, http.StatusTooManyRequests, rateLimited{Error: "rate_limited", RetryAfter: retrySeconds(retry)})
		return
	}
	gate, err := s.globalGate(ctx, now)
	if err != nil {
		s.internal(w, r, err)
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
			s.internal(w, r, err)
			return
		}
	}
	slot, reserved, err := s.reserveGlobalCreate(ctx, now)
	if err != nil {
		s.internal(w, r, err)
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
		s.internal(w, r, err)
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
	if err := s.blobs.Put(ctx, id.String(), io.MultiReader(bytes.NewReader(header[:]), body), h.Size); err != nil {
		var tooLarge *http.MaxBytesError
		switch {
		case errors.Is(err, store.ErrConflict):
			s.rejectCreate(w, r, http.StatusConflict, errConflict)
		case errors.As(body.err, &tooLarge):
			writeError(w, http.StatusRequestEntityTooLarge, errTooLarge)
		case body.err != nil || errors.Is(err, io.ErrUnexpectedEOF):
			writeError(w, http.StatusBadRequest, errBadRequest)
		default:
			s.internal(w, r, err)
		}
		return
	}
	readMAC, pepperID := s.ring.MAC(pepper.Read, readHash)
	manageMAC, _ := s.ring.MAC(pepper.Manage, manageHash)
	uploaded := s.clock()
	m := &store.Message{
		ID: id.String(), Version: parsed.Version, KDF: uint8(parsed.KDF), Flags: parsed.Flags, State: store.StateAvailable,
		CreatedAt: uploaded.UTC().Truncate(time.Minute), ExpiresAt: store.CeilMinute(uploaded.UTC().Add(time.Duration(h.TTLSeconds) * time.Second)), TTLSeconds: uint32(h.TTLSeconds),
		OpensMax: uint16(h.OpensMax), OpensRemaining: uint16(h.OpensMax), PepperID: pepperID,
		ReadMAC: readMAC[:], ManageMAC: manageMAC[:], Size: h.Size, BlobRef: id.String(),
	}
	if verifierHash != nil {
		verifierMAC, _ := s.ring.MAC(pepper.Verifier, verifierHash)
		m.VerifierMAC = verifierMAC[:]
	}
	if err := s.store.Create(ctx, m, s.storedCreateCounters(now)...); err != nil {
		if derr := s.blobs.Delete(context.WithoutCancel(ctx), id.String()); derr != nil {
			s.log.Error("create-cleanup", "err", derr.Error())
		}
		if errors.Is(err, store.ErrConflict) {
			s.rejectCreate(w, r, http.StatusConflict, errConflict)
			return
		}
		s.internal(w, r, err)
		return
	}
	stored = true
	writeJSON(w, http.StatusCreated, map[string]string{"id": id.String(), "expires_at": formatTime(m.ExpiresAt)})
}

func (s *Server) rejectCapped(w http.ResponseWriter, r *http.Request, now time.Time) {
	retry := pendingRetry
	if c, err := s.store.RateGet(r.Context(), globalStoredKey, dayWindow(now).start); err == nil && c.Count >= s.cfg.Rates.GlobalDailyCreates {
		retry = dayWindow(now).end.Sub(now) + time.Second
	}
	setRetryAfter(w, retry)
	s.rejectCreate(w, r, http.StatusServiceUnavailable, errUnavailable)
}

func (s *Server) allowRead(w http.ResponseWriter, r *http.Request) bool {
	return s.allowHourly(w, r, "read", s.cfg.Rates.ReadPerHour)
}

func (s *Server) allowHourly(w http.ResponseWriter, r *http.Request, kind string, limit int64) bool {
	retry, err := s.limitHourly(r.Context(), s.clock(), s.clientAddr(r), kind, limit)
	if err != nil {
		s.internal(w, r, err)
		return false
	}
	if retry > 0 {
		writeRateLimited(w, retry)
		return false
	}
	return true
}

func (s *Server) authorized(w http.ResponseWriter, r *http.Request, purpose pepper.Purpose) (access, bool) {
	a, ok, err := s.authorize(r.Context(), r.PathValue("id"), bearer(r), purpose)
	if err != nil {
		s.internal(w, r, err)
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

type claimBody struct {
	Verifier string `json:"verifier"`
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
	m, a, ok := s.authorizedLive(w, r, pepper.Read)
	if !ok {
		return
	}
	id := a.id
	var verifier []byte
	var err error
	verifierSent := false
	var body claimBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody)).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	if body.Verifier != "" {
		if verifier, err = core.DecodeExact(body.Verifier, core.TokenLen); err != nil {
			writeError(w, http.StatusBadRequest, errBadRequest)
			return
		}
		verifierSent = true
	}
	blob, size, ok := s.openBlob(w, r, m)
	if !ok {
		return
	}
	defer blob.Close()
	lease := core.NewSecret()
	leaseMAC, leasePepper := s.ring.MAC(pepper.Lease, lease[:])
	var verifierHash [32]byte
	if verifierSent {
		verifierHash = core.Hash(verifier)
	}
	result, err := s.store.Claim(ctx, store.ClaimParams{
		ID: id.String(), Clock: s.clock, VerifierSent: verifierSent,
		VerifyMAC: func(pepperID byte, stored []byte) bool {
			return s.ring.Verify(pepper.Verifier, verifierHash[:], stored, pepperID)
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
		s.internal(w, r, err)
		return
	}
	if result.Message.OpensRemaining == 0 {
		msgID := id.String()
		s.afterFunc(result.Lease.ExpiresAt.Add(store.LeaseTimerSlack).Sub(s.clock()), func() {
			if _, err := store.Release(context.Background(), s.store, s.blobs, msgID, s.clock()); err != nil {
				s.log.Error("lease-timer", "err", err.Error())
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
		s.internal(w, r, err)
		return nil, 0, false
	}
	return f, size, true
}

func (s *Server) serveBlob(w http.ResponseWriter, r *http.Request, m *store.Message, lease string, f io.Reader, size int64) {
	http.NewResponseController(w).SetWriteDeadline(time.Now().Add(max(writeTimeout, store.TransferTime(size))))
	if lease != "" {
		w.Header().Set(headerLease, lease)
	}
	w.Header().Set(headerPolicy, s.policyHeader(m))
	w.Header().Set("Content-Type", octetStream)
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.WriteHeader(http.StatusOK)
	if n, err := io.Copy(w, f); (err != nil || n != size) && r.Context().Err() == nil {
		s.log.Warn("blob-stream", "sent", n, "size", size)
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
		s.log.Error("finish-ending", "err", err.Error())
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
		s.internal(w, r, err)
		return
	}
	writeGone(w, string(state))
}

func (s *Server) finishGone(w http.ResponseWriter, r *http.Request, id string, state store.State, now time.Time) {
	if _, err := store.EndMessage(r.Context(), s.store, s.blobs, id, state, now); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.log.Error("finish-ending", "err", err.Error())
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

func (s *Server) handleBlob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !validPathID(w, r) || !s.allowRead(w, r) {
		return
	}
	m, a, ok := s.authorizedLive(w, r, pepper.Read)
	if !ok {
		return
	}
	id := a.id
	now := s.clock()
	lease, active, err := s.findLease(ctx, r, a, now)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if !active {
		s.writeCurrentGone(w, r, id.String())
		return
	}
	if err := s.store.RecordDownload(ctx, lease, now); err != nil {
		var goneErr *store.GoneError
		switch {
		case errors.As(err, &goneErr):
			s.finishGone(w, r, id.String(), goneErr.State, now)
		case errors.Is(err, store.ErrLeaseLimit):
			writeRateLimited(w, lease.ExpiresAt.Sub(now))
		case errors.Is(err, store.ErrLeaseInvalid):
			s.writeCurrentGone(w, r, id.String())
		default:
			s.internal(w, r, err)
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
	ctx := r.Context()
	_, a, ok := s.authorizedLive(w, r, pepper.Read)
	if !ok {
		return
	}
	id := a.id
	now := s.clock()
	lease, active, err := s.findLease(ctx, r, a, now)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if !active {
		s.writeCurrentGone(w, r, id.String())
		return
	}
	var goneErr *store.GoneError
	switch err := store.AckLease(ctx, s.store, s.blobs, lease, now); {
	case errors.As(err, &goneErr):
		s.finishGone(w, r, id.String(), goneErr.State, now)
		return
	case err != nil:
		s.internal(w, r, err)
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
		if gone, gerr := s.endedState(ctx, id.String()); gerr != nil {
			s.internal(w, r, gerr)
		} else {
			writeGone(w, string(gone))
		}
	case err != nil:
		s.internal(w, r, err)
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
		State: string(liveState(m, s.clock())), OpensUsed: int(m.OpensMax - m.OpensRemaining), OpensMax: int(m.OpensMax),
		CreatedAt: formatTime(m.CreatedAt), ExpiresAt: formatTime(m.ExpiresAt), FirstOpenedAt: formatOptTime(m.FirstOpenedAt),
		FailedPassphraseAttempts: m.FailCountTotal,
	})
}

type abuseBody struct {
	ID        string `json:"id"`
	ReadToken string `json:"read_token"`
	Reason    string `json:"reason"`
}

func (s *Server) handleAbuse(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	var body abuseBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	if _, err := core.ParseID(body.ID); err != nil {
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
		return
	}
	if !s.allowHourly(w, r, "abuse", s.cfg.Rates.AbusePerHour) {
		return
	}
	now := s.clock()
	a, ok, err := s.authorize(ctx, body.ID, body.ReadToken, pepper.Read)
	id := a.id
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if ok {
		err := s.recordAbuse(ctx, now, id.String(), body.Reason)
		if errors.Is(err, errAbuseFull) {
			s.log.Error("abuse-file-full")
			setRetryAfter(w, abuseFullRetry)
			writeError(w, http.StatusServiceUnavailable, errUnavailable)
			return
		}
		if err != nil {
			s.log.Error("abuse-record", "err", err.Error())
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

var (
	percentEscape = regexp.MustCompile(`%[0-9A-Fa-f]{2}`)
	linkFragment  = regexp.MustCompile(`#\S*`)
	tokenRun      = regexp.MustCompile(fmt.Sprintf(`[A-Za-z0-9_-]{%d,}`, base64.RawURLEncoding.EncodedLen(core.IDLen)))
	errAbuseFull  = errors.New("server: abuse report file reached its size limit")
	errBlobSize   = errors.New("server: blob size differs from the stored message size")
)

func percentDecode(text string) string {
	for range abuseDecodePasses {
		decoded := percentEscape.ReplaceAllStringFunc(text, func(esc string) string {
			b, _ := hex.DecodeString(esc[1:])
			return string(b)
		})
		if decoded == text {
			break
		}
		text = decoded
	}
	return strings.ToValidUTF8(text, "")
}

func visibleRune(r rune) rune {
	switch {
	case unicode.Is(unicode.Cf, r):
		return -1
	case unicode.IsControl(r):
		return ' '
	}
	return r
}

func redactReason(reason string) string {
	reason = strings.Map(visibleRune, percentDecode(reason))
	reason = linkFragment.ReplaceAllString(reason, "[redacted]")
	reason = tokenRun.ReplaceAllString(reason, "[redacted]")
	if len(reason) > abuseReasonRawBytes {
		reason = strings.ToValidUTF8(reason[:abuseReasonRawBytes], "")
	}
	for {
		encoded, _ := json.Marshal(reason)
		if len(encoded) <= abuseReasonJSONBytes {
			return reason
		}
		runes := []rune(reason)
		reason = string(runes[:len(runes)-max(1, (len(encoded)-abuseReasonJSONBytes)/maxJSONEscape)])
	}
}

func (s *Server) recordAbuse(ctx context.Context, now time.Time, id string, reason string) error {
	d := dayWindow(now)
	key := "abuse:id:" + id
	_, ok, err := s.store.RateReserve(context.WithoutCancel(ctx), store.RateHit{Key: key, WindowStart: d.start, PurgeAt: d.purgeAt(), Limit: abusePerRange})
	if err != nil || !ok {
		return err
	}
	if err := s.appendAbuse(now, id, reason); err != nil {
		if rerr := s.store.RateRelease(context.WithoutCancel(ctx), key, d.start); rerr != nil {
			s.log.Error("abuse-release", "err", rerr.Error())
		}
		return err
	}
	return nil
}

func (s *Server) appendAbuse(now time.Time, id, reason string) error {
	line, err := json.Marshal(map[string]string{"date": reportDate(now), "id": id, "reason": redactReason(reason)})
	if err != nil {
		return err
	}
	s.abuseMu.Lock()
	defer s.abuseMu.Unlock()
	if err := s.pruneAbuseLocked(now); err != nil {
		return err
	}
	f, err := os.OpenFile(s.abusePath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size()+int64(len(line))+1 > abuseFileLimit {
		return errAbuseFull
	}
	_, err = f.Write(append(line, '\n'))
	return err
}

func reportDate(t time.Time) string {
	return t.UTC().Format(time.DateOnly)
}

func (s *Server) pruneAbuse(now time.Time) error {
	s.abuseMu.Lock()
	defer s.abuseMu.Unlock()
	return s.pruneAbuseLocked(now)
}

func (s *Server) pruneAbuseLocked(now time.Time) error {
	data, err := os.ReadFile(s.abusePath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	cutoff := reportDate(now.Add(-abuseRetention))
	keep := data
	for len(keep) > 0 {
		line, rest, _ := bytes.Cut(keep, []byte("\n"))
		var r struct {
			Date string `json:"date"`
			Time string `json:"time"`
		}
		if json.Unmarshal(line, &r) == nil && max(r.Date, r.Time[:min(len(r.Time), len(time.DateOnly))]) >= cutoff {
			break
		}
		keep = rest
	}
	if len(keep) == len(data) {
		return nil
	}
	f, err := os.CreateTemp(filepath.Dir(s.abusePath), ".abuse-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(keep); err != nil {
		f.Close()
		return err
	}
	if err := errors.Join(f.Sync(), f.Close()); err != nil {
		return err
	}
	return os.Rename(f.Name(), s.abusePath)
}

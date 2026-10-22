package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/store"
)

const (
	headerCreate     = "X-Nylzo-Create"
	headerPoW        = "X-Nylzo-Pow"
	headerLease      = "X-Nylzo-Lease"
	headerPolicy     = "X-Nylzo-Policy"
	maxCreateHeader  = 1024
	maxJSONBody      = 4096
	constantBucket   = "nylzo/v1/no-ip-scope"
	timeFormat       = time.RFC3339
	octetStream      = "application/octet-stream"
	abuseReasonLimit = 1000
)

type createHeader struct {
	ID           string  `json:"id"`
	ReadHash     string  `json:"read_hash"`
	VerifierHash *string `json:"verifier_hash"`
	ManageHash   string  `json:"manage_hash"`
	KDFID        int     `json:"kdf_id"`
	OpensMax     int     `json:"opens_max"`
	TTLSeconds   int64   `json:"ttl_s"`
	Size         int64   `json:"size"`
}

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
	Versions instanceVersions `json:"versions"`
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
	s.log.Error("handler", "route", r.Pattern, "err", err.Error())
	writeError(w, http.StatusInternalServerError, errInternal)
}

func (s *Server) authorize(ctx context.Context, idStr, token string, purpose pepper.Purpose) (store.Record, core.ID, bool, error) {
	id, idErr := core.ParseID(idStr)
	raw, tokErr := core.DecodeExact(token, core.TokenLen)
	if idErr != nil || tokErr != nil {
		s.ring.Verify(purpose, s.ring.Dummy(), s.ring.Dummy(), s.ring.CurrentID())
		return store.Record{}, id, false, nil
	}
	now := s.clock()
	rec, err := s.store.Get(ctx, id.String())
	if errors.Is(err, store.ErrNotFound) {
		h := core.Hash(raw)
		s.ring.Verify(purpose, h[:], s.ring.Dummy(), s.ring.CurrentID())
		return store.Record{}, id, false, nil
	}
	if err != nil {
		return store.Record{}, id, false, err
	}
	if rec.Message != nil && !now.Before(rec.Message.ExpiresAt) {
		if err := store.EndMessage(ctx, s.store, s.blobs, id.String(), store.StateExpired, now); err != nil && !errors.Is(err, store.ErrNotFound) {
			return store.Record{}, id, false, err
		}
		if rec, err = s.store.Get(ctx, id.String()); err != nil {
			return store.Record{}, id, false, err
		}
	} else if rec.Message != nil && rec.Message.OpensRemaining == 0 {
		released, err := store.Release(ctx, s.store, s.blobs, id.String(), now)
		if err != nil {
			return store.Record{}, id, false, err
		}
		if released {
			if rec, err = s.store.Get(ctx, id.String()); err != nil {
				return store.Record{}, id, false, err
			}
		}
	}
	mac := rec.ReadMAC()
	if purpose == pepper.Manage {
		mac = rec.ManageMAC()
	}
	h := core.Hash(raw)
	ok := s.ring.Verify(purpose, h[:], mac, rec.PepperID())
	return rec, id, ok, nil
}

func (s *Server) handleInstance(w http.ResponseWriter, r *http.Request) {
	l := s.cfg.Limits
	resp := instanceResponse{
		Limits:   instanceLimits{MaxFileBytes: l.MaxFileBytes, MaxTextBytes: l.MaxTextBytes, MaxObjectBytes: l.MaxObjectBytes, MaxTTL: int64(l.MaxTTL / time.Second), DefaultTTL: int64(l.DefaultTTL / time.Second), MaxOpens: l.MaxOpens, DefaultKDF: int(l.DefaultKDF)},
		PoW:      instancePoW{Required: s.powRequired(), Difficulty: s.cfg.PoWDifficulty},
		Versions: instanceVersions{API: 1, Envelope: core.Version, Fragment: core.FragmentVersion, Server: s.cfg.Version},
	}
	if resp.PoW.Required {
		resp.PoW.Challenge = s.powChallenge(s.clock())
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
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&h); err != nil || dec.More() {
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

func (s *Server) rejectCreate(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Connection", "close")
	writeJSON(w, status, v)
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	h, id, readHash, verifierHash, manageHash, ok := s.parseCreateHeader(r)
	if !ok {
		s.rejectCreate(w, http.StatusBadRequest, errBadRequest)
		return
	}
	if r.ContentLength < 0 || r.Header.Get("Content-Length") == "" {
		s.rejectCreate(w, http.StatusLengthRequired, errLengthRequired)
		return
	}
	if r.ContentLength > s.cfg.Limits.MaxObjectBytes || h.Size > s.cfg.Limits.MaxObjectBytes {
		s.rejectCreate(w, http.StatusRequestEntityTooLarge, errTooLarge)
		return
	}
	if r.ContentLength != h.Size {
		s.rejectCreate(w, http.StatusBadRequest, errBadRequest)
		return
	}
	now := s.clock()
	addr := s.clientAddr(r)
	verdict, err := s.limitCreate(ctx, now, addr, h.Size)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if verdict.retry > 0 {
		retry := int64(verdict.retry/time.Second) + 1
		w.Header().Set("Retry-After", strconv.FormatInt(retry, 10))
		s.rejectCreate(w, http.StatusTooManyRequests, rateLimited{Error: "rate_limited", RetryAfter: retry})
		return
	}
	if verdict.capped {
		w.Header().Set("Retry-After", strconv.FormatInt(int64(dayWindow(now).end.Sub(now)/time.Second)+1, 10))
		s.rejectCreate(w, http.StatusServiceUnavailable, errUnavailable)
		return
	}
	if verdict.powNeeded {
		err := s.powVerify(ctx, r.Header.Get(headerPoW), id, now)
		if errors.Is(err, errPoWBusy) {
			w.Header().Set("Retry-After", "5")
			s.rejectCreate(w, http.StatusServiceUnavailable, errUnavailable)
			return
		}
		if err != nil {
			s.rejectCreate(w, http.StatusForbidden, errPoWRequired)
			return
		}
	}
	if _, err := s.store.Get(ctx, id.String()); err == nil {
		s.rejectCreate(w, http.StatusConflict, errConflict)
		return
	} else if !errors.Is(err, store.ErrNotFound) {
		s.internal(w, r, err)
		return
	}
	body := http.MaxBytesReader(w, r.Body, h.Size)
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
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, errTooLarge)
			return
		}
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	readMAC, pepperID := s.ring.MAC(pepper.Read, readHash)
	manageMAC, _ := s.ring.MAC(pepper.Manage, manageHash)
	m := &store.Message{
		ID: id.String(), Version: parsed.Version, KDF: uint8(parsed.KDF), Flags: parsed.Flags, State: store.StateAvailable,
		CreatedAt: now, ExpiresAt: now.Add(time.Duration(h.TTLSeconds) * time.Second), TTLSeconds: uint32(h.TTLSeconds),
		OpensMax: uint16(h.OpensMax), OpensRemaining: uint16(h.OpensMax), PepperID: pepperID,
		ReadMAC: readMAC[:], ManageMAC: manageMAC[:], Size: h.Size, BlobRef: id.String(),
	}
	if verifierHash != nil {
		verifierMAC, _ := s.ring.MAC(pepper.Verifier, verifierHash)
		m.VerifierMAC = verifierMAC[:]
	}
	if err := s.store.Create(ctx, m); err != nil {
		s.blobs.Delete(ctx, id.String())
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, errConflict)
			return
		}
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id.String(), "expires_at": formatTime(m.ExpiresAt)})
}

func (s *Server) readLimited(w http.ResponseWriter, r *http.Request) bool {
	retry, err := s.limitRead(r.Context(), s.clock(), s.clientAddr(r))
	if err != nil {
		s.internal(w, r, err)
		return false
	}
	if retry > 0 {
		writeRateLimited(w, int64(retry/time.Second)+1)
		return false
	}
	return true
}

func (s *Server) handleMetadata(w http.ResponseWriter, r *http.Request) {
	if !s.readLimited(w, r) {
		return
	}
	rec, _, ok, err := s.authorize(r.Context(), r.PathValue("id"), bearer(r), pepper.Read)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if !ok {
		writeNotFound(w)
		return
	}
	if t := rec.Tombstone; t != nil {
		writeJSON(w, http.StatusOK, metadataResponse{State: string(t.State), FirstOpenedAt: formatOptTime(t.FirstOpenedAt), EndedAt: formatTime(t.EndedAt)})
		return
	}
	m := rec.Message
	writeJSON(w, http.StatusOK, metadataResponse{
		State: string(m.State), CreatedAt: formatTime(m.CreatedAt), ExpiresAt: formatTime(m.ExpiresAt),
		OpensMax: int(m.OpensMax), OpensRemaining: int(m.OpensRemaining), TTL: int(m.TTLSeconds),
		PassphraseRequired: m.PassphraseRequired(), KDFID: int(m.KDF), Size: m.Size, FirstOpenedAt: formatOptTime(m.FirstOpenedAt),
	})
}

type claimBody struct {
	Verifier string `json:"verifier"`
}

func (s *Server) passphraseBucket(now time.Time, r *http.Request) []byte {
	if !s.cfg.PassphraseIPScope {
		h := core.Hash([]byte(constantBucket))
		return h[:3]
	}
	b := s.ring.Bucket(now, s.clientAddr(r), false)
	return b[:]
}

func (s *Server) policyHeader(m *store.Message) string {
	data, _ := json.Marshal(policyHeader{OpensMax: int(m.OpensMax), TTL: int(m.TTLSeconds), KDFID: int(m.KDF), Flags: int(m.Flags)})
	return core.Encode(data)
}

func (s *Server) handleClaim(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !s.readLimited(w, r) {
		return
	}
	rec, id, ok, err := s.authorize(ctx, r.PathValue("id"), bearer(r), pepper.Read)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if !ok {
		writeNotFound(w)
		return
	}
	if rec.Tombstone != nil {
		writeGone(w, string(rec.Tombstone.State))
		return
	}
	var verifier []byte
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
	now := s.clock()
	lease := core.NewSecret()
	leaseMAC, leasePepper := s.ring.MAC(pepper.Lease, lease[:])
	var verifierHash [32]byte
	if verifierSent {
		verifierHash = core.Hash(verifier)
	}
	result, err := s.store.Claim(ctx, store.ClaimParams{
		ID: id.String(), Now: now, Bucket: s.passphraseBucket(now, r), VerifierSent: verifierSent,
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
		s.afterFunc(result.Lease.ExpiresAt.Add(store.LeaseTimerSlack).Sub(now), func() {
			if _, err := store.Release(context.Background(), s.store, s.blobs, msgID, s.clock()); err != nil {
				s.log.Error("lease-timer", "err", err.Error())
			}
		})
	}
	w.Header().Set(headerLease, core.Encode(lease[:]))
	s.serveBlob(w, r, result.Message)
}

func ceilSeconds(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	return int64((d + time.Second - 1) / time.Second)
}

func (s *Server) serveBlob(w http.ResponseWriter, r *http.Request, m *store.Message) {
	if m.BlobRef == "" {
		writeGone(w, string(m.State))
		return
	}
	f, size, err := s.blobs.Open(r.Context(), m.BlobRef)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	defer f.Close()
	w.Header().Set(headerPolicy, s.policyHeader(m))
	w.Header().Set("Content-Type", octetStream)
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.WriteHeader(http.StatusOK)
	io.Copy(w, f)
}

func (s *Server) findLease(ctx context.Context, r *http.Request, id string, now time.Time) (store.Lease, bool, error) {
	raw, err := core.DecodeExact(r.Header.Get(headerLease), core.TokenLen)
	if err != nil {
		return store.Lease{}, false, nil
	}
	leases, err := s.store.Leases(ctx, id)
	if err != nil {
		return store.Lease{}, false, err
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
	if !s.readLimited(w, r) {
		return
	}
	rec, id, ok, err := s.authorize(ctx, r.PathValue("id"), bearer(r), pepper.Read)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if !ok {
		writeNotFound(w)
		return
	}
	if rec.Tombstone != nil {
		writeGone(w, string(rec.Tombstone.State))
		return
	}
	now := s.clock()
	lease, active, err := s.findLease(ctx, r, id.String(), now)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if !active {
		writeGone(w, string(rec.Message.State))
		return
	}
	if _, err := s.store.RecordDownload(ctx, lease.RowID, now); err != nil {
		switch {
		case errors.Is(err, store.ErrLeaseLimit):
			writeRateLimited(w, ceilSeconds(lease.ExpiresAt.Sub(now)))
		case errors.Is(err, store.ErrLeaseInvalid):
			writeGone(w, string(rec.Message.State))
		default:
			s.internal(w, r, err)
		}
		return
	}
	s.serveBlob(w, r, rec.Message)
}

func (s *Server) handleAck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rec, id, ok, err := s.authorize(ctx, r.PathValue("id"), bearer(r), pepper.Read)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if !ok {
		writeNotFound(w)
		return
	}
	if rec.Tombstone != nil {
		writeGone(w, string(rec.Tombstone.State))
		return
	}
	now := s.clock()
	lease, active, err := s.findLease(ctx, r, id.String(), now)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if !active {
		writeGone(w, string(rec.Message.State))
		return
	}
	if err := s.store.Ack(ctx, lease.RowID); err != nil {
		s.internal(w, r, err)
		return
	}
	if _, err := store.Release(ctx, s.store, s.blobs, id.String(), now); err != nil {
		s.internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) endWith(w http.ResponseWriter, r *http.Request, purpose pepper.Purpose, state store.State) {
	ctx := r.Context()
	rec, id, ok, err := s.authorize(ctx, r.PathValue("id"), bearer(r), purpose)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if !ok {
		writeNotFound(w)
		return
	}
	if rec.Tombstone != nil {
		writeGone(w, string(rec.Tombstone.State))
		return
	}
	if err := store.EndMessage(ctx, s.store, s.blobs, id.String(), state, s.clock()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeGone(w, string(store.StateDeleted))
			return
		}
		s.internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleBurn(w http.ResponseWriter, r *http.Request) {
	s.endWith(w, r, pepper.Read, store.StateDeleted)
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	s.endWith(w, r, pepper.Manage, store.StateCancelled)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	rec, _, ok, err := s.authorize(r.Context(), r.PathValue("id"), bearer(r), pepper.Manage)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if !ok {
		writeNotFound(w)
		return
	}
	if t := rec.Tombstone; t != nil {
		writeJSON(w, http.StatusOK, statusResponse{State: string(t.State), FirstOpenedAt: formatOptTime(t.FirstOpenedAt), EndedAt: formatTime(t.EndedAt)})
		return
	}
	m := rec.Message
	writeJSON(w, http.StatusOK, statusResponse{
		State: string(m.State), OpensUsed: int(m.OpensMax - m.OpensRemaining), OpensMax: int(m.OpensMax),
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
	now := s.clock()
	retry, err := s.limitAbuse(ctx, now, s.clientAddr(r))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if retry > 0 {
		writeRateLimited(w, int64(retry/time.Second)+1)
		return
	}
	var body abuseBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return
	}
	_, id, ok, err := s.authorize(ctx, body.ID, body.ReadToken, pepper.Read)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if ok {
		if err := s.recordAbuse(now, id.String(), body.Reason); err != nil {
			s.log.Error("abuse-record", "err", err.Error())
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (s *Server) recordAbuse(now time.Time, id, reason string) error {
	if len(reason) > abuseReasonLimit {
		reason = reason[:abuseReasonLimit]
	}
	line, err := json.Marshal(map[string]string{"time": formatTime(now), "id": id, "reason": reason})
	if err != nil {
		return err
	}
	s.abuseMu.Lock()
	defer s.abuseMu.Unlock()
	f, err := os.OpenFile(s.abusePath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

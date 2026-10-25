package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/store"
)

var dummyPublicKey = core.PublicKey(new([core.SeedLen]byte))

type access struct {
	id        core.ID
	record    store.Record
	known     bool
	signature core.Signature
	leases    []store.Lease
}

// authorize does the same work for an unknown id, a wrong key and a failed signature (spec §5.5, AK21).
func (s *Server) authorize(
	ctx context.Context,
	r *http.Request,
	rawID string,
	purpose pepper.Purpose,
	body []byte,
) (access, bool, error) {
	id, idErr := core.ParseID(rawID)
	signature, parsed := parseSignature(r)
	fresh := spentBy(r, signature.Challenge)
	granted := access{id: id, signature: signature}
	if idErr == nil {
		var err error
		if granted.record, granted.known, err = s.lookUp(ctx, id); err != nil {
			return granted, false, err
		}
	}
	verified := s.verifySignature(r, &granted, purpose, body)
	if !granted.known || !parsed || !fresh || !verified {
		return granted, false, nil
	}
	s.settle(ctx, &granted, s.clock())
	return granted, true, nil
}

func parseSignature(r *http.Request) (core.Signature, bool) {
	signature, err := core.ParseAuthorization(r.Header.Get("Authorization"))
	if err != nil {
		return core.Signature{PublicKey: dummyPublicKey}, false
	}
	return signature, true
}

func (s *Server) lookUp(ctx context.Context, id core.ID) (store.Record, bool, error) {
	record, err := s.store.Get(ctx, id.String())
	if err == nil {
		return record, true, nil
	}
	if errors.Is(err, store.ErrNotFound) {
		return store.Record{}, false, nil
	}
	return store.Record{}, false, err
}

func (s *Server) verifySignature(r *http.Request, granted *access, purpose pepper.Purpose, body []byte) bool {
	mac, pepperID := s.ring.Dummy(), s.ring.CurrentID()
	if granted.known {
		mac, pepperID = granted.record.ReadMAC(), granted.record.PepperID()
		if purpose == pepper.Manage {
			mac = granted.record.ManageMAC()
		}
	}
	signature := granted.signature
	keyOK := core.ValidPublicKey(signature.PublicKey[:])
	if !keyOK {
		signature.PublicKey = dummyPublicKey
	}
	macOK := s.ring.Verify(purpose, signature.PublicKey[:], mac, pepperID)
	signatureOK := signature.Verify(signedPurpose(purpose), r.Method, r.URL.EscapedPath(), body)
	return keyOK && macOK && signatureOK
}

func signedPurpose(purpose pepper.Purpose) string {
	if purpose == pepper.Manage {
		return core.PurposeManage
	}
	return core.PurposeRead
}

// settle finishes what only an authorized request may finish: an ending, or the release after the last lease.
func (s *Server) settle(ctx context.Context, granted *access, now time.Time) {
	message := granted.record.Message
	if message == nil {
		return
	}
	settled, err := s.settleMessage(ctx, granted, message, now)
	if err == nil && settled {
		err = s.reload(ctx, granted)
	}
	if err != nil {
		entry := failure(ctx, err)
		s.logger.Log(ctx, entry.level, "settle", "err", entry.text)
	}
}

func (s *Server) reload(ctx context.Context, granted *access) error {
	record, err := s.store.Get(ctx, granted.id.String())
	if err != nil {
		return err
	}
	granted.record = record
	return nil
}

func (s *Server) settleMessage(
	ctx context.Context,
	granted *access,
	message *store.Message,
	now time.Time,
) (bool, error) {
	id := granted.id.String()
	var err error
	switch {
	case ended(message, now):
		_, err = store.EndMessage(ctx, s.store, s.blobs, id, store.StateExpired, now)
	case message.OpensRemaining == 0:
		leases, leasesErr := s.store.Leases(ctx, id)
		if leasesErr != nil {
			return false, leasesErr
		}
		if slices.ContainsFunc(leases, func(lease store.Lease) bool { return lease.Active(now) }) {
			granted.leases = leases
			return false, nil
		}
		_, err = store.Release(ctx, s.store, s.blobs, id, now)
	default:
		return false, nil
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return false, err
	}
	return true, nil
}

func (s *Server) authorized(w http.ResponseWriter, r *http.Request, purpose pepper.Purpose) (access, bool) {
	if !emptyBody(w, r) {
		return access{}, false
	}
	granted, ok, err := s.authorize(r.Context(), r, r.PathValue("id"), purpose, nil)
	if err != nil {
		s.writeInternal(w, r, err)
		return granted, false
	}
	if !ok {
		writeNotFound(w)
		return granted, false
	}
	return granted, true
}

func (s *Server) authorizedLive(w http.ResponseWriter, r *http.Request, purpose pepper.Purpose) (access, bool) {
	granted, ok := s.authorized(w, r, purpose)
	if !ok {
		return granted, false
	}
	if tombstone := granted.record.Tombstone; tombstone != nil {
		writeGone(w, string(tombstone.State))
		return granted, false
	}
	if now := s.clock(); ended(granted.record.Message, now) {
		writeGone(w, string(liveState(granted.record.Message, now)))
		return granted, false
	}
	return granted, true
}

func emptyBody(w http.ResponseWriter, r *http.Request) bool {
	var one [1]byte
	if extraBytes, err := io.ReadFull(r.Body, one[:]); extraBytes != 0 || !errors.Is(err, io.EOF) {
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

func ended(message *store.Message, now time.Time) bool {
	return message.Ending != "" || !now.Before(message.ExpiresAt)
}

func liveState(message *store.Message, now time.Time) store.State {
	expired := !now.Before(message.ExpiresAt)
	switch {
	case message.Ending != "":
		return message.Ending
	case expired && message.OpensRemaining == 0:
		return store.StateOpened
	case expired:
		return store.StateExpired
	default:
		return message.State
	}
}

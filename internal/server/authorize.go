package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/core/signing"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/secretmode"
	"github.com/misaon/nylzo/internal/store"
)

var dummyPublicKey = signing.PublicKey(new([core.SeedLen]byte))

type access struct {
	id        core.ID
	record    store.Record
	known     bool
	signature signing.Signature
}

// authorize does the same work for an unknown id, a wrong key and a failed signature (spec §5.5, AK21).
func (s *Server) authorize(
	ctx context.Context,
	r *http.Request,
	rawID string,
	purpose seal.Purpose,
	body []byte,
) (access, bool, error) {
	id, idErr := core.ParseID(rawID)
	signature, parsed := parseSignature(r)
	fresh := spentBy(r, signature.Challenge)
	granted := access{id: id, signature: signature}
	row := s.dummyRow
	if idErr == nil {
		found, known, err := s.store.Lookup(ctx, seal.LocatorOf(id[:]))
		if err != nil {
			return granted, false, err
		}
		row, granted.known = found, known
	}
	opened := s.openWithSignature(r, &granted, &row, purpose, body)
	if !granted.known || !parsed || !fresh || !opened {
		return granted, false, nil
	}
	s.settle(ctx, &granted.record, time.Now())
	return granted, true, nil
}

// refusalFloor is above the p99 of the work of a refused request, so its answer time says nothing about the id.
const refusalFloor = 5 * time.Millisecond

func waitForTheFloor(r *http.Request, arrived time.Time) {
	select {
	case <-r.Context().Done():
	case <-time.After(time.Until(arrived.Add(refusalFloor))):
	}
}

func parseSignature(r *http.Request) (signing.Signature, bool) {
	signature, err := signing.ParseAuthorization(r.Header.Get("Authorization"))
	if err != nil {
		return signing.Signature{PublicKey: dummyPublicKey}, false
	}
	return signature, true
}

func (s *Server) openWithSignature(
	r *http.Request,
	granted *access,
	row *store.Row,
	purpose seal.Purpose,
	body []byte,
) bool {
	signature := granted.signature
	keyOK := signing.ValidPublicKey(signature.PublicKey[:])
	if !keyOK {
		signature.PublicKey = dummyPublicKey
	}
	record, openErr := s.openRow(row, purpose, signature.PublicKey[:])
	components, single := requestComponents(r)
	signatureOK := signature.Verify(signedPurpose(purpose), r.Method, r.URL.EscapedPath(), body, components)
	granted.record = record
	return keyOK && openErr == nil && signatureOK && single
}

// requestComponents rejects repeated singleton fields so intermediaries cannot select a different value.
func requestComponents(r *http.Request) (signing.Components, bool) {
	single := true
	for _, name := range []string{api.ShareKeyHeaderName, api.LeaseHeaderName, api.KE1HeaderName, api.KE3HeaderName} {
		single = single && len(r.Header.Values(name)) <= 1
	}
	return signing.Components{
		ShareKey: r.Header.Get(api.ShareKeyHeaderName),
		Lease:    r.Header.Get(api.LeaseHeaderName),
		KE1:      r.Header.Get(api.KE1HeaderName),
		KE3:      r.Header.Get(api.KE3HeaderName),
	}, single
}

// openRow opens the record with a zero key after a failed unwrap, so a wrong key costs the same work as a right one.
func (s *Server) openRow(row *store.Row, purpose seal.Purpose, publicKey []byte) (store.Record, error) {
	wrapped := row.ReadWrap
	if purpose == seal.Manage {
		wrapped = row.ManageWrap
	}
	var record store.Record
	var err error
	secretmode.Do(func() {
		key, unwrapErr := seal.UnwrapRecordKey(s.recordsKey, publicKey, wrapped, row.Locator, purpose)
		var openErr error
		record, openErr = store.OpenRecord(row, key)
		clear(key[:])
		err = errors.Join(unwrapErr, openErr)
	})
	return record, err
}

// forget clears the record key when the request ends, so a later memory image does not hold it (A4-2).
func (a *access) forget() {
	clear(a.record.Key[:])
}

// readEraseSalt reads the salt that seals the registration and checks it against its tag, so a changed salt costs no
// passphrase attempt, and answers a blob that is gone as a claim does.
func (s *Server) readEraseSalt(
	w http.ResponseWriter,
	r *http.Request,
	record *store.Record,
	salt *seal.EraseSalt,
) bool {
	var key seal.BlobKey
	defer key.Clear()
	if !s.readBlobKey(w, r, record, &key) {
		return false
	}
	file, read, ok := s.openOrFinishBlob(w, r, record)
	defer clear(read[:])
	if !ok {
		return false
	}
	file.Close()
	if err := seal.CheckSaltTag(&key, read); err != nil {
		s.writeBlobFailure(w, r, err)
		return false
	}
	*salt = read
	return true
}

func writePassphraseRefusal(w http.ResponseWriter, refusal *store.PassphraseError) {
	writeJSON(w, http.StatusForbidden, api.PassphraseResponse{
		Error:        api.ErrorPassphrase,
		RetryAfter:   ceilSeconds(refusal.RetryAfter),
		AttemptsLeft: refusal.AttemptsLeft,
	})
}

func signedPurpose(purpose seal.Purpose) string {
	if purpose == seal.Manage {
		return signing.PurposeManage
	}
	return signing.PurposeRead
}

// settle finishes what only an authorized request may finish: an ending, or the release after the last lease.
func (s *Server) settle(ctx context.Context, record *store.Record, now time.Time) {
	finished, err := store.Finish(ctx, s.store, s.blobs, record, now)
	if finished {
		s.leaseTimers.Cancel(record.Row.Locator)
	}
	if err == nil && finished {
		err = s.reload(ctx, record)
	}
	if err != nil {
		entry := failure(ctx, err)
		s.events.Log(ctx, entry.level, "settle", "err", entry.text)
	}
}

func (s *Server) reload(ctx context.Context, record *store.Record) error {
	row, err := s.store.Load(ctx, record.Row.Locator)
	if err != nil {
		return err
	}
	reloaded, err := store.OpenRecord(&row, record.Key)
	if err != nil {
		return err
	}
	*record = reloaded
	return nil
}

func (s *Server) authorized(w http.ResponseWriter, r *http.Request, purpose seal.Purpose) (access, bool) {
	if !emptyBody(w, r) {
		return access{}, false
	}
	arrived := time.Now()
	granted, ok, err := s.authorize(r.Context(), r, r.PathValue("id"), purpose, nil)
	if err != nil {
		s.writeInternal(w, r, err)
		return granted, false
	}
	if !ok {
		waitForTheFloor(r, arrived)
		writeNotFound(w)
		return granted, false
	}
	return granted, true
}

func (s *Server) authorizedLive(w http.ResponseWriter, r *http.Request, purpose seal.Purpose) (access, bool) {
	granted, ok := s.authorized(w, r, purpose)
	if !ok {
		return granted, false
	}
	if now := time.Now(); granted.record.Ended(now) {
		writeGone(w, string(granted.record.State(now)))
		return granted, false
	}
	return granted, true
}

// emptyBodyWait bounds the read of a body of unknown length, which a proxy sends for a request without one, such as
// Caddy for an HTTP/3 request without Content-Length, and which ends at once unless the client holds it open.
const emptyBodyWait = time.Second

// emptyBody refuses a declared body without reading it and gives a body of unknown length one second to end, so a
// signed request never waits longer for bytes it must not have.
func emptyBody(w http.ResponseWriter, r *http.Request) bool {
	if r.ContentLength > 0 || (r.ContentLength < 0 && !endsWithin(w, r, emptyBodyWait)) {
		writeError(w, http.StatusBadRequest, errBadRequest)
		return false
	}
	return true
}

func endsWithin(w http.ResponseWriter, r *http.Request, wait time.Duration) bool {
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(wait))
	var one [1]byte
	read, err := io.ReadFull(r.Body, one[:])
	return read == 0 && errors.Is(err, io.EOF)
}

func validPathID(w http.ResponseWriter, r *http.Request) bool {
	if _, err := core.ParseID(r.PathValue("id")); err != nil {
		writeNotFound(w)
		return false
	}
	return true
}

// newDummyRow is opened instead of a missing row; random wrapping keys make sure no public key opens it.
func newDummyRow(recordsKey seal.Key) store.Row {
	id := core.NewID()
	message := &store.Message{Version: 1, OpensMax: 1, OpensRemaining: 1}
	record := store.NewRecord(
		recordsKey,
		seal.LocatorOf(id[:]),
		message,
		core.NewSecret()[:],
		core.NewSecret()[:],
		"",
		new(seal.NewBlobKey()),
	)
	return record.Row
}

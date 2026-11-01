package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/signing"
	"github.com/misaon/nylzo/internal/seal"
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
		found, known, err := s.lookUp(ctx, id)
		if err != nil {
			return granted, false, err
		}
		if known {
			row, granted.known = found, true
		}
	}
	opened := openWithSignature(r, &granted, &row, purpose, body)
	if !granted.known || !parsed || !fresh || !opened {
		return granted, false, nil
	}
	s.settle(ctx, &granted.record, time.Now())
	return granted, true, nil
}

func parseSignature(r *http.Request) (signing.Signature, bool) {
	signature, err := signing.ParseAuthorization(r.Header.Get("Authorization"))
	if err != nil {
		return signing.Signature{PublicKey: dummyPublicKey}, false
	}
	return signature, true
}

func (s *Server) lookUp(ctx context.Context, id core.ID) (store.Row, bool, error) {
	row, err := s.store.Load(ctx, seal.LocatorOf(id[:]))
	if err == nil {
		return row, true, nil
	}
	if errors.Is(err, store.ErrNotFound) {
		return store.Row{}, false, nil
	}
	return store.Row{}, false, err
}

func openWithSignature(
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
	record, openErr := openRow(row, purpose, signature.PublicKey[:])
	signatureOK := signature.Verify(signedPurpose(purpose), r.Method, r.URL.EscapedPath(), body)
	granted.record = record
	return keyOK && openErr == nil && signatureOK
}

// openRow opens the record with a zero key after a failed unwrap, so a wrong key costs the same work as a right one.
func openRow(row *store.Row, purpose seal.Purpose, publicKey []byte) (store.Record, error) {
	wrapped := row.ReadWrap
	if purpose == seal.Manage {
		wrapped = row.ManageWrap
	}
	key, unwrapErr := seal.Unwrap(seal.KeyFor(publicKey, row.Locator, purpose), wrapped, row.Locator, purpose)
	record, openErr := store.OpenRecord(row, key)
	return record, errors.Join(unwrapErr, openErr)
}

// verifierInput binds the verifier mac to the erase salt, so once the blob is erased no copy of a record tests a passphrase.
func verifierInput(salt seal.EraseSalt, verifierKey []byte) []byte {
	return append(salt[:len(salt):len(salt)], verifierKey...)
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
	if err == nil && finished {
		err = s.reload(ctx, record)
	}
	if err != nil {
		entry := failure(ctx, err)
		s.logger.Log(ctx, entry.level, "settle", "err", entry.text)
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

// newDummyRow is opened instead of a missing row; random wrapping keys make sure no public key opens it.
func newDummyRow() store.Row {
	id := core.NewID()
	message := &store.Message{Version: 1, OpensMax: 1, OpensRemaining: 1}
	record := store.NewRecord(
		seal.LocatorOf(id[:]),
		message,
		core.NewSecret()[:],
		core.NewSecret()[:],
		"",
		seal.NewKey(),
	)
	return record.Row
}

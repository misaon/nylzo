package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/misaon/nylzo/internal/core/envelope"
	"github.com/misaon/nylzo/internal/epoch"
	"github.com/misaon/nylzo/internal/seal"
)

const (
	recordFormat = 3
	macLen       = 32
	fieldsLen    = 310
	leaseLen     = 42
)

// BlobReferenceLen is the number of random bytes in a blob reference.
const BlobReferenceLen = 16

var endings = []State{"", StateDeleted, StateCancelled}

// DummyLocator keys the row that a lookup of an unknown id reads; finding an id that hashes to it is a preimage.
var DummyLocator seal.Locator = sha256.Sum256([]byte("nylzo/v1/dummy-row"))

// NewDummyRow has the shape of a live row and random keys, so no public key opens it. No pepper opens its wraps either
// until serve seals them under its own when it starts (ResealRow).
func NewDummyRow() Row {
	var readKey, manageKey, reference [seal.KeyLen]byte
	rand.Read(readKey[:])
	rand.Read(manageKey[:])
	rand.Read(reference[:])
	far := time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC)
	message := &Message{
		Version:        envelope.Version,
		OpensMax:       1,
		OpensRemaining: 1,
		CreatedAt:      far,
		ExpiresAt:      far,
		Registration: seal.WrapRegistration(
			new(seal.NewEraseSalt()),
			DummyLocator,
			make([]byte, seal.RegistrationLen),
		),
	}
	blobReference := base64.RawURLEncoding.EncodeToString(reference[:BlobReferenceLen])
	blob := seal.NewBlobKey()
	return NewRecord(seal.NewKey(), DummyLocator, message, readKey[:], manageKey[:], blobReference, &blob).Row
}

// ResealRow puts both wraps of a row under the current pepper key (seal.ResealWrap). The dummy row gets new wraps when
// no key opens it, so it costs what a live row does.
func ResealRow(row *Row, current seal.Key, earlier []seal.Key) (bool, error) {
	readChanged, readErr := seal.ResealWrap(current, earlier, &row.ReadWrap, row.Locator, seal.Read)
	manageChanged, manageErr := seal.ResealWrap(current, earlier, &row.ManageWrap, row.Locator, seal.Manage)
	err := errors.Join(readErr, manageErr)
	if err != nil && row.Locator == DummyLocator {
		row.ReadWrap, row.ManageWrap = dummyWrap(current, seal.Read), dummyWrap(current, seal.Manage)
		return true, nil
	}
	return readChanged || manageChanged, err
}

func dummyWrap(pepperKey seal.Key, purpose seal.Purpose) []byte {
	publicKey := seal.NewKey()
	return seal.WrapRecordKey(pepperKey, publicKey[:], seal.NewKey(), DummyLocator, purpose)
}

// Record is a row opened with its record key, for the length of one request.
type Record struct {
	Row     Row
	Key     seal.Key
	Message *Message
}

// NewRecord seals a new message; the public keys only derive the wrapping keys and are never kept, and the pepper key
// seals both wraps.
func NewRecord(
	pepperKey seal.Key,
	locator seal.Locator,
	message *Message,
	readKey, manageKey []byte,
	blobReference string,
	blob *seal.BlobKey,
) Record {
	key := seal.NewKey()
	record := Record{
		Row: Row{
			Locator:       locator,
			ReadWrap:      seal.WrapRecordKey(pepperKey, readKey, key, locator, seal.Read),
			ManageWrap:    seal.WrapRecordKey(pepperKey, manageKey, key, locator, seal.Manage),
			BlobReference: blobReference,
			BlobWrap:      seal.WrapBlobKey(key, blob, locator),
			BlobPurgeAt:   BlobPurgeAt(message.ExpiresAt),
			PurgeAt:       ceil(message.ExpiresAt.Add(TombstoneTTL), RowPurgeStep),
		},
		Key:     key,
		Message: message,
	}
	record.reseal()
	return record
}

func OpenRecord(row *Row, key seal.Key) (Record, error) {
	fields, err := seal.OpenRecord(key, row.Locator, row.Record)
	if err != nil {
		return Record{}, err
	}
	message, err := decodeMessage(fields)
	if err != nil {
		return Record{}, err
	}
	return Record{Row: *row, Key: key, Message: message}, nil
}

func (r *Record) HasBlob() bool {
	return r.Row.BlobWrap != nil
}

func (r *Record) BlobKey() (seal.BlobKey, error) {
	if !r.HasBlob() {
		return seal.BlobKey{}, ErrBlobMissing
	}
	return seal.UnwrapBlobKey(r.Key, r.Row.BlobWrap, r.Row.Locator)
}

// State checks a recorded ending, spent opens, expiry and a blob the operator removed, in that order.
func (r *Record) State(now time.Time) State {
	message := r.Message
	switch {
	case message.Ending != "":
		return message.Ending
	case message.OpensRemaining == 0:
		return StateOpened
	case !now.Before(message.ExpiresAt):
		return StateExpired
	case !r.HasBlob():
		return StateDeleted
	default:
		return StateAvailable
	}
}

// Ended tells whether the message refuses claims, downloads and endings; spent opens end it once a release is recorded.
func (r *Record) Ended(now time.Time) bool {
	message := r.Message
	released := message.OpensRemaining == 0 && !message.EndedAt.IsZero()
	return message.Ending != "" || released || !now.Before(message.ExpiresAt) || !r.HasBlob()
}

func (r *Record) Releasable(now time.Time) bool {
	return r.Message.OpensRemaining == 0 && !slices.ContainsFunc(r.Message.Leases, func(lease Lease) bool {
		return lease.Active(now)
	})
}

// EndedAt is absent for a message the operator removed without its key.
func (r *Record) EndedAt(now time.Time) time.Time {
	message := r.Message
	switch {
	case !message.EndedAt.IsZero():
		return message.EndedAt
	case message.OpensRemaining == 0:
		return earlier(message.ExpiresAt, lastLeaseEnd(message))
	case !now.Before(message.ExpiresAt):
		return message.ExpiresAt
	default:
		return time.Time{}
	}
}

// recordEnd also drops the blob wrap, so the transaction that ends a message makes its blob unreadable.
func (r *Record) recordEnd(now time.Time) {
	end := earlier(now, r.Message.ExpiresAt).UTC().Truncate(time.Minute)
	if r.Message.EndedAt.IsZero() {
		r.Message.EndedAt = end
	}
	r.Row.BlobWrap = nil
	r.Row.BlobPurgeAt = ceil(now, BlobPurgeStep)
	r.Row.PurgeAt = ceil(end.Add(TombstoneTTL), RowPurgeStep)
}

func (r *Record) reseal() {
	r.Row.Record = seal.Record(r.Key, r.Row.Locator, encodeMessage(r.Message))
}

// pruneLeases drops expired leases but keeps the last one, which dates the end of a message whose opens ran out.
func (r *Record) pruneLeases(now time.Time) {
	message := r.Message
	if len(message.Leases) == 0 {
		return
	}
	latest := slices.MaxFunc(message.Leases, func(first, second Lease) int {
		return first.ExpiresAt.Compare(second.ExpiresAt)
	})
	message.Leases = slices.DeleteFunc(message.Leases, func(lease Lease) bool {
		return !lease.Active(now)
	})
	if len(message.Leases) == 0 && message.OpensRemaining == 0 {
		message.Leases = append(message.Leases, latest)
	}
}

func earlier(first, second time.Time) time.Time {
	if second.Before(first) {
		return second
	}
	return first
}

func lastLeaseEnd(message *Message) time.Time {
	end := message.ExpiresAt
	for i, lease := range message.Leases {
		if i == 0 || lease.ExpiresAt.After(end) {
			end = lease.ExpiresAt
		}
	}
	return end
}

func encodeMessage(message *Message) []byte {
	data := make([]byte, 0, fieldsLen+len(message.Leases)*leaseLen)
	data = append(data, recordFormat, message.Version, message.KDF, message.Flags)
	data = appendTime(data, message.CreatedAt)
	data = appendTime(data, message.ExpiresAt)
	data = binary.BigEndian.AppendUint32(data, message.TTLSeconds)
	data = binary.BigEndian.AppendUint16(data, message.OpensMax)
	data = binary.BigEndian.AppendUint16(data, message.OpensRemaining)
	data = binary.BigEndian.AppendUint64(data, narrow64(message.Size))
	data = append(data, message.RegistrationPepperID)
	data = append(data, fixed(message.Registration, seal.SealedRegistrationLen)...)
	data = binary.BigEndian.AppendUint16(data, saturate[uint16](int64(message.FailCountTotal)))
	data = appendTime(data, message.NextAttemptAt)
	data = appendTime(data, optional(message.FirstOpenedAt))
	data = append(data, saturate[uint8](int64(slices.Index(endings, message.Ending))))
	data = appendTime(data, message.EndedAt)
	data = binary.BigEndian.AppendUint32(data, saturate[uint32](message.ReportDay))
	data = append(data, saturate[uint8](int64(message.Reports)))
	data = binary.BigEndian.AppendUint32(data, message.Epoch)
	data = append(data, message.Generation[:]...)
	data = append(data, saturate[uint8](int64(len(message.Leases))))
	for _, lease := range message.Leases {
		data = append(data, lease.PepperID)
		data = append(data, fixedMAC(lease.MAC)...)
		data = appendTime(data, lease.ExpiresAt)
		data = append(data, saturate[uint8](int64(lease.Downloads)))
	}
	return data
}

type decoder struct {
	data []byte
}

const (
	uint16Bytes = 2
	uint32Bytes = 4
	uint64Bytes = 8
)

func (d *decoder) next(length int) []byte {
	if len(d.data) < length {
		d.data = nil
		return make([]byte, length)
	}
	taken := d.data[:length]
	d.data = d.data[length:]
	return taken
}

func (d *decoder) readByte() byte     { return d.next(1)[0] }
func (d *decoder) readUint16() uint16 { return binary.BigEndian.Uint16(d.next(uint16Bytes)) }
func (d *decoder) readUint32() uint32 { return binary.BigEndian.Uint32(d.next(uint32Bytes)) }
func (d *decoder) readUint64() uint64 { return binary.BigEndian.Uint64(d.next(uint64Bytes)) }
func (d *decoder) readTime() time.Time {
	return fromMilliseconds(signed(d.readUint64()))
}
func (d *decoder) readMAC() []byte           { return slices.Clone(d.next(macLen)) }
func (d *decoder) truncated() bool           { return d.data == nil }
func (d *decoder) remaining(length int) bool { return len(d.data) >= length }

func decodeMessage(data []byte) (*Message, error) {
	fields := &decoder{data: data}
	format := fields.readByte()
	if format != recordFormat {
		return nil, fmt.Errorf("%w: unknown record format", ErrCorrupt)
	}
	message := &Message{Version: fields.readByte(), KDF: fields.readByte(), Flags: fields.readByte()}
	message.CreatedAt = fields.readTime()
	message.ExpiresAt = fields.readTime()
	message.TTLSeconds = fields.readUint32()
	message.OpensMax = fields.readUint16()
	message.OpensRemaining = fields.readUint16()
	message.Size = signed(fields.readUint64())
	message.RegistrationPepperID = fields.readByte()
	message.Registration = slices.Clone(fields.next(seal.SealedRegistrationLen))
	message.FailCountTotal = int(fields.readUint16())
	message.NextAttemptAt = fields.readTime()
	if firstOpened := fields.readTime(); !firstOpened.IsZero() {
		message.FirstOpenedAt = &firstOpened
	}
	ending := int(fields.readByte())
	message.EndedAt = fields.readTime()
	message.ReportDay = int64(fields.readUint32())
	message.Reports = int(fields.readByte())
	message.Epoch = fields.readUint32()
	copy(message.Generation[:], fields.next(epoch.GenerationIDLen))
	leases := int(fields.readByte())
	if fields.truncated() || ending >= len(endings) || !fields.remaining(leases*leaseLen) {
		return nil, fmt.Errorf("%w: truncated or unknown field", ErrCorrupt)
	}
	message.Ending = endings[ending]
	for range leases {
		lease := Lease{PepperID: fields.readByte(), MAC: fields.readMAC()}
		lease.ExpiresAt = fields.readTime()
		lease.Downloads = int(fields.readByte())
		message.Leases = append(message.Leases, lease)
	}
	if message.OpensMax < 1 || message.OpensRemaining > message.OpensMax || message.Size < 0 {
		return nil, fmt.Errorf("%w: opens %d of %d", ErrCorrupt, message.OpensRemaining, message.OpensMax)
	}
	return message, nil
}

func appendTime(data []byte, moment time.Time) []byte {
	if moment.IsZero() {
		return binary.BigEndian.AppendUint64(data, 0)
	}
	return binary.BigEndian.AppendUint64(data, narrow64(moment.UnixMilli()))
}

// narrow64 and saturate store a value the record cannot hold as its nearest bound; decodeMessage then rejects it.
func narrow64(value int64) uint64 {
	if value < 0 {
		return 0
	}
	return uint64(value)
}

//nolint:ireturn // T is always a concrete unsigned integer type, never an interface.
func saturate[T uint8 | uint16 | uint32](value int64) T {
	if value < 0 {
		return 0
	}
	if bound := ^T(0); value > int64(bound) {
		return bound
	}
	return T(value)
}

func signed(value uint64) int64 {
	if value > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(value)
}

func fromMilliseconds(milliseconds int64) time.Time {
	if milliseconds == 0 {
		return time.Time{}
	}
	return time.UnixMilli(milliseconds).UTC()
}

func optional(moment *time.Time) time.Time {
	if moment == nil {
		return time.Time{}
	}
	return *moment
}

func fixedMAC(mac []byte) []byte {
	return fixed(mac, macLen)
}

func fixed(value []byte, length int) []byte {
	padded := make([]byte, length)
	copy(padded, value)
	return padded
}

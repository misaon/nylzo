package store

import (
	"encoding/binary"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/misaon/nylzo/internal/seal"
)

const (
	recordFormat = 1
	macLen       = 32
	fieldsLen    = 103
	leaseLen     = 42
	day          = 24 * time.Hour
)

var endings = []State{"", StateDeleted, StateCancelled}

// Record is a row opened with its record key, for the length of one request.
type Record struct {
	Row     Row
	Key     seal.Key
	Message *Message
}

// NewRecord seals a new message; the public keys only derive the wrapping keys and are never kept.
func NewRecord(
	locator seal.Locator,
	message *Message,
	readKey, manageKey []byte,
	blobRef string,
	blobKey seal.Key,
) Record {
	key := seal.NewKey()
	record := Record{
		Row: Row{
			Locator:     locator,
			ReadWrap:    seal.Wrap(seal.KeyFor(readKey, locator, seal.Read), key, locator, seal.Read),
			ManageWrap:  seal.Wrap(seal.KeyFor(manageKey, locator, seal.Manage), key, locator, seal.Manage),
			BlobRef:     blobRef,
			BlobWrap:    seal.WrapBlobKey(key, blobKey, locator),
			BlobPurgeAt: ceil(message.ExpiresAt, BlobPurgeStep),
			PurgeAt:     ceil(message.ExpiresAt.Add(TombstoneTTL), RowPurgeStep),
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

func (r *Record) BlobKey() (seal.Key, error) {
	if !r.HasBlob() {
		return seal.Key{}, ErrBlobMissing
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

// Ended tells whether the message refuses claims, downloads and endings; spent opens alone do not end it.
func (r *Record) Ended(now time.Time) bool {
	return r.Message.Ending != "" || !now.Before(r.Message.ExpiresAt) || !r.HasBlob()
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

func (r *Record) recordEnd(now time.Time) {
	end := earlier(now, r.Message.ExpiresAt).UTC().Truncate(time.Minute)
	if r.Message.EndedAt.IsZero() {
		r.Message.EndedAt = end
	}
	r.Row.BlobPurgeAt = ceil(now, BlobPurgeStep)
	r.Row.PurgeAt = ceil(end.Add(TombstoneTTL), RowPurgeStep)
}

func (r *Record) reseal() {
	r.Row.Record = seal.Record(r.Key, r.Row.Locator, encodeMessage(r.Message))
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
	data = append(data, message.VerifierPepperID, boolByte(message.PassphraseRequired()))
	data = append(data, fixedMAC(message.VerifierMAC)...)
	data = binary.BigEndian.AppendUint16(data, narrow16(message.FailCountTotal))
	data = appendTime(data, message.NextAttemptAt)
	data = appendTime(data, optional(message.FirstOpenedAt))
	data = append(data, narrow8(slices.Index(endings, message.Ending)))
	data = appendTime(data, message.EndedAt)
	data = binary.BigEndian.AppendUint32(data, narrow32(message.ReportDay))
	data = append(data, narrow8(message.Reports), narrow8(len(message.Leases)))
	for _, lease := range message.Leases {
		data = append(data, lease.PepperID)
		data = append(data, fixedMAC(lease.MAC)...)
		data = appendTime(data, lease.ExpiresAt)
		data = append(data, narrow8(lease.Downloads))
	}
	return data
}

type decoder struct {
	data []byte
}

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
func (d *decoder) readUint16() uint16 { return binary.BigEndian.Uint16(d.next(2)) }
func (d *decoder) readUint32() uint32 { return binary.BigEndian.Uint32(d.next(4)) }
func (d *decoder) readTime() time.Time {
	return fromMilliseconds(signed(binary.BigEndian.Uint64(d.next(8))))
}
func (d *decoder) readMAC() []byte           { return slices.Clone(d.next(macLen)) }
func (d *decoder) truncated() bool           { return d.data == nil }
func (d *decoder) remaining(length int) bool { return len(d.data) >= length }

func decodeMessage(data []byte) (*Message, error) {
	fields := &decoder{data: data}
	if fields.readByte() != recordFormat {
		return nil, fmt.Errorf("%w: unknown record format", ErrCorrupt)
	}
	message := &Message{Version: fields.readByte(), KDF: fields.readByte(), Flags: fields.readByte()}
	message.CreatedAt = fields.readTime()
	message.ExpiresAt = fields.readTime()
	message.TTLSeconds = fields.readUint32()
	message.OpensMax = fields.readUint16()
	message.OpensRemaining = fields.readUint16()
	message.Size = signed(binary.BigEndian.Uint64(fields.next(8)))
	message.VerifierPepperID = fields.readByte()
	hasVerifier, verifierMAC := fields.readByte(), fields.readMAC()
	if hasVerifier == 1 {
		message.VerifierMAC = verifierMAC
	}
	message.FailCountTotal = int(fields.readUint16())
	message.NextAttemptAt = fields.readTime()
	if firstOpened := fields.readTime(); !firstOpened.IsZero() {
		message.FirstOpenedAt = &firstOpened
	}
	ending := int(fields.readByte())
	message.EndedAt = fields.readTime()
	message.ReportDay = int64(fields.readUint32())
	message.Reports = int(fields.readByte())
	leases := int(fields.readByte())
	if fields.truncated() || ending >= len(endings) || hasVerifier > 1 || !fields.remaining(leases*leaseLen) {
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

// The narrowing helpers store a value the record cannot hold as its nearest bound; decodeMessage then rejects it.
func narrow64(value int64) uint64 {
	if value < 0 {
		return 0
	}
	return uint64(value)
}

func narrow32(value int64) uint32 {
	if value < 0 {
		return 0
	}
	if value > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(value)
}

func narrow16(value int) uint16 {
	if value < 0 {
		return 0
	}
	if value > math.MaxUint16 {
		return math.MaxUint16
	}
	return uint16(value)
}

func narrow8(value int) byte {
	if value < 0 {
		return 0
	}
	if value > math.MaxUint8 {
		return math.MaxUint8
	}
	return byte(value)
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
	fixed := make([]byte, macLen)
	copy(fixed, mac)
	return fixed
}

func boolByte(value bool) byte {
	if value {
		return 1
	}
	return 0
}

func reportDay(now time.Time) int64 {
	return now.UTC().Unix() / int64(day/time.Second)
}

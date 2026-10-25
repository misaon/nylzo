package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

type State string

const (
	StateAvailable State = "available"
	StateOpened    State = "opened"
	StateExpired   State = "expired"
	StateCancelled State = "cancelled"
	StateDeleted   State = "deleted"
)

const (
	LeaseDuration     = 120 * time.Second
	LeaseTimerSlack   = time.Second
	MaxLeaseDownload  = 3
	TombstoneTTL      = 7 * 24 * time.Hour
	MaxFailedAttempts = 100
	FreeAttempts      = 5
	MaxBackoff        = time.Hour
	SweepBatch        = 1000
	BlobGrace         = 15 * time.Minute
	MinTransferRate   = 16000
)

func CeilMinute(t time.Time) time.Time {
	if r := t.Truncate(time.Minute); !r.Equal(t) {
		return r.Add(time.Minute)
	}
	return t
}

func TransferTime(size int64) time.Duration {
	return time.Duration(size) * time.Second / MinTransferRate
}

func LeaseFor(size int64) time.Duration {
	return max(LeaseDuration, TransferTime(size))
}

var (
	ErrNotFound     = errors.New("store: not found")
	ErrConflict     = errors.New("store: id already exists")
	ErrLeaseInvalid = errors.New("store: lease is not active")
	ErrLeaseLimit   = errors.New("store: lease download limit reached")
	ErrBlobMissing  = errors.New("store: message has no blob")
)

type GoneError struct {
	State State
}

func (e *GoneError) Error() string {
	return fmt.Sprintf("store: message is gone (%s)", e.State)
}

type PassphraseError struct {
	RetryAfter   time.Duration
	AttemptsLeft int
}

func (e *PassphraseError) Error() string {
	return fmt.Sprintf("store: passphrase rejected (retry after %s, %d attempts left)", e.RetryAfter, e.AttemptsLeft)
}

type Message struct {
	ID             string
	Version        uint8
	KDF            uint8
	Flags          uint8
	State          State
	CreatedAt      time.Time
	ExpiresAt      time.Time
	TTLSeconds     uint32
	OpensMax       uint16
	OpensRemaining uint16
	PepperID       byte
	ReadMAC        []byte
	ManageMAC      []byte
	VerifierMAC    []byte
	Size           int64
	BlobRef        string
	FailCountTotal int
	NextAttemptAt  time.Time
	FirstOpenedAt  *time.Time
	Ending         State
}

func (m *Message) PassphraseRequired() bool {
	return len(m.VerifierMAC) > 0
}

type Tombstone struct {
	ID            string
	PepperID      byte
	ReadMAC       []byte
	ManageMAC     []byte
	State         State
	FirstOpenedAt *time.Time
	EndedAt       time.Time
	PurgeAt       time.Time
}

type Record struct {
	Message   *Message
	Tombstone *Tombstone
}

func (r Record) PepperID() byte {
	if r.Message != nil {
		return r.Message.PepperID
	}
	return r.Tombstone.PepperID
}

func (r Record) ReadMAC() []byte {
	if r.Message != nil {
		return r.Message.ReadMAC
	}
	return r.Tombstone.ReadMAC
}

func (r Record) ManageMAC() []byte {
	if r.Message != nil {
		return r.Message.ManageMAC
	}
	return r.Tombstone.ManageMAC
}

type Lease struct {
	RowID     int64
	ID        string
	MAC       []byte
	PepperID  byte
	ExpiresAt time.Time
	Downloads int
}

func (l Lease) Active(now time.Time) bool {
	return now.Before(l.ExpiresAt)
}

type ClaimParams struct {
	ID            string
	Clock         func() time.Time
	VerifierSent  bool
	VerifyMAC     func(pepperID byte, storedMAC []byte) bool
	LeaseMAC      []byte
	LeasePepperID byte
}

type ClaimResult struct {
	Message *Message
	Lease   Lease
}

type RateHit struct {
	Key         string
	WindowStart time.Time
	PurgeAt     time.Time
	Bytes       int64
	Limit       int64
	BytesLimit  int64
}

func (h RateHit) Over(c RateCount) bool {
	return (h.Limit > 0 && c.Count > h.Limit) || (h.BytesLimit > 0 && c.Bytes > h.BytesLimit)
}

type RateCount struct {
	Count int64
	Bytes int64
}

type ExpiredMessage struct {
	ID        string
	BlobRef   string
	ExpiresAt time.Time
}

type Messages interface {
	Create(ctx context.Context, m *Message, counters ...RateHit) error
	Get(ctx context.Context, id string) (Record, error)
	Claim(ctx context.Context, p ClaimParams) (ClaimResult, error)
	Leases(ctx context.Context, id string) ([]Lease, error)
	RecordDownload(ctx context.Context, lease Lease, now time.Time) error
	Ack(ctx context.Context, lease Lease, now time.Time) (string, error)
	ReleaseIfDone(ctx context.Context, id string, now time.Time) (string, error)
	ClearBlob(ctx context.Context, id, blobRef string, now time.Time) error
	MarkEnding(
		ctx context.Context,
		id string,
		state State,
		now time.Time,
	) (ending State, ref string, recorded bool, err error)
	End(ctx context.Context, id string, state State, now time.Time) error
	EndExpired(ctx context.Context, ids []string, now time.Time) (int, error)
	ExpiredPage(ctx context.Context, now time.Time, after ExpiredMessage, limit int) ([]ExpiredMessage, error)
	SpentPage(ctx context.Context, after string, limit int) ([]string, error)
	EndingPage(ctx context.Context, now time.Time, after string, limit int) ([]string, error)
	PurgeLeases(ctx context.Context, now time.Time, limit int) (int, error)
	PurgeTombstones(ctx context.Context, now time.Time, limit int) (int, error)
	KnownBlobRefs(ctx context.Context) (map[string]struct{}, error)
}

type Counters interface {
	RateHit(ctx context.Context, hits []RateHit) ([]RateCount, error)
	RateGet(ctx context.Context, key string, windowStart time.Time) (RateCount, error)
	RateReserve(ctx context.Context, hit RateHit) (int64, bool, error)
	RateReserveBatch(ctx context.Context, hits []RateHit) ([]bool, error)
	RateRelease(ctx context.Context, key string, windowStart time.Time) error
	RatePurge(ctx context.Context, now time.Time, limit int) (int, error)
}

type Store interface {
	Messages
	Counters
	Checkpoint(ctx context.Context) error
	Close() error
}

type BlobInfo struct {
	Ref      string
	Modified time.Time
}

type Blobs interface {
	Put(ctx context.Context, ref string, r io.Reader, size int64) error
	Open(ctx context.Context, ref string) (io.ReadCloser, int64, error)
	Delete(ctx context.Context, ref string) error
	DeleteBatch(ctx context.Context, refs []string) []error
	List(ctx context.Context) ([]BlobInfo, error)
	PurgeTemp(ctx context.Context, olderThan time.Time) (int, error)
}

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
	LeaseDuration    = 120 * time.Second
	LeaseTimerSlack  = time.Second
	MaxLeaseDownload = 3
	TombstoneTTL     = 7 * 24 * time.Hour
	DailyFailCap     = 100
	FreeAttempts     = 5
	MaxBackoff       = time.Hour
	SweepBatch       = 1000
	ReconcileGrace   = 15 * time.Minute
)

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
	DayFailCount   int
	DayFailDate    string
	FailCountTotal int
	FirstOpenedAt  *time.Time
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
	Now           time.Time
	Bucket        []byte
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
}

type RateCount struct {
	Count int64
	Bytes int64
}

type SweepReport struct {
	Expired        int
	ExpiredLeases  int
	BlobsReleased  int
	Tombstones     int
	Counters       int
	Attempts       int
	OrphanedBlobs  int
	StaleTempFiles int
}

type Store interface {
	Create(ctx context.Context, m *Message) error
	Get(ctx context.Context, id string) (Record, error)
	Claim(ctx context.Context, p ClaimParams) (ClaimResult, error)
	Leases(ctx context.Context, id string) ([]Lease, error)
	RecordDownload(ctx context.Context, leaseRowID int64, now time.Time) (int, error)
	Ack(ctx context.Context, leaseRowID int64) error
	ReleaseIfDone(ctx context.Context, id string, now time.Time) (string, error)
	ClearBlob(ctx context.Context, id string, blobRef string, now time.Time) error
	End(ctx context.Context, id string, state State, now time.Time) (string, error)
	RateHit(ctx context.Context, hits []RateHit) ([]RateCount, error)
	RateGet(ctx context.Context, key string, windowStart time.Time) (RateCount, error)
	Sweep(ctx context.Context, now time.Time, deleteBlob func(ref string) error) (SweepReport, error)
	KnownBlobRefs(ctx context.Context) (map[string]struct{}, error)
	Close() error
}

type BlobInfo struct {
	Ref      string
	Modified time.Time
}

type Blobs interface {
	Put(ctx context.Context, ref string, r io.Reader, size int64) error
	Open(ctx context.Context, ref string) (io.ReadSeekCloser, int64, error)
	Delete(ctx context.Context, ref string) error
	List(ctx context.Context) ([]BlobInfo, error)
	PurgeTemp(ctx context.Context, olderThan time.Time) (int, error)
}

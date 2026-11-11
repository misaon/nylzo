package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/epoch"
	"github.com/misaon/nylzo/internal/seal"
)

type State string

const (
	StateAvailable State = api.StateAvailable
	StateOpened    State = api.StateOpened
	StateExpired   State = api.StateExpired
	StateCancelled State = api.StateCancelled
	StateDeleted   State = api.StateDeleted
)

const (
	LeaseDuration     = 120 * time.Second
	LeaseTimerSlack   = time.Second
	MaxLeaseDownload  = 3
	TombstoneTTL      = 7 * 24 * time.Hour
	MaxFailedAttempts = 100
	FreeAttempts      = 5
	MaxBackoff        = time.Hour
	SweepBatch        = 100
	CounterPurgeBatch = 1000
	BlobGrace         = 15 * time.Minute
	MinTransferRate   = 16000
	BlobPurgeStep     = 15 * time.Minute
	RowPurgeStep      = time.Hour
	ReportsPerMessage = 3
	MaxActiveLeases   = 8
)

func CeilMinute(moment time.Time) time.Time {
	return ceil(moment, time.Minute)
}

// ExpiresAt is when a message whose upload was stored at uploaded expires: ttl later, rounded up to the minute.
func ExpiresAt(uploaded time.Time, ttl time.Duration) time.Time {
	return CeilMinute(uploaded.UTC().Add(ttl))
}

// BlobPurgeAt is when the sweep deletes the blob of a message that expires at expiresAt.
func BlobPurgeAt(expiresAt time.Time) time.Time {
	return ceil(expiresAt, BlobPurgeStep)
}

func ceil(moment time.Time, step time.Duration) time.Time {
	if truncated := moment.Truncate(step); !truncated.Equal(moment) {
		return truncated.Add(step)
	}
	return moment
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
	ErrCorrupt      = errors.New("store: a record holds a value it cannot have")
	ErrPoolEmpty    = errors.New(
		"store: no prepared blob file was free, so the upload was refused and the pool refilled",
	)
)

type GoneError struct {
	State State
}

func (e *GoneError) Error() string {
	return fmt.Sprintf("store: message is gone (%s)", e.State)
}

// BusyError refuses a claim while MaxActiveLeases leases are active, before any open is spent.
type BusyError struct {
	RetryAfter time.Duration
}

func (e *BusyError) Error() string {
	return fmt.Sprintf("store: every lease slot of the message is in use (retry after %s)", e.RetryAfter)
}

type PassphraseError struct {
	RetryAfter   time.Duration
	AttemptsLeft int
}

func (e *PassphraseError) Error() string {
	return fmt.Sprintf("store: passphrase rejected (retry after %s, %d attempts left)", e.RetryAfter, e.AttemptsLeft)
}

// Message holds the fields of a sealed record; only a request that brings pk_R or pk_M can read them.
type Message struct {
	Version          uint8
	KDF              uint8
	Flags            uint8
	CreatedAt        time.Time
	ExpiresAt        time.Time
	TTLSeconds       uint32
	OpensMax         uint16
	OpensRemaining   uint16
	Size             int64
	VerifierPepperID byte
	VerifierMAC      []byte
	FailCountTotal   int
	NextAttemptAt    time.Time
	FirstOpenedAt    *time.Time
	Ending           State
	EndedAt          time.Time
	Leases           []Lease
	ReportDay        int64
	Reports          int
	// Epoch and RatchetID name the expiry share of the message (spec section 5.6).
	Epoch     uint32
	RatchetID epoch.RatchetID
}

func (m *Message) PassphraseRequired() bool {
	return len(m.VerifierMAC) > 0
}

type Lease struct {
	MAC       []byte
	PepperID  byte
	ExpiresAt time.Time
	Downloads int
}

func (l *Lease) Active(now time.Time) bool {
	return now.Before(l.ExpiresAt)
}

// Row is what the store keeps; only the locator, the blob reference and the two cleanup times are in the clear.
type Row struct {
	Locator       seal.Locator
	ReadWrap      []byte
	ManageWrap    []byte
	Record        []byte
	BlobReference string
	BlobWrap      []byte
	BlobPurgeAt   time.Time
	PurgeAt       time.Time
}

type DueBlob struct {
	Locator   seal.Locator
	Reference string
}

type ClaimParameters struct {
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
	Limit       int64
}

type RateCount struct {
	Count int64
}

type Messages interface {
	Create(ctx context.Context, row *Row, counters ...RateHit) error
	Load(ctx context.Context, locator seal.Locator) (Row, error)
	Lookup(ctx context.Context, locator seal.Locator) (Row, bool, error)
	Update(ctx context.Context, locator seal.Locator, change func(row *Row) error) error
	DueBlobs(ctx context.Context, now time.Time, after seal.Locator, limit int) ([]DueBlob, error)
	ShredBlobs(ctx context.Context, due []DueBlob) error
	ForgetBlobs(ctx context.Context, due []DueBlob) error
	PurgeRows(ctx context.Context, now time.Time, limit int) (int, error)
	KnownBlobReferences(ctx context.Context) (map[string]struct{}, error)
}

type Counters interface {
	RateHit(ctx context.Context, hits []RateHit) ([]RateCount, error)
	RateGet(ctx context.Context, key string, windowStart time.Time) (RateCount, error)
	RateReserve(ctx context.Context, hit RateHit) (int64, error)
	RateRelease(ctx context.Context, key string, windowStart time.Time) error
	RateCopy(ctx context.Context, sourceKey, targetKey string, windowStart time.Time) error
	RatePurge(ctx context.Context, now time.Time, limit int) (int, error)
}

type Store interface {
	Messages
	Counters
	Checkpoint(ctx context.Context) error
}

// BlobInfo names a stored blob and the blob_purge_at its file carries.
type BlobInfo struct {
	Reference string
	PurgeAt   time.Time
}

type Blobs interface {
	// PutExpiring stores the content and reports when it was stored; the blob carries the BlobPurgeAt of a message
	// stored then with ttl.
	PutExpiring(
		ctx context.Context,
		reference string,
		content io.Reader,
		size int64,
		ttl time.Duration,
	) (time.Time, error)
	Open(ctx context.Context, reference string) (io.ReadCloser, int64, error)
	Delete(ctx context.Context, reference string) error
	DeleteBatch(ctx context.Context, references []string) []error
	List(ctx context.Context) ([]BlobInfo, error)
	PurgeTemporary(ctx context.Context, olderThan time.Time) (int, error)
	Replenish(ctx context.Context) error
	Restamp(ctx context.Context, now time.Time) error
}

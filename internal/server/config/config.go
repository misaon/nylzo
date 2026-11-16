package config

import (
	"errors"
	"fmt"
	"math"
	"net/netip"
	"path/filepath"
	"slices"
	"time"

	"github.com/misaon/nylzo/internal/buildinfo"
	"github.com/misaon/nylzo/internal/core/envelope"
	"github.com/misaon/nylzo/internal/core/proofofwork"
	"github.com/misaon/nylzo/internal/server/diskspace"
	"github.com/misaon/nylzo/internal/server/operatoraddress"
)

const (
	proxyModeNone     = ""
	ProxyModeScaleway = "scaleway"
	MaxPoWConcurrency = 4
	MaxPoWDifficulty  = proofofwork.MaxDifficulty
	MaxOpens          = 100
	MaxTTL            = 7 * 24 * time.Hour
	MaxStreams        = 1024
)

var ErrInvalid = errors.New("server: invalid configuration")

type Limits struct {
	MaxFileBytes int64
	MaxTTL       time.Duration
	DefaultTTL   time.Duration
	MaxOpens     int
}

// MaxObjectBytes is the largest envelope that an attachment of MaxFileBytes seals into.
func (l Limits) MaxObjectBytes() int64 {
	return envelope.ObjectLimit(l.MaxFileBytes)
}

type Rates struct {
	CreatePerHour      int64
	CreatePerDay       int64
	CreateBytesPerDay  int64
	ReadPerHour        int64
	ReadBytesPerHour   int64
	CoarseMultiplier   int64
	BucketPerSecond    float64
	BucketBurst        float64
	SourcePerSecond    float64
	SourceBurst        float64
	GlobalDailyCreates int64
}

type Server struct {
	Listen                 string
	DataDirectory          string
	EpochDirectoryOverride string
	Pepper                 []byte
	PepperPrevious         []byte
	TrustedProxies         []netip.Prefix
	// EdgeReports tells that the trusted proxy is the nylzo frontend, which reports the pages it serves and keeps
	// clients off the address it reports to.
	EdgeReports    bool
	ProxyMode      string
	SweepInterval  time.Duration
	Limits         Limits
	Rates          Rates
	PoWDifficulty  int
	PoWConcurrency int
	MaxStreams     int
	MinFreeBytes   int64
	Operator       Operator
	PublicURL      string
	Version        string
	Build          buildinfo.Info
	Development    Development
}

// Development holds the opt-outs a developer machine needs; each one weakens what a stolen server reveals.
type Development struct {
	AllowSwap              bool
	AllowUnerasableStorage bool
	AllowUnerasedMemory    bool
}

func Defaults() Server {
	return Server{
		Listen:        "127.0.0.1:8080",
		DataDirectory: "data",
		SweepInterval: 5 * time.Minute,
		Limits: Limits{
			MaxFileBytes: envelope.MaxFileBytes,
			MaxTTL:       MaxTTL,
			DefaultTTL:   24 * time.Hour,
			MaxOpens:     10,
		},
		Rates: Rates{
			CreatePerHour:     20,
			CreatePerDay:      100,
			CreateBytesPerDay: 50_000_000,
			ReadPerHour:       300,
			ReadBytesPerHour:  256 << 20,
			CoarseMultiplier:  5,
			BucketPerSecond:   500,
			BucketBurst:       1000,
			SourcePerSecond:   10,
			SourceBurst:       20,
		},
		PoWDifficulty:  4,
		PoWConcurrency: 4,
		MaxStreams:     256,
		MinFreeBytes:   1 << 30,
		Version:        "dev",
	}
}

type rule struct {
	holds   bool
	message string
}

// EpochDirectory is where the epoch seed lives; keep it out of every backup (spec section 5.6).
func (s *Server) EpochDirectory() string {
	if s.EpochDirectoryOverride != "" {
		return s.EpochDirectoryOverride
	}
	return s.DataEpochDirectory()
}

// DataEpochDirectory is the default epoch directory inside the data directory, which every backup of it copies.
func (s *Server) DataEpochDirectory() string {
	return filepath.Join(s.DataDirectory, "epoch")
}

func (s *Server) Validate() error {
	for _, rule := range s.rules() {
		if !rule.holds {
			return fmt.Errorf("%w: %s", ErrInvalid, rule.message)
		}
	}
	if err := s.Operator.validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if s.PublicURL == "" {
		return nil
	}
	if err := operatoraddress.CheckPublicURL(s.PublicURL); err != nil {
		return fmt.Errorf("%w: NYLZO_PUBLIC_URL: %w", ErrInvalid, err)
	}
	return nil
}

func (s *Server) rules() []rule {
	minTTL := envelope.MinTTLSeconds * time.Second
	limits, rates := &s.Limits, &s.Rates
	return []rule{
		{
			limits.MaxFileBytes >= 1 && limits.MaxFileBytes <= envelope.MaxFileBytes,
			fmt.Sprintf("NYLZO_MAX_FILE_BYTES must be between 1 and %d", envelope.MaxFileBytes),
		},
		{
			limits.MaxOpens >= 1 && limits.MaxOpens <= MaxOpens,
			fmt.Sprintf("NYLZO_MAX_OPENS must be between 1 and %d", MaxOpens),
		},
		{
			limits.MaxTTL >= minTTL && limits.MaxTTL <= MaxTTL,
			fmt.Sprintf("NYLZO_MAX_TTL must be between %s and %s", minTTL, MaxTTL),
		},
		{
			limits.DefaultTTL >= minTTL && limits.DefaultTTL <= limits.MaxTTL,
			fmt.Sprintf("NYLZO_DEFAULT_TTL must be between %s and NYLZO_MAX_TTL", minTTL),
		},
		{
			s.PoWDifficulty >= 0 && s.PoWDifficulty <= MaxPoWDifficulty,
			fmt.Sprintf("NYLZO_POW_DIFFICULTY must be between 0 and %d", MaxPoWDifficulty),
		},
		{
			s.PoWConcurrency >= 1 && s.PoWConcurrency <= MaxPoWConcurrency,
			fmt.Sprintf("NYLZO_POW_CONCURRENCY must be between 1 and %d", MaxPoWConcurrency),
		},
		{
			s.MaxStreams >= 1 && s.MaxStreams <= MaxStreams,
			fmt.Sprintf("NYLZO_MAX_STREAMS must be between 1 and %d", MaxStreams),
		},
		{s.SweepInterval >= 0, "NYLZO_SWEEP_INTERVAL must not be negative"},
		{s.MinFreeBytes >= 0, "NYLZO_MIN_FREE_BYTES must not be negative"},
		{
			s.MinFreeBytes == 0 || diskspace.Measurable,
			"NYLZO_MIN_FREE_BYTES must be 0 on a system where nylzo cannot measure free space",
		},
		{
			s.ProxyMode == proxyModeNone || s.ProxyMode == ProxyModeScaleway,
			"NYLZO_PROXY_MODE must be empty or scaleway",
		},
		{
			rates.BucketPerSecond > 0 && rates.SourcePerSecond > 0,
			"NYLZO_RATE_BUCKET_RPS and NYLZO_RATE_SOURCE_RPS must be positive",
		},
		{rates.BucketBurst >= 1, "NYLZO_RATE_BUCKET_BURST must be at least 1"},
		{rates.SourceBurst >= 1, "NYLZO_RATE_SOURCE_BURST must be at least 1"},
		{
			rates.ReadBytesPerHour == 0 || rates.ReadBytesPerHour >= limits.MaxObjectBytes(),
			"NYLZO_RATE_READ_BYTES_HOUR must be 0 or at least the largest object, or no such message could be read",
		},
		{
			slices.Min(rates.counted()) >= 0 && rates.GlobalDailyCreates >= 0 && rates.CoarseMultiplier >= 0,
			"the NYLZO_RATE_* limits must not be negative",
		},
		{
			rates.CoarseMultiplier == 0 || slices.Max(rates.counted()) <= math.MaxInt64/rates.CoarseMultiplier,
			"a NYLZO_RATE_* limit times NYLZO_RATE_COARSE_MULTIPLIER does not fit in 64 bits",
		},
	}
}

func (r *Rates) counted() []int64 {
	return []int64{
		r.CreatePerHour,
		r.CreatePerDay,
		r.CreateBytesPerDay,
		r.ReadPerHour,
		r.ReadBytesPerHour,
	}
}

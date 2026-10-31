package config

import (
	"crypto/hpke"
	"errors"
	"fmt"
	"math"
	"net/netip"
	"slices"
	"time"

	"github.com/misaon/nylzo/internal/buildinfo"
	"github.com/misaon/nylzo/internal/core/envelope"
	"github.com/misaon/nylzo/internal/core/proofofwork"
	"github.com/misaon/nylzo/internal/server/diskspace"
	"github.com/misaon/nylzo/internal/server/webclient"
)

const (
	ProxyModeNone     = ""
	ProxyModeScaleway = "scaleway"
	MaxPoWConcurrency = 4
	MaxPoWDifficulty  = proofofwork.MaxDifficulty
	MaxOpens          = 100
	MaxTTL            = 7 * 24 * time.Hour
	MaxStreams        = 1024
)

var ErrInvalid = errors.New("server: invalid configuration")

type Limits struct {
	MaxFileBytes   int64
	MaxObjectBytes int64
	MaxTTL         time.Duration
	DefaultTTL     time.Duration
	MaxOpens       int
}

type Rates struct {
	CreatePerHour      int64
	CreatePerDay       int64
	CreateBytesPerDay  int64
	ReadPerHour        int64
	AbusePerHour       int64
	CoarseMultiplier   int64
	BucketPerSecond    float64
	BucketBurst        float64
	SourcePerSecond    float64
	SourceBurst        float64
	GlobalDailyCreates int64
}

type Server struct {
	Listen         string
	DataDir        string
	Pepper         []byte
	PepperPrevious []byte
	ReportKey      hpke.PublicKey
	TrustedProxies []netip.Prefix
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
}

func Defaults() Server {
	return Server{
		Listen:        "127.0.0.1:8080",
		DataDir:       "data",
		SweepInterval: 5 * time.Minute,
		Limits: Limits{
			MaxFileBytes:   envelope.MaxFileBytes,
			MaxObjectBytes: envelope.ObjectLimit(envelope.MaxFileBytes),
			MaxTTL:         MaxTTL,
			DefaultTTL:     24 * time.Hour,
			MaxOpens:       10,
		},
		Rates: Rates{
			CreatePerHour:     20,
			CreatePerDay:      100,
			CreateBytesPerDay: 50_000_000,
			ReadPerHour:       300,
			AbusePerHour:      5,
			CoarseMultiplier:  5,
			BucketPerSecond:   50,
			BucketBurst:       100,
			SourcePerSecond:   10,
			SourceBurst:       20,
		},
		PoWDifficulty:  4,
		PoWConcurrency: 4,
		MaxStreams:     64,
		MinFreeBytes:   1 << 30,
		Version:        "dev",
	}
}

type rule struct {
	holds   bool
	message string
}

// Validate checks a configuration that serves requests, which needs the operator's report key.
func (s *Server) Validate() error {
	if err := s.validateWithoutReports(); err != nil {
		return err
	}
	if s.ReportKey == nil {
		return fmt.Errorf("%w: NYLZO_REPORT_KEY is required; create a key pair with nylzo report-key", ErrInvalid)
	}
	return nil
}

func (s *Server) validateWithoutReports() error {
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
	if err := webclient.CheckPublicURL(s.PublicURL); err != nil {
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
			limits.MaxObjectBytes == envelope.ObjectLimit(limits.MaxFileBytes),
			"the object limit must follow NYLZO_MAX_FILE_BYTES",
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
			s.ProxyMode == ProxyModeNone || s.ProxyMode == ProxyModeScaleway,
			"NYLZO_PROXY_MODE must be empty or scaleway",
		},
		{
			rates.BucketPerSecond > 0 && rates.SourcePerSecond > 0,
			"NYLZO_RATE_BUCKET_RPS and NYLZO_RATE_SOURCE_RPS must be positive",
		},
		{rates.BucketBurst >= 1, "NYLZO_RATE_BUCKET_BURST must be at least 1"},
		{rates.SourceBurst >= 1, "NYLZO_RATE_SOURCE_BURST must be at least 1"},
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
	return []int64{r.CreatePerHour, r.CreatePerDay, r.CreateBytesPerDay, r.ReadPerHour, r.AbusePerHour}
}

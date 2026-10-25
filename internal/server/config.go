package server

import (
	"errors"
	"fmt"
	"io"
	"math"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/pepper"
)

const (
	ProxyModeNone     = ""
	ProxyModeScaleway = "scaleway"
	MaxPoWConcurrency = 4
	MaxPoWDifficulty  = core.MaxPoWDifficulty
	MaxOpens          = 100
	MaxTTL            = 7 * 24 * time.Hour
	MaxStreams        = 1024
)

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

type Config struct {
	Listen         string
	DataDir        string
	Pepper         []byte
	PepperPrevious []byte
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
}

func Defaults() Config {
	return Config{
		Listen:        ":8080",
		DataDir:       "data",
		SweepInterval: 5 * time.Minute,
		Limits: Limits{
			MaxFileBytes:   core.MaxFileBytes,
			MaxObjectBytes: core.ObjectLimit(core.MaxFileBytes),
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

var ErrConfig = errors.New("server: invalid configuration")

const maxSecretFileBytes = 4096

type envFunc func(string) string

func FromEnv(getenv envFunc, needPepper bool) (Config, error) {
	c := Defaults()
	var err error
	set := func(key string, apply func(string) error) {
		if err != nil {
			return
		}
		v := strings.TrimSpace(getenv(key))
		if v == "" {
			return
		}
		if aerr := apply(v); aerr != nil {
			err = fmt.Errorf("%w: %s: %v", ErrConfig, key, aerr)
		}
	}
	if port := strings.TrimSpace(getenv("PORT")); port != "" {
		c.Listen = ":" + port
	}
	set("NYLZO_LISTEN", func(v string) error { c.Listen = v; return nil })
	set("NYLZO_DATA_DIR", func(v string) error { c.DataDir = v; return nil })
	setSecret := func(key string, dst *[]byte) {
		if err != nil {
			return
		}
		b, serr := secretFromEnv(getenv, key)
		if serr != nil {
			err = fmt.Errorf("%w: %v", ErrConfig, serr)
			return
		}
		*dst = b
	}
	setSecret("NYLZO_PEPPER", &c.Pepper)
	setSecret("NYLZO_PEPPER_PREVIOUS", &c.PepperPrevious)
	set("NYLZO_TRUSTED_PROXIES", func(v string) error {
		for part := range strings.SplitSeq(v, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if !strings.Contains(part, "/") {
				addr, perr := netip.ParseAddr(part)
				if perr != nil {
					return perr
				}
				part = netip.PrefixFrom(addr, addr.BitLen()).String()
			}
			p, perr := netip.ParsePrefix(part)
			if perr != nil {
				return perr
			}
			if p = p.Masked(); p.Addr().Is4In6() {
				p = netip.PrefixFrom(p.Addr().Unmap(), p.Bits()-96)
			}
			c.TrustedProxies = append(c.TrustedProxies, p)
		}
		return nil
	})
	set("NYLZO_PROXY_MODE", func(v string) error {
		switch v {
		case ProxyModeScaleway:
			c.ProxyMode = v
			return nil
		case "none", "off":
			c.ProxyMode = ProxyModeNone
			return nil
		}
		return fmt.Errorf("unknown mode %q", v)
	})
	set("NYLZO_PASSPHRASE_IP_SCOPE", func(string) error {
		return errors.New("removed: passphrase limits are kept per message, without address buckets")
	})
	set("NYLZO_SWEEP_INTERVAL", func(v string) error { return parseDuration(v, &c.SweepInterval) })
	set("NYLZO_MAX_FILE_BYTES", func(v string) error { return parseInt64(v, &c.Limits.MaxFileBytes) })
	set("NYLZO_MAX_TTL", func(v string) error { return parseDuration(v, &c.Limits.MaxTTL) })
	set("NYLZO_DEFAULT_TTL", func(v string) error { return parseDuration(v, &c.Limits.DefaultTTL) })
	set("NYLZO_MAX_OPENS", func(v string) error { return parseInt(v, &c.Limits.MaxOpens) })
	set("NYLZO_RATE_CREATE_HOUR", func(v string) error { return parseInt64(v, &c.Rates.CreatePerHour) })
	set("NYLZO_RATE_CREATE_DAY", func(v string) error { return parseInt64(v, &c.Rates.CreatePerDay) })
	set("NYLZO_RATE_CREATE_BYTES_DAY", func(v string) error { return parseInt64(v, &c.Rates.CreateBytesPerDay) })
	set("NYLZO_RATE_READ_HOUR", func(v string) error { return parseInt64(v, &c.Rates.ReadPerHour) })
	set("NYLZO_RATE_ABUSE_HOUR", func(v string) error { return parseInt64(v, &c.Rates.AbusePerHour) })
	set("NYLZO_RATE_COARSE_MULTIPLIER", func(v string) error { return parseInt64(v, &c.Rates.CoarseMultiplier) })
	set("NYLZO_RATE_BUCKET_RPS", func(v string) error { return parseFloat(v, &c.Rates.BucketPerSecond) })
	set("NYLZO_RATE_BUCKET_BURST", func(v string) error { return parseFloat(v, &c.Rates.BucketBurst) })
	set("NYLZO_RATE_SOURCE_RPS", func(v string) error { return parseFloat(v, &c.Rates.SourcePerSecond) })
	set("NYLZO_RATE_SOURCE_BURST", func(v string) error { return parseFloat(v, &c.Rates.SourceBurst) })
	set("NYLZO_GLOBAL_DAILY_CREATES", func(v string) error { return parseInt64(v, &c.Rates.GlobalDailyCreates) })
	set("NYLZO_POW_DIFFICULTY", func(v string) error { return parseInt(v, &c.PoWDifficulty) })
	set("NYLZO_POW_CONCURRENCY", func(v string) error { return parseInt(v, &c.PoWConcurrency) })
	set("NYLZO_MAX_STREAMS", func(v string) error { return parseInt(v, &c.MaxStreams) })
	set("NYLZO_MIN_FREE_BYTES", func(v string) error { return parseInt64(v, &c.MinFreeBytes) })
	set("NYLZO_OPERATOR", func(v string) error { c.Operator.Name = v; return nil })
	set("NYLZO_CONTACT", func(v string) error { c.Operator.Contact = v; return nil })
	set("NYLZO_HOSTING", func(v string) error { c.Operator.Hosting = v; return nil })
	set("NYLZO_PUBLIC_URL", func(v string) error { c.PublicURL = v; return CheckPublicURL(v) })
	if err != nil {
		return c, err
	}
	c.Limits.MaxObjectBytes = core.ObjectLimit(c.Limits.MaxFileBytes)
	if needPepper && len(c.Pepper) == 0 {
		return c, fmt.Errorf(
			"%w: NYLZO_PEPPER or NYLZO_PEPPER_FILE is required (32 bytes, hex or base64url)",
			ErrConfig,
		)
	}
	return c, c.Validate()
}

func secretFromEnv(getenv envFunc, key string) ([]byte, error) {
	value := strings.TrimSpace(getenv(key))
	path := strings.TrimSpace(getenv(key + "_FILE"))
	switch {
	case value != "" && path != "":
		return nil, fmt.Errorf("%s and %s_FILE are both set", key, key)
	case path != "":
		raw, err := readSecretFile(path)
		if err != nil {
			return nil, fmt.Errorf("%s_FILE: %w", key, err)
		}
		b, err := pepper.ParseSecret(strings.TrimSpace(string(raw)))
		if err != nil {
			return nil, fmt.Errorf("%s_FILE: %w", key, err)
		}
		return b, nil
	case value != "":
		b, err := pepper.ParseSecret(value)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		return b, nil
	}
	return nil, nil
}

func readSecretFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxSecretFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxSecretFileBytes {
		return nil, errors.New("the file is larger than a secret can be")
	}
	return raw, nil
}

func (c Config) Validate() error {
	minTTL := core.MinTTLSeconds * time.Second
	r := c.Rates
	for _, check := range []struct {
		name string
		ok   bool
	}{
		{fmt.Sprintf("NYLZO_MAX_FILE_BYTES must be between 1 and %d", core.MaxFileBytes), c.Limits.MaxFileBytes >= 1 && c.Limits.MaxFileBytes <= core.MaxFileBytes},
		{"the object limit must follow NYLZO_MAX_FILE_BYTES", c.Limits.MaxObjectBytes == core.ObjectLimit(c.Limits.MaxFileBytes)},
		{fmt.Sprintf("NYLZO_MAX_OPENS must be between 1 and %d", MaxOpens), c.Limits.MaxOpens >= 1 && c.Limits.MaxOpens <= MaxOpens},
		{fmt.Sprintf("NYLZO_MAX_TTL must be between %s and %s", minTTL, MaxTTL), c.Limits.MaxTTL >= minTTL && c.Limits.MaxTTL <= MaxTTL},
		{fmt.Sprintf("NYLZO_DEFAULT_TTL must be between %s and NYLZO_MAX_TTL", minTTL), c.Limits.DefaultTTL >= minTTL && c.Limits.DefaultTTL <= c.Limits.MaxTTL},
		{fmt.Sprintf("NYLZO_POW_DIFFICULTY must be between 0 and %d", MaxPoWDifficulty), c.PoWDifficulty >= 0 && c.PoWDifficulty <= MaxPoWDifficulty},
		{fmt.Sprintf("NYLZO_POW_CONCURRENCY must be between 1 and %d", MaxPoWConcurrency), c.PoWConcurrency >= 1 && c.PoWConcurrency <= MaxPoWConcurrency},
		{fmt.Sprintf("NYLZO_MAX_STREAMS must be between 1 and %d", MaxStreams), c.MaxStreams >= 1 && c.MaxStreams <= MaxStreams},
		{"NYLZO_SWEEP_INTERVAL must not be negative", c.SweepInterval >= 0},
		{"NYLZO_MIN_FREE_BYTES must not be negative", c.MinFreeBytes >= 0},
		{"NYLZO_MIN_FREE_BYTES must be 0 on a system where nylzo cannot measure free space", c.MinFreeBytes == 0 || freeSpaceSupported},
		{"NYLZO_PROXY_MODE must be empty or scaleway", c.ProxyMode == ProxyModeNone || c.ProxyMode == ProxyModeScaleway},
		{"NYLZO_RATE_BUCKET_RPS and NYLZO_RATE_SOURCE_RPS must be positive", r.BucketPerSecond > 0 && r.SourcePerSecond > 0},
		{"NYLZO_RATE_BUCKET_BURST must be at least 1", r.BucketBurst >= 1},
		{"NYLZO_RATE_SOURCE_BURST must be at least 1", r.SourceBurst >= 1},
		{"the NYLZO_RATE_* limits must not be negative", min(r.CreatePerHour, r.CreatePerDay, r.CreateBytesPerDay, r.ReadPerHour, r.AbusePerHour, r.CoarseMultiplier, r.GlobalDailyCreates) >= 0},
		{"a NYLZO_RATE_* limit times NYLZO_RATE_COARSE_MULTIPLIER does not fit in 64 bits", r.CoarseMultiplier == 0 || max(r.CreatePerHour, r.CreatePerDay, r.CreateBytesPerDay, r.ReadPerHour, r.AbusePerHour) <= math.MaxInt64/r.CoarseMultiplier},
	} {
		if !check.ok {
			return fmt.Errorf("%w: %s", ErrConfig, check.name)
		}
	}
	if err := c.Operator.validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrConfig, err)
	}
	if c.PublicURL != "" {
		if err := CheckPublicURL(c.PublicURL); err != nil {
			return fmt.Errorf("%w: NYLZO_PUBLIC_URL: %v", ErrConfig, err)
		}
	}
	return nil
}

func parseDuration(v string, out *time.Duration) error {
	if secs, err := strconv.ParseInt(v, 10, 64); err == nil {
		if secs < 0 || secs > int64(math.MaxInt64/time.Second) {
			return fmt.Errorf("want 0 to %d seconds, got %q", int64(math.MaxInt64/time.Second), v)
		}
		*out = time.Duration(secs) * time.Second
		return nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return err
	}
	*out = d
	return nil
}

func parseInt64(v string, out *int64) error {
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 {
		return fmt.Errorf("want a non-negative integer, got %q", v)
	}
	*out = n
	return nil
}

func parseInt(v string, out *int) error {
	var n int64
	if err := parseInt64(v, &n); err != nil {
		return err
	}
	if n > math.MaxInt {
		return fmt.Errorf("want at most %d, got %q", math.MaxInt, v)
	}
	*out = int(n)
	return nil
}

func parseFloat(v string, out *float64) error {
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f <= 0 || math.IsInf(f, 0) || math.IsNaN(f) {
		return fmt.Errorf("want a positive number, got %q", v)
	}
	*out = f
	return nil
}

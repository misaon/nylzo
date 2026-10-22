package server

import (
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/pepper"
)

const (
	ProxyModeNone     = ""
	ProxyModeScaleway = "scaleway"
)

type Limits struct {
	MaxFileBytes   int64
	MaxTextBytes   int64
	MaxObjectBytes int64
	MaxTTL         time.Duration
	DefaultTTL     time.Duration
	MaxOpens       int
	DefaultKDF     core.KDF
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
	GlobalDailyCreates int64
}

type Config struct {
	Listen            string
	DataDir           string
	Pepper            []byte
	PepperPrevious    []byte
	TrustedProxies    []netip.Prefix
	ProxyMode         string
	PassphraseIPScope bool
	SweepInterval     time.Duration
	Limits            Limits
	Rates             Rates
	PoWDifficulty     int
	PoWConcurrency    int
	PublicURL         string
	Version           string
}

func Defaults() Config {
	return Config{
		Listen:            ":8080",
		DataDir:           "data",
		PassphraseIPScope: true,
		SweepInterval:     5 * time.Minute,
		Limits: Limits{
			MaxFileBytes:   core.MaxFileBytes,
			MaxTextBytes:   core.MaxTextBytes,
			MaxObjectBytes: core.MaxObjectBytes,
			MaxTTL:         7 * 24 * time.Hour,
			DefaultTTL:     24 * time.Hour,
			MaxOpens:       10,
			DefaultKDF:     core.KDFScrypt,
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
		},
		PoWDifficulty:  4,
		PoWConcurrency: 4,
		Version:        "dev",
	}
}

var ErrConfig = errors.New("server: invalid configuration")

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
	set("NYLZO_PEPPER", func(v string) error {
		b, perr := pepper.ParseSecret(v)
		c.Pepper = b
		return perr
	})
	set("NYLZO_PEPPER_PREVIOUS", func(v string) error {
		b, perr := pepper.ParseSecret(v)
		c.PepperPrevious = b
		return perr
	})
	set("NYLZO_TRUSTED_PROXIES", func(v string) error {
		for _, part := range strings.Split(v, ",") {
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
			c.TrustedProxies = append(c.TrustedProxies, p.Masked())
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
	set("NYLZO_PASSPHRASE_IP_SCOPE", func(v string) error {
		switch v {
		case "on", "true", "1":
			c.PassphraseIPScope = true
		case "off", "false", "0":
			c.PassphraseIPScope = false
		default:
			return fmt.Errorf("want on or off, got %q", v)
		}
		return nil
	})
	set("NYLZO_SWEEP_INTERVAL", func(v string) error { return parseDuration(v, &c.SweepInterval) })
	set("NYLZO_MAX_FILE_BYTES", func(v string) error { return parseInt64(v, &c.Limits.MaxFileBytes) })
	set("NYLZO_MAX_TTL", func(v string) error { return parseDuration(v, &c.Limits.MaxTTL) })
	set("NYLZO_DEFAULT_TTL", func(v string) error { return parseDuration(v, &c.Limits.DefaultTTL) })
	set("NYLZO_MAX_OPENS", func(v string) error { return parseInt(v, &c.Limits.MaxOpens) })
	set("NYLZO_DEFAULT_KDF", func(v string) error {
		k, kerr := core.ParseKDF(v)
		c.Limits.DefaultKDF = k
		return kerr
	})
	set("NYLZO_RATE_CREATE_HOUR", func(v string) error { return parseInt64(v, &c.Rates.CreatePerHour) })
	set("NYLZO_RATE_CREATE_DAY", func(v string) error { return parseInt64(v, &c.Rates.CreatePerDay) })
	set("NYLZO_RATE_CREATE_BYTES_DAY", func(v string) error { return parseInt64(v, &c.Rates.CreateBytesPerDay) })
	set("NYLZO_RATE_READ_HOUR", func(v string) error { return parseInt64(v, &c.Rates.ReadPerHour) })
	set("NYLZO_RATE_ABUSE_HOUR", func(v string) error { return parseInt64(v, &c.Rates.AbusePerHour) })
	set("NYLZO_RATE_COARSE_MULTIPLIER", func(v string) error { return parseInt64(v, &c.Rates.CoarseMultiplier) })
	set("NYLZO_RATE_BUCKET_RPS", func(v string) error { return parseFloat(v, &c.Rates.BucketPerSecond) })
	set("NYLZO_RATE_BUCKET_BURST", func(v string) error { return parseFloat(v, &c.Rates.BucketBurst) })
	set("NYLZO_GLOBAL_DAILY_CREATES", func(v string) error { return parseInt64(v, &c.Rates.GlobalDailyCreates) })
	set("NYLZO_POW_DIFFICULTY", func(v string) error { return parseInt(v, &c.PoWDifficulty) })
	set("NYLZO_POW_CONCURRENCY", func(v string) error { return parseInt(v, &c.PoWConcurrency) })
	set("NYLZO_PUBLIC_URL", func(v string) error { c.PublicURL = strings.TrimSuffix(v, "/"); return nil })
	if err != nil {
		return c, err
	}
	c.Limits.MaxObjectBytes = c.Limits.MaxFileBytes + core.MaxTextBytes + core.PadFile + 64
	if needPepper && len(c.Pepper) == 0 {
		return c, fmt.Errorf("%w: NYLZO_PEPPER is required (32 bytes, hex or base64url)", ErrConfig)
	}
	if c.Limits.MaxOpens < 1 || c.Limits.MaxOpens > 65535 || c.Limits.MaxTTL < core.MinTTLSeconds*time.Second || c.Limits.DefaultTTL > c.Limits.MaxTTL || c.PoWDifficulty < 0 || c.PoWDifficulty > 32 || c.PoWConcurrency < 1 {
		return c, fmt.Errorf("%w: limits out of range", ErrConfig)
	}
	return c, nil
}

func parseDuration(v string, out *time.Duration) error {
	if secs, err := strconv.ParseInt(v, 10, 64); err == nil {
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
	*out = int(n)
	return nil
}

func parseFloat(v string, out *float64) error {
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f <= 0 {
		return fmt.Errorf("want a positive number, got %q", v)
	}
	*out = f
	return nil
}

package config

import (
	"errors"
	"fmt"
	"io"
	"math"
	"net/netip"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/misaon/nylzo/internal/core/envelope"
	"github.com/misaon/nylzo/internal/pepper"
	"github.com/misaon/nylzo/internal/server/abuse"
	"github.com/misaon/nylzo/internal/server/webclient"
)

const maxSecretFileBytes = 4096

type variable struct {
	name  string
	apply func(value string) error
}

func FromEnv(getenv func(string) string, needPepper bool) (Server, error) {
	settings := Defaults()
	if port := strings.TrimSpace(getenv("PORT")); port != "" {
		settings.Listen = ":" + port
	}
	if err := settings.readSecrets(getenv); err != nil {
		return settings, err
	}
	for _, variable := range settings.variables() {
		value := strings.TrimSpace(getenv(variable.name))
		if value == "" {
			continue
		}
		if err := variable.apply(value); err != nil {
			return settings, fmt.Errorf("%w: %s: %w", ErrInvalid, variable.name, err)
		}
	}
	settings.Limits.MaxObjectBytes = envelope.ObjectLimit(settings.Limits.MaxFileBytes)
	if !needPepper {
		err := settings.validateWithoutReports()
		return settings, err
	}
	if len(settings.Pepper) == 0 {
		return settings, fmt.Errorf(
			"%w: NYLZO_PEPPER or NYLZO_PEPPER_FILE is required (32 bytes, hex or base64url)",
			ErrInvalid,
		)
	}
	err := settings.Validate()
	return settings, err
}

func (s *Server) readSecrets(getenv func(string) string) error {
	var err error
	if s.Pepper, err = secretFromEnv(getenv, "NYLZO_PEPPER"); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if s.PepperPrevious, err = secretFromEnv(getenv, "NYLZO_PEPPER_PREVIOUS"); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	return nil
}

func (s *Server) variables() []variable {
	text := func(target *string) func(string) error {
		return func(value string) error { *target = value; return nil }
	}
	return []variable{
		{"NYLZO_LISTEN", text(&s.Listen)},
		{"NYLZO_DATA_DIR", text(&s.DataDir)},
		{"NYLZO_TRUSTED_PROXIES", s.parseTrustedProxies},
		{"NYLZO_PROXY_MODE", s.parseProxyMode},
		{"NYLZO_PASSPHRASE_IP_SCOPE", func(string) error {
			return errors.New("removed: passphrase limits are kept per message, without address buckets")
		}},
		{"NYLZO_SWEEP_INTERVAL", durationInto(&s.SweepInterval)},
		{"NYLZO_MAX_FILE_BYTES", int64Into(&s.Limits.MaxFileBytes)},
		{"NYLZO_MAX_TTL", durationInto(&s.Limits.MaxTTL)},
		{"NYLZO_DEFAULT_TTL", durationInto(&s.Limits.DefaultTTL)},
		{"NYLZO_MAX_OPENS", intInto(&s.Limits.MaxOpens)},
		{"NYLZO_RATE_CREATE_HOUR", int64Into(&s.Rates.CreatePerHour)},
		{"NYLZO_RATE_CREATE_DAY", int64Into(&s.Rates.CreatePerDay)},
		{"NYLZO_RATE_CREATE_BYTES_DAY", int64Into(&s.Rates.CreateBytesPerDay)},
		{"NYLZO_RATE_READ_HOUR", int64Into(&s.Rates.ReadPerHour)},
		{"NYLZO_RATE_ABUSE_HOUR", int64Into(&s.Rates.AbusePerHour)},
		{"NYLZO_RATE_COARSE_MULTIPLIER", int64Into(&s.Rates.CoarseMultiplier)},
		{"NYLZO_RATE_BUCKET_RPS", floatInto(&s.Rates.BucketPerSecond)},
		{"NYLZO_RATE_BUCKET_BURST", floatInto(&s.Rates.BucketBurst)},
		{"NYLZO_RATE_SOURCE_RPS", floatInto(&s.Rates.SourcePerSecond)},
		{"NYLZO_RATE_SOURCE_BURST", floatInto(&s.Rates.SourceBurst)},
		{"NYLZO_GLOBAL_DAILY_CREATES", int64Into(&s.Rates.GlobalDailyCreates)},
		{"NYLZO_POW_DIFFICULTY", intInto(&s.PoWDifficulty)},
		{"NYLZO_POW_CONCURRENCY", intInto(&s.PoWConcurrency)},
		{"NYLZO_MAX_STREAMS", intInto(&s.MaxStreams)},
		{"NYLZO_MIN_FREE_BYTES", int64Into(&s.MinFreeBytes)},
		{"NYLZO_OPERATOR", text(&s.Operator.Name)},
		{"NYLZO_CONTACT", text(&s.Operator.Contact)},
		{"NYLZO_HOSTING", text(&s.Operator.Hosting)},
		{"NYLZO_REPORT_KEY", func(value string) error {
			key, err := abuse.ParsePublicKey(value)
			s.ReportKey = key
			return err
		}},
		{"NYLZO_PUBLIC_URL", func(value string) error {
			s.PublicURL = value
			return webclient.CheckPublicURL(value)
		}},
	}
}

func (s *Server) parseTrustedProxies(value string) error {
	for part := range strings.SplitSeq(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		prefix, err := parseProxy(part)
		if err != nil {
			return err
		}
		s.TrustedProxies = append(s.TrustedProxies, prefix)
	}
	return nil
}

func parseProxy(part string) (netip.Prefix, error) {
	if !strings.Contains(part, "/") {
		address, err := netip.ParseAddr(part)
		if err != nil {
			return netip.Prefix{}, err
		}
		part = netip.PrefixFrom(address, address.BitLen()).String()
	}
	prefix, err := netip.ParsePrefix(part)
	if err != nil {
		return netip.Prefix{}, err
	}
	if prefix = prefix.Masked(); prefix.Addr().Is4In6() {
		prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96)
	}
	return prefix, nil
}

func (s *Server) parseProxyMode(value string) error {
	switch value {
	case ProxyModeScaleway:
		s.ProxyMode = value
	case "none", "off":
		s.ProxyMode = ProxyModeNone
	default:
		return fmt.Errorf("unknown mode %q", value)
	}
	return nil
}

func secretFromEnv(getenv func(string) string, key string) ([]byte, error) {
	value := strings.TrimSpace(getenv(key))
	path := strings.TrimSpace(getenv(key + "_FILE"))
	switch {
	case value != "" && path != "":
		return nil, fmt.Errorf("%s and %s_FILE are both set", key, key)
	case path != "":
		secret, err := readSecretFile(path)
		if err != nil {
			return nil, fmt.Errorf("%s_FILE: %w", key, err)
		}
		return secret, nil
	case value != "":
		secret, err := pepper.ParseSecret(value)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		return secret, nil
	default:
		return nil, nil
	}
}

func readSecretFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if info, statErr := file.Stat(); statErr != nil {
		return nil, statErr
	} else if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("group or others can access the file; restrict it with chmod 600 or 400")
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxSecretFileBytes+1))
	defer clear(raw)
	if err != nil {
		return nil, err
	}
	if len(raw) > maxSecretFileBytes {
		return nil, errors.New("the file is larger than a secret can be")
	}
	return pepper.ParseSecret(strings.TrimSpace(string(raw)))
}

func durationInto(target *time.Duration) func(string) error {
	return func(value string) error {
		seconds, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			duration, durationErr := time.ParseDuration(value)
			if durationErr != nil {
				return durationErr
			}
			*target = duration
			return nil
		}
		if seconds < 0 || seconds > int64(math.MaxInt64/time.Second) {
			return fmt.Errorf("want 0 to %d seconds, got %q", int64(math.MaxInt64/time.Second), value)
		}
		*target = time.Duration(seconds) * time.Second
		return nil
	}
}

func int64Into(target *int64) func(string) error {
	return func(value string) error {
		number, err := parseNonNegative(value)
		if err != nil {
			return err
		}
		*target = number
		return nil
	}
}

func intInto(target *int) func(string) error {
	return func(value string) error {
		number, err := parseNonNegative(value)
		if err != nil {
			return err
		}
		if number > math.MaxInt {
			return fmt.Errorf("want at most %d, got %q", math.MaxInt, value)
		}
		*target = int(number)
		return nil
	}
}

func parseNonNegative(value string) (int64, error) {
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil || number < 0 {
		return 0, fmt.Errorf("want a non-negative integer, got %q", value)
	}
	return number, nil
}

func floatInto(target *float64) func(string) error {
	return func(value string) error {
		number, err := strconv.ParseFloat(value, 64)
		if err != nil || number <= 0 || math.IsInf(number, 0) || math.IsNaN(number) {
			return fmt.Errorf("want a positive number, got %q", value)
		}
		*target = number
		return nil
	}
}

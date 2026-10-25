package config_test

import (
	"bytes"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server/abuse"
	"github.com/misaon/nylzo/internal/server/config"
)

const (
	currentPepper  = "00112233445566778899aabbccddeeff00112233445566778899aabbccddee01"
	previousPepper = "ffeeddccbbaa99887766554433221100ffeeddccbbaa99887766554433221102"
)

var reportKey = func() string {
	key, err := abuse.NewKeyPair()
	if err != nil {
		panic(err)
	}
	return core.Encode(key.PublicKey().Bytes())
}()

// environment gives every configuration the operator's report key unless the test sets the variable itself.
func environment(variables map[string]string) func(string) string {
	return func(name string) string {
		if value, set := variables[name]; set || name != "NYLZO_REPORT_KEY" {
			return value
		}
		return reportKey
	}
}

func TestServeNeedsAValidReportKey(t *testing.T) {
	t.Parallel()
	for name, value := range map[string]string{"missing": "", "not a key": "AAAA", "an Ed25519 key": core.Encode(make([]byte, 32))} {
		_, err := config.FromEnv(
			environment(map[string]string{"NYLZO_PEPPER": currentPepper, "NYLZO_REPORT_KEY": value}),
			true,
		)
		if !errors.Is(err, config.ErrInvalid) || !strings.Contains(err.Error(), "NYLZO_REPORT_KEY") {
			t.Errorf("%s report key: %v", name, err)
		}
	}
	if _, err := config.FromEnv(environment(map[string]string{"NYLZO_REPORT_KEY": ""}), false); err != nil {
		t.Fatalf("sweep-once and takedown need no report key: %v", err)
	}
}

func withPepper(name, value string) func(string) string {
	return environment(map[string]string{"NYLZO_PEPPER": currentPepper, name: value})
}

func TestFromEnvReadsEveryVariable(t *testing.T) {
	t.Parallel()
	settings, err := config.FromEnv(environment(map[string]string{
		"NYLZO_PEPPER":               currentPepper,
		"NYLZO_PEPPER_PREVIOUS":      previousPepper,
		"NYLZO_TRUSTED_PROXIES":      "10.0.0.0/8, 192.0.2.1, ::ffff:172.17.0.1, ::ffff:10.0.0.0/104",
		"NYLZO_PROXY_MODE":           "scaleway",
		"NYLZO_SWEEP_INTERVAL":       "15m",
		"NYLZO_MAX_TTL":              "604800",
		"NYLZO_MAX_OPENS":            "3",
		"NYLZO_MAX_FILE_BYTES":       "1048576",
		"NYLZO_RATE_CREATE_HOUR":     "7",
		"NYLZO_GLOBAL_DAILY_CREATES": "500",
		"NYLZO_POW_CONCURRENCY":      "4",
		"NYLZO_MIN_FREE_BYTES":       "0",
		"NYLZO_PUBLIC_URL":           " https://nylzo.example ",
		"PORT":                       "9090",
	}), true)
	if err != nil {
		t.Fatal(err)
	}
	proxies := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("192.0.2.1/32"),
		netip.MustParsePrefix("172.17.0.1/32"),
		netip.MustParsePrefix("10.0.0.0/8"),
	}
	if settings.Listen != ":9090" || !slices.Equal(settings.TrustedProxies, proxies) ||
		settings.ProxyMode != config.ProxyModeScaleway || settings.SweepInterval != 15*time.Minute ||
		settings.Limits.MaxTTL != config.MaxTTL || settings.Limits.MaxOpens != 3 ||
		settings.Limits.MaxObjectBytes != core.ObjectLimit(1048576) || settings.Rates.CreatePerHour != 7 ||
		settings.Rates.GlobalDailyCreates != 500 || settings.PoWConcurrency != 4 || settings.MinFreeBytes != 0 ||
		settings.PublicURL != "https://nylzo.example" || len(settings.PepperPrevious) != 32 {
		t.Fatalf("settings %+v", settings)
	}
	if got := config.Defaults().Limits.MaxObjectBytes; got != 5373983 {
		t.Fatalf("default max_object_bytes %d, spec section 3 says 5373983", got)
	}
	if _, withoutPepper := config.FromEnv(environment(nil), false); withoutPepper != nil {
		t.Fatalf("sweep-once without pepper: %v", withoutPepper)
	}
}

func TestFromEnvRefusesInvalidValues(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name, value string
	}{
		{"NYLZO_PEPPER", "short"},
		{"NYLZO_PEPPER", strings.Repeat("ab", 32)},
		{"NYLZO_PROXY_MODE", "cloudflare"},
		{"NYLZO_TRUSTED_PROXIES", "10.0.0.0/33"},
		{"NYLZO_PASSPHRASE_IP_SCOPE", "on"},
		{"NYLZO_SWEEP_INTERVAL", "-1"},
		{"NYLZO_MAX_TTL", "169h"},
		{"NYLZO_MAX_TTL", "604801"},
		{"NYLZO_MAX_TTL", "36028797019568768"},
		{"NYLZO_DEFAULT_TTL", "59"},
		{"NYLZO_DEFAULT_TTL", "30s"},
		{"NYLZO_MAX_OPENS", "0"},
		{"NYLZO_MAX_OPENS", "101"},
		{"NYLZO_MAX_FILE_BYTES", strconv.Itoa(core.MaxFileBytes + 1)},
		{"NYLZO_RATE_COARSE_MULTIPLIER", "4611686018427387905"},
		{"NYLZO_RATE_SOURCE_RPS", "0"},
		{"NYLZO_RATE_SOURCE_RPS", "NaN"},
		{"NYLZO_RATE_SOURCE_BURST", "+Inf"},
		{"NYLZO_RATE_SOURCE_BURST", "0.5"},
		{"NYLZO_RATE_BUCKET_BURST", "0.5"},
		{"NYLZO_POW_DIFFICULTY", "9"},
		{"NYLZO_POW_CONCURRENCY", "5"},
		{"NYLZO_POW_CONCURRENCY", "9223372036854775807"},
		{"NYLZO_MAX_STREAMS", "0"},
		{"NYLZO_MIN_FREE_BYTES", "-1"},
		{"NYLZO_PUBLIC_URL", "https://nylzo.example/"},
	} {
		_, err := config.FromEnv(withPepper(testCase.name, testCase.value), true)
		if !errors.Is(err, config.ErrInvalid) || !strings.Contains(err.Error(), testCase.name) {
			t.Errorf("%s=%s: want an error naming the variable, got %v", testCase.name, testCase.value, err)
		}
	}
	if _, err := config.FromEnv(
		environment(nil),
		true,
	); err == nil ||
		!strings.Contains(err.Error(), "NYLZO_PEPPER_FILE") {
		t.Fatalf("a missing pepper must mention both ways to set it: %v", err)
	}
}

func TestValidateRefusesSettingsOutsideTheLimits(t *testing.T) {
	t.Parallel()
	for name, change := range map[string]func(*config.Server){
		"no PoW slot":         func(settings *config.Server) { settings.PoWConcurrency = 0 },
		"no stream slot":      func(settings *config.Server) { settings.MaxStreams = 0 },
		"negative free":       func(settings *config.Server) { settings.MinFreeBytes = -1 },
		"zero burst":          func(settings *config.Server) { settings.Rates.BucketBurst = 0 },
		"zero rate":           func(settings *config.Server) { settings.Rates.SourcePerSecond = 0 },
		"negative sweep":      func(settings *config.Server) { settings.SweepInterval = -time.Second },
		"stale object limit":  func(settings *config.Server) { settings.Limits.MaxFileBytes = 1 << 20 },
		"overflowing limit":   func(settings *config.Server) { settings.Rates.CreatePerHour = 1 << 62 },
		"plain http publicly": func(settings *config.Server) { settings.PublicURL = "http://nylzo.example" },
	} {
		settings := config.Defaults()
		change(&settings)
		if err := settings.Validate(); !errors.Is(err, config.ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestPepperFromFile(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, []byte(content), 0o400); err != nil {
			t.Fatal(err)
		}
		return path
	}
	fromVariable, err := config.FromEnv(
		environment(map[string]string{"NYLZO_PEPPER": currentPepper, "NYLZO_PEPPER_PREVIOUS": previousPepper}),
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	fromFile, err := config.FromEnv(environment(map[string]string{
		"NYLZO_PEPPER_FILE":          write("pepper", currentPepper+"\n"),
		"NYLZO_PEPPER_PREVIOUS_FILE": write("previous", "  "+previousPepper+"\r\n"),
	}), true)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(fromFile.Pepper, fromVariable.Pepper) || len(fromFile.Pepper) != 32 ||
		!bytes.Equal(fromFile.PepperPrevious, fromVariable.PepperPrevious) {
		t.Fatal("a pepper from a file differs from the same pepper in a variable")
	}
	for name, variables := range map[string]map[string]string{
		"both set":       {"NYLZO_PEPPER": currentPepper, "NYLZO_PEPPER_FILE": write("both", currentPepper)},
		"missing file":   {"NYLZO_PEPPER_FILE": filepath.Join(directory, "absent")},
		"empty file":     {"NYLZO_PEPPER_FILE": write("empty", "\n")},
		"bad content":    {"NYLZO_PEPPER_FILE": write("bad", "not a pepper")},
		"too large":      {"NYLZO_PEPPER_FILE": write("large", currentPepper+strings.Repeat(" ", 4097))},
		"bad previous":   {"NYLZO_PEPPER": currentPepper, "NYLZO_PEPPER_PREVIOUS_FILE": write("bad-previous", "short")},
		"previous twice": {"NYLZO_PEPPER": currentPepper, "NYLZO_PEPPER_PREVIOUS": previousPepper, "NYLZO_PEPPER_PREVIOUS_FILE": write("twice", previousPepper)},
	} {
		_, refused := config.FromEnv(environment(variables), true)
		if !errors.Is(refused, config.ErrInvalid) || !strings.Contains(refused.Error(), "_FILE") {
			t.Fatalf("%s: want an error naming the _FILE variable, got %v", name, refused)
		}
		if strings.Contains(refused.Error(), currentPepper) || strings.Contains(refused.Error(), previousPepper) {
			t.Fatalf("%s: the error leaks the pepper: %v", name, refused)
		}
	}
	if runtime.GOOS == "windows" {
		return
	}
	shared := write("shared", currentPepper)
	if err = os.Chmod(shared, 0o640); err != nil {
		t.Fatal(err)
	}
	_, refused := config.FromEnv(environment(map[string]string{"NYLZO_PEPPER_FILE": shared}), true)
	if !errors.Is(refused, config.ErrInvalid) || !strings.Contains(refused.Error(), "chmod") {
		t.Fatalf("a pepper file that the group can read: %v", refused)
	}
}

func TestOperatorConfiguration(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name      string
		variables map[string]string
		valid     bool
	}{
		{"all set", map[string]string{"NYLZO_OPERATOR": "Example s.r.o.", "NYLZO_CONTACT": "abuse@example.org", "NYLZO_HOSTING": "Vultr, Frankfurt"}, true},
		{"https contact", map[string]string{"NYLZO_CONTACT": "https://example.org/contact"}, true},
		{"https with a port", map[string]string{"NYLZO_CONTACT": "https://example.org:8443/report?lang=cs#form"}, true},
		{"czech name", map[string]string{"NYLZO_OPERATOR": "Ondřej Misák"}, true},
		{"200 bytes", map[string]string{"NYLZO_HOSTING": strings.Repeat("a", 200)}, true},
		{"201 bytes", map[string]string{"NYLZO_HOSTING": strings.Repeat("a", 201)}, false},
		{"control character", map[string]string{"NYLZO_OPERATOR": "Example\x1b[31m"}, false},
		{"bidi override", map[string]string{"NYLZO_OPERATOR": "Example\u202e"}, false},
		{"invalid utf-8", map[string]string{"NYLZO_HOSTING": "Example \xff"}, false},
		{"javascript contact", map[string]string{"NYLZO_CONTACT": "javascript:alert(1)"}, false},
		{"http contact", map[string]string{"NYLZO_CONTACT": "http://example.org"}, false},
		{"mailto contact", map[string]string{"NYLZO_CONTACT": "mailto:abuse@example.org"}, false},
		{"named address", map[string]string{"NYLZO_CONTACT": "Abuse <abuse@example.org>"}, false},
		{"https with user", map[string]string{"NYLZO_CONTACT": "https://user@example.org"}, false},
		{"https without host", map[string]string{"NYLZO_CONTACT": "https:///contact"}, false},
		{"https with only a port", map[string]string{"NYLZO_CONTACT": "https://:443/contact"}, false},
		{"https with a quote", map[string]string{"NYLZO_CONTACT": `https://example.org/"onmouseover`}, false},
		{"https with a space", map[string]string{"NYLZO_CONTACT": "https://example.org/a b"}, false},
		{"https with a bidi override", map[string]string{"NYLZO_CONTACT": "https://example.org/\u202e"}, false},
		{"address with query", map[string]string{"NYLZO_CONTACT": "abuse@example.org?subject=x"}, false},
		{"address with two dots", map[string]string{"NYLZO_CONTACT": "a..b@example.org"}, false},
		{"address with a leading dot", map[string]string{"NYLZO_CONTACT": ".a@example.org"}, false},
		{"address without a dot in the domain", map[string]string{"NYLZO_CONTACT": "abuse@localhost"}, false},
	} {
		if _, err := config.FromEnv(environment(testCase.variables), false); (err == nil) != testCase.valid {
			t.Errorf("%s: %v, want valid %v", testCase.name, err, testCase.valid)
		}
	}
}

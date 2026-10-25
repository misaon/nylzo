package server_test

import (
	"strings"
	"testing"

	"github.com/misaon/nylzo/internal/server"
	"github.com/misaon/nylzo/internal/server/webclient"
)

func TestTransparencyReportsWhatTheServerUses(t *testing.T) {
	t.Parallel()
	cfg := baseConfig(pepperA, nil)
	cfg.Version = "v1.2.3"
	cfg.Rates.CreatePerHour = 7
	cfg.Rates.GlobalDailyCreates = 900
	cfg.PoWDifficulty = 5
	cfg.Limits.MaxOpens = 4
	cfg.Operator = server.Operator{Name: "Example s.r.o.", Contact: "security@example.org", Hosting: "Vultr, Frankfurt"}
	ts, err := newWebServerWith(t, builtClient(t), nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	resp, body := get(t, ts.URL+server.TransparencyPath, nil)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Security-Policy") != webclient.NonHTMLCSP {
		t.Fatalf("%d %v", resp.StatusCode, resp.Header)
	}
	for _, want := range []string{
		`"server":"v1.2.3"`,
		`"operator":{"name":"Example s.r.o.","contact":"security@example.org","hosting":"Vultr, Frankfurt"}`,
		`"max_opens":4`,
		`"create_per_hour":7`,
		`"global_daily_creates":900`,
		`"pow_difficulty":5`,
		`"retention":{"tombstone_s":604800,"abuse_report_s":604800,"daily_counter_s":86400}`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("transparency is missing %s: %s", want, body)
		}
	}
	plain, err := newWebServer(t, builtClient(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, body := get(t, plain.URL+server.TransparencyPath, nil); strings.Contains(body, "operator") {
		t.Fatalf("an unset operator must not appear: %s", body)
	}
}

func TestOperatorConfiguration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		env  map[string]string
		ok   bool
	}{
		{"all set", map[string]string{"NYLZO_OPERATOR": "Example s.r.o.", "NYLZO_CONTACT": "abuse@example.org", "NYLZO_HOSTING": "Vultr, Frankfurt"}, true},
		{"https contact", map[string]string{"NYLZO_CONTACT": "https://example.org/contact"}, true},
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
		{"address with query", map[string]string{"NYLZO_CONTACT": "abuse@example.org?subject=x"}, false},
		{"address with two dots", map[string]string{"NYLZO_CONTACT": "a..b@example.org"}, false},
		{"address with a leading dot", map[string]string{"NYLZO_CONTACT": ".a@example.org"}, false},
		{"address without a dot in the domain", map[string]string{"NYLZO_CONTACT": "abuse@localhost"}, false},
		{"https with a port", map[string]string{"NYLZO_CONTACT": "https://example.org:8443/report?lang=cs#form"}, true},
		{"https with a bidi override", map[string]string{"NYLZO_CONTACT": "https://example.org/\u202e"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := server.FromEnv(func(k string) string { return tc.env[k] }, false)
			if (err == nil) != tc.ok {
				t.Fatalf("FromEnv(%q) = %v, want ok %v", tc.env, err, tc.ok)
			}
		})
	}
}

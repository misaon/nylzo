package server_test

import (
	"strings"
	"testing"

	"github.com/misaon/nylzo/internal/server/config"

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
	cfg.Operator = config.Operator{Name: "Example s.r.o.", Contact: "security@example.org", Hosting: "Vultr, Frankfurt"}
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

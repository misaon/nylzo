package main

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/caddyserver/caddy/v2/caddyconfig"
)

// TestTheAdapterRefusesRequestRecording covers F11: an operator's edit that would log the X-Nylzo-* headers fails at
// load, where the CI test of the shipped Caddyfiles cannot see it.
func TestTheAdapterRefusesRequestRecording(t *testing.T) {
	hetzner := adaptHetzner(t)
	compose, err := os.ReadFile(profileCaddyfiles["Compose"])
	if err != nil {
		t.Fatal(err)
	}
	adapt := func(source string) ([]byte, error) {
		adapted, _, adaptErr := caddyconfig.GetAdapter(adapterName).Adapt(
			[]byte(source),
			map[string]any{"filename": "Caddyfile"},
		)
		return adapted, adaptErr
	}
	adapted, err := adapt(string(compose))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err = json.Unmarshal(adapted, &config); err != nil {
		t.Fatal(err)
	}
	for name, adaptedConfig := range map[string]map[string]any{"Compose": config, "Hetzner Flatcar": hetzner} {
		frontends := 0
		walkObjects(adaptedConfig, func(object map[string]any) {
			if object["handler"] == frontendHandler && object[loggingCheckedField] == true {
				frontends++
			}
		})
		if frontends == 0 {
			t.Errorf("%s: the adapter marked no %s as checked", name, frontendHandler)
		}
	}
	site := "\trequest_body {"
	logBlock := "\tlog default {\n\t\texclude http.log http.handlers http.stdlib\n\t}\n"
	crawler := "\tnylzo_crawler_tls {$NYLZO_DOMAIN}\n"
	transport := "\t\t\ttransport http {\n\t\t\t\tkeepalive 25s\n\t\t\t}\n"
	secrets := "\t\tcurves x25519mlkem768\n\t}\n\trequest_body"
	for name, test := range map[string]struct {
		edits [][2]string
		want  error
	}{
		"bare site log":          {[][2]string{{site, "\tlog\n" + site}}, errAccessLog},
		"site log with format":   {[][2]string{{site, "\tlog {\n\t\tformat json\n\t}\n" + site}}, errAccessLog},
		"no crawler option":      {[][2]string{{crawler, ""}, {site, "\tlog\n" + site}}, errAccessLog},
		"no global log":          {[][2]string{{logBlock, ""}}, errNoDefaultLog},
		"default log unbounded":  {[][2]string{{logBlock, "\tlog default {\n\t\texclude http.log http.stdlib\n\t}\n"}}, errLogsRequests},
		"access log included":    {[][2]string{{logBlock, logBlock + "\tlog access {\n\t\tinclude http.log.access\n\t}\n"}}, errLogsRequests},
		"parent logger included": {[][2]string{{logBlock, logBlock + "\tlog everything {\n\t\tinclude http\n\t}\n"}}, errLogsRequests},
		"parent logger excluded": {[][2]string{{logBlock, "\tlog default {\n\t\texclude http\n\t}\n"}}, nil},
		"default log discarded":  {[][2]string{{logBlock, "\tlog default {\n\t\toutput discard\n\t}\n"}}, nil},
		"tracing":                {[][2]string{{site, "\ttracing\n" + site}}, errTracing},
		"FastCGI transport":      {[][2]string{{transport, "\t\t\ttransport fastcgi\n"}}, errFastCGI},
		"TLS secrets": {
			[][2]string{{secrets, "\t\tcurves x25519mlkem768\n\t\tinsecure_secrets_log /tmp/keys\n\t}\n\trequest_body"}},
			errTLSSecretsFile,
		},
	} {
		t.Run(name, func(t *testing.T) {
			mutated := string(compose)
			for _, edit := range test.edits {
				if !strings.Contains(mutated, edit[0]) {
					t.Fatalf("the Compose Caddyfile no longer holds %q", edit[0])
				}
				mutated = strings.Replace(mutated, edit[0], edit[1], 1)
			}
			if _, got := adapt(mutated); !errors.Is(got, test.want) {
				t.Fatalf("got %v, want %v", got, test.want)
			}
		})
	}
	t.Run("QLOGDIR", func(t *testing.T) {
		t.Setenv("QLOGDIR", t.TempDir())
		if _, got := adapt(string(compose)); !errors.Is(got, errRecordsHeaders) {
			t.Fatalf("got %v, want %v", got, errRecordsHeaders)
		}
	})
}

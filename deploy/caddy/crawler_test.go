package main

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
)

type remoteConn struct {
	net.Conn

	remote net.Addr
}

func (r remoteConn) RemoteAddr() net.Addr { return r.remote }

func TestCrawlerMatchesOnlyThePublishedRanges(t *testing.T) {
	var crawler Crawler
	if err := crawler.Provision(caddy.Context{}); err != nil {
		t.Fatal(err)
	}
	published := crawler.prefixes[0].Addr()
	for address, want := range map[string]bool{
		netip.AddrPortFrom(published, 443).String():                          true,
		netip.AddrPortFrom(netip.AddrFrom16(published.As16()), 443).String(): true,
		"127.0.0.1:443": false,
		netip.AddrPortFrom(netip.MustParseAddr("2001:db8::1"), 443).String():               false,
		netip.AddrPortFrom(crawler.prefixes[len(crawler.prefixes)-1].Addr(), 443).String(): true,
	} {
		remote := net.TCPAddrFromAddrPort(netip.MustParseAddrPort(address))
		if got := crawler.Match(&tls.ClientHelloInfo{Conn: remoteConn{remote: remote}}); got != want {
			t.Errorf("%s matched %v, want %v", address, got, want)
		}
	}
	if crawler.Match(&tls.ClientHelloInfo{}) {
		t.Error("a handshake without a connection matched")
	}
}

func adaptHetzner(t *testing.T) map[string]any {
	t.Helper()
	t.Setenv("NYLZO_DOMAIN", "localhost")
	t.Setenv("NYLZO_CONTACT", operatorContact)
	var source []byte
	for _, name := range []string{"Caddyfile", "www.Caddyfile", "monitor.Caddyfile"} {
		part, err := os.ReadFile(filepath.Join("..", "hetzner-flatcar", "host", "proxy", name))
		if err != nil {
			t.Fatal(err)
		}
		source = append(append(source, part...), '\n')
	}
	adapted, _, err := caddyconfig.GetAdapter(crawlerAdapterKey).Adapt(source, map[string]any{"filename": "Caddyfile"})
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err = json.Unmarshal(adapted, &config); err != nil {
		t.Fatal(err)
	}
	return config
}

func TestAdapterOpensX25519OnlyToCrawlersOfTheNamedSites(t *testing.T) {
	config := adaptHetzner(t)
	applications := config["apps"].(map[string]any)
	if _, ok := applications[crawlerOption]; ok {
		t.Fatal("the adapter left its option in the configuration")
	}
	crawlerPolicies := 0
	for name, value := range applications["http"].(map[string]any)["servers"].(map[string]any) {
		server := value.(map[string]any)
		policies, _ := server["tls_connection_policies"].([]any)
		for i, item := range policies {
			policy := item.(map[string]any)
			curves := stringList(policy["curves"])
			match, _ := policy["match"].(map[string]any)
			if _, crawler := match[crawlerMatcher]; !crawler {
				if !slices.Equal(curves, []string{postQuantumCurve}) || policy["protocol_min"] != "tls1.3" {
					t.Errorf("%s: a policy for everyone allows %v from %v", name, curves, policy["protocol_min"])
				}
				continue
			}
			crawlerPolicies++
			if !slices.Equal(stringList(server["listen"]), []string{":443"}) {
				t.Errorf("%s on %v gives crawlers X25519", name, server["listen"])
			}
			hosts := stringList(match["sni"])
			if !slices.Equal(curves, []string{postQuantumCurve, classicalCurve}) || len(hosts) == 0 ||
				slices.ContainsFunc(
					hosts,
					func(host string) bool { return host != "localhost" && host != "www.localhost" },
				) {
				t.Errorf("%s: crawler policy %v", name, policy)
			}
			next := policies[i+1].(map[string]any)
			if !slices.Equal(stringList(next["match"].(map[string]any)["sni"]), hosts) ||
				!slices.Equal(stringList(next["curves"]), []string{postQuantumCurve}) {
				t.Errorf("%s: the crawler policy is not followed by the post-quantum policy of its sites", name)
			}
		}
	}
	if crawlerPolicies == 0 {
		t.Fatal("no site gives crawlers X25519")
	}
}

func TestCrawlerOptionNeverWidensOrRunsWithoutTheAdapter(t *testing.T) {
	for name, test := range map[string]struct {
		source string
		want   error
	}{
		"policy that allows X25519": {
			"{\n\tnylzo_crawler_tls example.org\n}\nexample.org {\n\ttls {\n\t\tcurves x25519mlkem768 x25519\n\t}\n\trespond ok\n}\n",
			errPolicyNotQuantum,
		},
		"policy for another host": {
			"{\n\tnylzo_crawler_tls example.org\n}\nexample.org, other.example.org {\n\ttls {\n\t\tcurves x25519mlkem768\n\t}\n\trespond ok\n}\n",
			errPolicyWiderThan,
		},
		"no named site": {
			"{\n\tnylzo_crawler_tls missing.example.org\n}\nexample.org {\n\ttls {\n\t\tcurves x25519mlkem768\n\t}\n\trespond ok\n}\n",
			errNoCrawlerSite,
		},
	} {
		if _, _, err := (crawlerAdapter{}).Adapt([]byte(test.source), nil); !errors.Is(err, test.want) {
			t.Errorf("%s: %v, want %v", name, err, test.want)
		}
	}
	adapted, _, err := caddyconfig.GetAdapter("caddyfile").Adapt(
		[]byte("{\n\tadmin off\n\tnylzo_crawler_tls example.org\n}\nhttp://example.org {\n\trespond ok\n}\n"), nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	var config caddy.Config
	if err = json.Unmarshal(adapted, &config); err != nil {
		t.Fatal(err)
	}
	if err = caddy.Validate(&config); err == nil || !strings.Contains(err.Error(), errAdapterRequired.Error()) {
		t.Fatalf("the plain Caddyfile adapter started with the crawler option: %v", err)
	}
}

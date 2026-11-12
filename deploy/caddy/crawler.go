package main

import (
	"bytes"
	"crypto/tls"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddytls"
)

const (
	crawlerOption    = "nylzo_crawler_tls"
	crawlerMatcher   = "nylzo_crawler"
	postQuantumCurve = "x25519mlkem768"
	classicalCurve   = "x25519"
	defaultHTTPSPort = 443
	adapterName      = "nylzo"
)

//go:embed crawlers.json
var crawlerList []byte

var (
	errAdapterRequired   = errors.New(crawlerOption + " needs the Caddyfile loaded with --adapter " + adapterName)
	errNoCrawlerSite     = errors.New(crawlerOption + " names no site served on the HTTPS port")
	errPolicyNotQuantum  = errors.New("allows more than " + postQuantumCurve)
	errPolicyWiderThan   = errors.New("also serves a host that " + crawlerOption + " does not name")
	errNoCrawlerPrefixes = errors.New("the crawler list holds no address range")
	errPrefixNotMasked   = errors.New("is not a network address")
)

func init() {
	caddy.RegisterModule(new(Crawler))
	caddy.RegisterModule(CrawlerSites{})
	httpcaddyfile.RegisterGlobalOption(crawlerOption, parseCrawlerSites)
	caddyconfig.RegisterAdapter(adapterName, nylzoAdapter{})
}

type crawlerFile struct {
	Sources  []string `json:"sources"`
	Prefixes []string `json:"prefixes"`
}

func crawlerPrefixes() ([]netip.Prefix, error) {
	var list crawlerFile
	if err := json.Unmarshal(crawlerList, &list); err != nil {
		return nil, fmt.Errorf("crawler list: %w", err)
	}
	if len(list.Prefixes) == 0 {
		return nil, errNoCrawlerPrefixes
	}
	prefixes := make([]netip.Prefix, 0, len(list.Prefixes))
	for _, text := range list.Prefixes {
		prefix, err := netip.ParsePrefix(text)
		if err != nil {
			return nil, fmt.Errorf("crawler list: %w", err)
		}
		if prefix != prefix.Masked() {
			return nil, fmt.Errorf("crawler list: %s %w", text, errPrefixNotMasked)
		}
		prefixes = append(prefixes, prefix)
	}
	return prefixes, nil
}

// Crawler matches a handshake from the address ranges search engines publish for their crawlers.
type Crawler struct {
	prefixes []netip.Prefix
}

func (*Crawler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "tls.handshake_match." + crawlerMatcher,
		New: func() caddy.Module { return new(Crawler) },
	}
}

func (c *Crawler) Provision(_ caddy.Context) error {
	var err error
	c.prefixes, err = crawlerPrefixes()
	return err
}

func (c *Crawler) Match(hello *tls.ClientHelloInfo) bool {
	if hello.Conn == nil {
		return false
	}
	address, err := netip.ParseAddrPort(hello.Conn.RemoteAddr().String())
	if err != nil {
		return false
	}
	ip := address.Addr().Unmap()
	return slices.ContainsFunc(c.prefixes, func(prefix netip.Prefix) bool { return prefix.Contains(ip) })
}

// CrawlerSites carries the option to the adapter; under any other adapter it stops Caddy from starting.
type CrawlerSites struct {
	Hosts []string `json:"hosts"`
}

func (CrawlerSites) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{ID: crawlerOption, New: func() caddy.Module { return new(CrawlerSites) }}
}

func (CrawlerSites) Provision(_ caddy.Context) error { return errAdapterRequired }

func (CrawlerSites) Start() error { return errAdapterRequired }

func (CrawlerSites) Stop() error { return nil }

func parseCrawlerSites(dispenser *caddyfile.Dispenser, _ any) (any, error) {
	dispenser.Next()
	hosts := dispenser.RemainingArgs()
	if len(hosts) == 0 || dispenser.NextBlock(0) {
		return nil, dispenser.ArgErr()
	}
	return httpcaddyfile.App{Name: crawlerOption, Value: caddyconfig.JSON(CrawlerSites{Hosts: hosts}, nil)}, nil
}

type nylzoAdapter struct{}

// Adapt refuses a configuration that records request headers, and lets the named sites also accept X25519, only from
// crawler addresses, by a policy ahead of their own.
//
//nolint:gocritic // caddyconfig.Adapter fixes these three unnamed results.
func (nylzoAdapter) Adapt(body []byte, options map[string]any) ([]byte, []caddyconfig.Warning, error) {
	adapted, warnings, err := caddyfile.Adapter{ServerType: httpcaddyfile.ServerType{}}.Adapt(body, options)
	if err != nil {
		return nil, warnings, err
	}
	config, err := decodeConfig(adapted)
	if err != nil {
		return nil, warnings, err
	}
	applications, _ := config["apps"].(map[string]any)
	sites, crawled := applications[crawlerOption].(map[string]any)
	if crawled {
		delete(applications, crawlerOption)
		web, _ := applications["http"].(map[string]any)
		if err = addCrawlerPolicies(web, stringList(sites["hosts"])); err != nil {
			return nil, warnings, err
		}
	}
	if err = refuseHeaderDumps(); err != nil {
		return nil, warnings, err
	}
	if err = refuseRequestRecording(config); err != nil {
		return nil, warnings, err
	}
	markChecked(config)
	result, err := json.Marshal(config)
	return result, warnings, err
}

func decodeConfig(encoded []byte) (map[string]any, error) {
	var config map[string]any
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := decoder.Decode(&config); err != nil {
		return nil, err
	}
	return config, nil
}

func addCrawlerPolicies(web map[string]any, hosts []string) error {
	port := strconv.Itoa(defaultHTTPSPort)
	if configured, ok := web["https_port"].(json.Number); ok {
		port = configured.String()
	}
	servers, _ := web["servers"].(map[string]any)
	found := false
	for _, value := range servers {
		server, _ := value.(map[string]any)
		if !listensOnlyOn(server, port) {
			continue
		}
		policies, _ := server["tls_connection_policies"].([]any)
		combined := make([]any, 0, len(policies)+1)
		for _, item := range policies {
			policy, _ := item.(map[string]any)
			if servesNamedSite(policy, hosts) {
				crawler, err := crawlerPolicy(policy, hosts)
				if err != nil {
					return err
				}
				combined, found = append(combined, crawler), true
			}
			combined = append(combined, policy)
		}
		server["tls_connection_policies"] = combined
	}
	if !found {
		return errNoCrawlerSite
	}
	return nil
}

func servesNamedSite(policy map[string]any, hosts []string) bool {
	match, _ := policy["match"].(map[string]any)
	return slices.ContainsFunc(stringList(match["sni"]), func(name string) bool { return slices.Contains(hosts, name) })
}

func crawlerPolicy(policy map[string]any, hosts []string) (map[string]any, error) {
	match, _ := policy["match"].(map[string]any)
	names := stringList(match["sni"])
	for _, name := range names {
		if !slices.Contains(hosts, name) {
			return nil, fmt.Errorf("the TLS policy for %s %w", name, errPolicyWiderThan)
		}
	}
	if curves := stringList(policy["curves"]); !slices.Equal(curves, []string{postQuantumCurve}) {
		return nil, fmt.Errorf("the TLS policy for %s %w", strings.Join(names, ", "), errPolicyNotQuantum)
	}
	encoded, err := json.Marshal(policy)
	if err != nil {
		return nil, err
	}
	crawler, err := decodeConfig(encoded)
	if err != nil {
		return nil, err
	}
	crawlerMatch, _ := crawler["match"].(map[string]any)
	crawlerMatch[crawlerMatcher] = map[string]any{}
	crawler["curves"] = []string{postQuantumCurve, classicalCurve}
	return crawler, nil
}

func listensOnlyOn(server map[string]any, port string) bool {
	addresses := stringList(server["listen"])
	return len(addresses) > 0 && !slices.ContainsFunc(addresses, func(address string) bool {
		return !strings.HasSuffix(address, ":"+port)
	})
}

func stringList(value any) []string {
	items, _ := value.([]any)
	texts := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			texts = append(texts, text)
		}
	}
	return texts
}

var (
	_ caddy.App                  = (*CrawlerSites)(nil)
	_ caddy.Provisioner          = (*CrawlerSites)(nil)
	_ caddy.Provisioner          = (*Crawler)(nil)
	_ caddytls.ConnectionMatcher = (*Crawler)(nil)
	_ caddyconfig.Adapter        = nylzoAdapter{}
)

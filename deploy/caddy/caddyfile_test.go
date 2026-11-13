package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"

	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/server/web"
	"github.com/misaon/nylzo/internal/server/webclient"
)

const operatorContact = "security@example.org"

func embeddedPages(t *testing.T, publicURL string) webclient.Pages {
	t.Helper()
	build, err := webclient.Load(web.Files())
	if err != nil {
		t.Fatal(err)
	}
	pages, err := build.Render(publicURL, operatorContact)
	if err != nil {
		t.Fatal(err)
	}
	return pages
}

// expectSameResponse compares what Caddy sent with what the frontend writes, whether the build has the page or not.
func expectSameResponse(t *testing.T, response *http.Response, status int, write func(http.ResponseWriter)) {
	t.Helper()
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	expected := httptest.NewRecorder()
	write(expected)
	if response.StatusCode != status || expected.Code != status || !bytes.Equal(body, expected.Body.Bytes()) {
		t.Fatalf(
			"status %d with %d bytes, want %d with %d bytes",
			response.StatusCode,
			len(body),
			status,
			expected.Body.Len(),
		)
	}
	for _, name := range []string{"Connection-Allowlist", "Content-Security-Policy", "Content-Type", "Retry-After", "Vary"} {
		if got, want := response.Header.Get(name), expected.Header().Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

type zeros struct{}

func (zeros) Read(buffer []byte) (int, error) {
	clear(buffer)
	return len(buffer), nil
}

type upstream struct {
	mutex    sync.Mutex
	requests []http.Header
}

func (u *upstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	u.mutex.Lock()
	u.requests = append(u.requests, r.Header.Clone())
	u.mutex.Unlock()
	if _, err := io.Copy(io.Discard, r.Body); err != nil {
		http.Error(w, "unread body", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"ok":true}`)
}

func (u *upstream) last(t *testing.T) http.Header {
	t.Helper()
	u.mutex.Lock()
	defer u.mutex.Unlock()
	if len(u.requests) == 0 {
		t.Fatal("the upstream received no request")
	}
	return u.requests[len(u.requests)-1]
}

func freePort(t *testing.T) int {
	t.Helper()
	for range 20 {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := listener.Addr().(*net.TCPAddr).Port
		_ = listener.Close()
		packets, err := net.ListenPacket("udp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			_ = packets.Close()
			return port
		}
	}
	t.Fatal("no free port")
	return 0
}

func rewrite(value any, apply func(key string, value any) any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			typed[key] = apply(key, rewrite(child, apply))
		}
	case []any:
		for i, child := range typed {
			typed[i] = rewrite(child, apply)
		}
	default:
	}
	return value
}

const (
	otherAddresses   = "192.0.2.0/24"
	crawlerAddresses = "127.0.0.0/8"
)

func startCaddy(t *testing.T, caddyfile, backend string) (string, *x509.CertPool) {
	t.Helper()
	return startCaddyWith(t, caddyfile, backend, "", otherAddresses)
}

func startCaddyWithMaintenance(t *testing.T, caddyfile, backend, maintenance string) (string, *x509.CertPool) {
	t.Helper()
	return startCaddyWith(t, caddyfile, backend, maintenance, otherAddresses)
}

// startCaddyWith serves the Caddyfile on loopback; crawlers stands in for the embedded crawler list.
func startCaddyWith(
	t *testing.T,
	caddyfile, backend, maintenance, crawlers string,
	edits ...func(config map[string]any),
) (string, *x509.CertPool) {
	t.Helper()
	t.Setenv("NYLZO_DOMAIN", "localhost")
	t.Setenv("NYLZO_CONTACT", operatorContact)
	source, err := os.ReadFile(caddyfile)
	if err != nil {
		t.Fatal(err)
	}
	adapted, _, err := caddyconfig.GetAdapter("nylzo").Adapt(source, map[string]any{"filename": caddyfile})
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err = json.Unmarshal(adapted, &config); err != nil {
		t.Fatal(err)
	}
	httpPort, httpsPort := freePort(t), freePort(t)
	storage := t.TempDir()
	config["storage"] = map[string]any{"module": "file_system", "root": storage}
	applications := config["apps"].(map[string]any)
	applications["pki"] = map[string]any{
		"certificate_authorities": map[string]any{"local": map[string]any{"install_trust": false}},
	}
	server := applications["http"].(map[string]any)
	server["http_port"], server["https_port"] = httpPort, httpsPort
	rewrite(config, func(key string, value any) any {
		switch key {
		case "listen":
			listeners := value.([]any)
			for i, listener := range listeners {
				port := httpsPort
				if listener == ":80" {
					port = httpPort
				}
				listeners[i] = fmt.Sprintf("127.0.0.1:%d", port)
			}
		case "dial":
			return backend
		case "maintenance":
			return maintenance
		case "issuers":
			return []any{map[string]any{"module": "internal"}}
		case "match":
			matchers, _ := value.(map[string]any)
			if _, ok := matchers[crawlerMatcher]; ok {
				delete(matchers, crawlerMatcher)
				matchers["remote_ip"] = map[string]any{"ranges": []string{crawlers}}
			}
		default:
		}
		return value
	})
	for _, edit := range edits {
		edit(config)
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err = caddy.Load(encoded, true); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = caddy.Stop() })
	address := fmt.Sprintf("localhost:%d", httpsPort)
	roots := x509.NewCertPool()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if pem, readErr := os.ReadFile(
			filepath.Join(storage, "pki", "authorities", "local", "root.crt"),
		); readErr == nil &&
			roots.AppendCertsFromPEM(pem) {
			connection, dialErr := tls.Dial("tcp", address, &tls.Config{RootCAs: roots, ServerName: "localhost"})
			if dialErr == nil {
				_ = connection.Close()
				return address, roots
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("Caddy did not serve a certificate in time")
	return "", nil
}

var profileCaddyfiles = map[string]string{
	"Hetzner Flatcar": filepath.Join("..", "hetzner-flatcar", "host", "proxy", "Caddyfile"),
	"Compose":         filepath.Join("..", "compose", "Caddyfile"),
}

func TestCaddyfilesProtectTheEdge(t *testing.T) {
	for name, caddyfile := range profileCaddyfiles {
		t.Run(name, func(t *testing.T) {
			recorder := &upstream{}
			backend := httptest.NewServer(recorder)
			address, roots := startCaddy(t, caddyfile, backend.Listener.Addr().String())
			base := &tls.Config{RootCAs: roots, ServerName: "localhost"}

			for refused, config := range map[string]*tls.Config{
				"TLS 1.2":      {MaxVersion: tls.VersionTLS12},
				"X25519 alone": {MinVersion: tls.VersionTLS13, CurvePreferences: []tls.CurveID{tls.X25519}},
				"P-256 alone":  {MinVersion: tls.VersionTLS13, CurvePreferences: []tls.CurveID{tls.CurveP256}},
			} {
				config.RootCAs, config.ServerName = roots, "localhost"
				if connection, err := tls.Dial("tcp", address, config); err == nil {
					_ = connection.Close()
					t.Errorf("Caddy accepted a handshake with %s", refused)
				}
			}
			connection, err := tls.Dial("tcp", address, &tls.Config{
				RootCAs:          roots,
				ServerName:       "localhost",
				CurvePreferences: []tls.CurveID{tls.X25519, tls.X25519MLKEM768},
			})
			if err != nil {
				t.Fatalf("handshake offering X25519 first: %v", err)
			}
			if curve := connection.ConnectionState().CurveID; curve != tls.X25519MLKEM768 {
				t.Errorf("a client that offers X25519MLKEM768 negotiated %s", curve)
			}
			_ = connection.Close()

			client := &http.Client{
				Transport: &http.Transport{TLSClientConfig: base.Clone(), ForceAttemptHTTP2: true},
				Timeout:   10 * time.Second,
			}
			request, _ := http.NewRequest(http.MethodGet, "https://"+address+"/api/v1/instance", http.NoBody)
			for header, value := range map[string]string{"X-Real-IP": "7.7.7.7", "Forwarded": "for=8.8.8.8", "X-Forwarded-For": "9.9.9.9", "True-Client-IP": "6.6.6.6", "X-Client-IP": "5.5.5.5"} {
				request.Header.Set(header, value)
			}
			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			_ = response.Body.Close()
			forwarded := recorder.last(t)
			for _, header := range []string{"X-Real-IP", "Forwarded", "True-Client-IP", "X-Client-IP"} {
				if value := forwarded.Get(header); value != "" {
					t.Errorf("the client's %s reached the upstream: %q", header, value)
				}
			}
			if value := forwarded.Get("X-Forwarded-For"); value != "127.0.0.1" {
				t.Errorf("X-Forwarded-For = %q, want only the peer address", value)
			}
			oversized, _ := http.NewRequest(
				http.MethodPost,
				"https://"+address+"/api/v1/messages",
				io.LimitReader(zeros{}, 7<<20),
			)
			response, err = client.Do(oversized)
			if err != nil {
				t.Fatal(err)
			}
			_ = response.Body.Close()
			if response.StatusCode != http.StatusRequestEntityTooLarge {
				t.Errorf("a 7 MiB body got %d, want 413", response.StatusCode)
			}

			sessions := tls.NewLRUClientSessionCache(4)
			quicConfig := base.Clone()
			quicConfig.ClientSessionCache = sessions
			first := &http3.Transport{TLSClientConfig: quicConfig}
			response, err = (&http.Client{Transport: first, Timeout: 10 * time.Second}).Get(
				"https://" + address + "/api/first",
			)
			if err != nil {
				t.Fatalf("HTTP/3: %v", err)
			}
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("HTTP/3 API request: %d", response.StatusCode)
			}
			time.Sleep(300 * time.Millisecond)
			_ = first.Close()
			var early *quic.Conn
			second := &http3.Transport{
				TLSClientConfig: quicConfig,
				Dial: func(ctx context.Context, address string, config *tls.Config, quicConfig *quic.Config) (*quic.Conn, error) {
					connection, dialErr := quic.DialAddrEarly(ctx, address, config, quicConfig)
					early = connection
					return connection, dialErr
				},
			}
			defer second.Close()
			request, _ = http.NewRequest(http3.MethodGet0RTT, "https://"+address+"/api/early", http.NoBody)
			response, err = (&http.Client{Transport: second, Timeout: 10 * time.Second}).Do(request)
			if err != nil {
				t.Fatalf("HTTP/3 resumption: %v", err)
			}
			_ = response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("a resumed post-quantum session got %d from the API", response.StatusCode)
			}
			if early == nil || !early.ConnectionState().TLS.DidResume {
				t.Fatal("the second HTTP/3 connection did not resume its session, so it tested nothing")
			}
			if early.ConnectionState().Used0RTT {
				t.Error("Caddy accepted 0-RTT early data")
			}
			if value := recorder.last(t).Get("Early-Data"); value != "" {
				t.Errorf("the upstream received Early-Data: %q", value)
			}

			backend.Close()
			response, err = client.Get("https://" + address + "/api/unavailable")
			if err != nil {
				t.Fatal(err)
			}
			_ = response.Body.Close()
			if response.StatusCode != http.StatusBadGateway {
				t.Fatalf("status %d without an upstream, want 502", response.StatusCode)
			}
			for header, want := range map[string]string{
				"Content-Security-Policy":   webclient.NonHTMLCSP,
				"X-Content-Type-Options":    "nosniff",
				"Strict-Transport-Security": "max-age=63072000; includeSubDomains; preload",
				"Cache-Control":             "no-store",
				"Server":                    "",
			} {
				if got := response.Header.Get(header); got != want {
					t.Errorf("Caddy's 502 has %s %q, want %q", header, got, want)
				}
			}
			navigation, _ := http.NewRequest(http.MethodGet, "https://"+address+"/api/unavailable", http.NoBody)
			navigation.Header.Set("Sec-Fetch-Dest", "document")
			navigation.Header.Set("Accept-Language", "cs")
			response, err = client.Do(navigation)
			if err != nil {
				t.Fatal(err)
			}
			expectSameResponse(t, response, http.StatusBadGateway, func(w http.ResponseWriter) {
				embeddedPages(t, "").ServeServerError(w, navigation, http.StatusBadGateway)
			})
		})
	}
}

func TestCaddyfilesBoundSlowAndLargeRequests(t *testing.T) {
	for name, caddyfile := range profileCaddyfiles {
		t.Run(name, func(t *testing.T) {
			t.Setenv("NYLZO_DOMAIN", "localhost")
			source, err := os.ReadFile(caddyfile)
			if err != nil {
				t.Fatal(err)
			}
			adapted, _, err := caddyconfig.GetAdapter("nylzo").Adapt(source, map[string]any{"filename": caddyfile})
			if err != nil {
				t.Fatal(err)
			}
			var config struct {
				Admin struct {
					Disabled bool `json:"disabled"`
				} `json:"admin"`
				Apps struct {
					HTTP struct {
						Servers map[string]struct {
							Allow0RTT         *bool          `json:"allow_0rtt"`
							ReadTimeout       caddy.Duration `json:"read_timeout"`
							ReadHeaderTimeout caddy.Duration `json:"read_header_timeout"`
							ReadIdleTimeout   caddy.Duration `json:"read_idle_timeout"`
							WriteTimeout      caddy.Duration `json:"write_timeout"`
							IdleTimeout       caddy.Duration `json:"idle_timeout"`
							MaxHeaderBytes    int            `json:"max_header_bytes"`
						} `json:"servers"`
					} `json:"http"`
				} `json:"apps"`
			}
			if err = json.Unmarshal(adapted, &config); err != nil {
				t.Fatal(err)
			}
			if !config.Admin.Disabled {
				t.Error("the admin API is enabled")
			}
			for name, server := range config.Apps.HTTP.Servers {
				if server.Allow0RTT == nil || *server.Allow0RTT {
					t.Errorf("%s allows 0-RTT", name)
				}
				for setting, value := range map[string]caddy.Duration{"read_body": server.ReadTimeout, "read_header": server.ReadHeaderTimeout, "read_body_idle": server.ReadIdleTimeout, "write": server.WriteTimeout, "idle": server.IdleTimeout} {
					if value <= 0 || time.Duration(value) > 6*time.Minute {
						t.Errorf(
							"%s timeout %s is %s, want a bound of at most six minutes",
							name,
							setting,
							time.Duration(value),
						)
					}
				}
				if server.MaxHeaderBytes <= 0 || server.MaxHeaderBytes > 16<<10 {
					t.Errorf("%s max_header_size is %d", name, server.MaxHeaderBytes)
				}
			}
		})
	}
}

// logEverythingTo sends every log to one file at debug level and keeps what each log includes and excludes.
func logEverythingTo(path string) func(config map[string]any) {
	return func(config map[string]any) {
		writer := map[string]any{"output": "file", "filename": path}
		logging := config["logging"].(map[string]any)
		for _, log := range logging["logs"].(map[string]any) {
			log.(map[string]any)["writer"] = writer
			log.(map[string]any)["level"] = "DEBUG"
		}
		logging["sink"] = map[string]any{"writer": writer}
	}
}

// composeCaddyfile joins Caddyfiles the way the Hetzner activation does.
func composeCaddyfile(t *testing.T, parts ...string) string {
	t.Helper()
	var composed []byte
	for _, part := range parts {
		source, err := os.ReadFile(part)
		if err != nil {
			t.Fatal(err)
		}
		composed = append(append(composed, source...), '\n')
	}
	path := filepath.Join(t.TempDir(), "Caddyfile")
	if err := os.WriteFile(path, composed, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func awaitCertificate(t *testing.T, address string, roots *x509.CertPool, name string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		connection, err := tls.Dial("tcp", address, &tls.Config{RootCAs: roots, ServerName: name})
		if err == nil {
			_ = connection.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("Caddy served no certificate for %s in time: %v", name, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// TestCaddyfilesLogNoRequestSecrets covers F11: a logged X-Nylzo-Lease downloads a blob without an open, and the
// X-Nylzo-Verifier of protocol v2 tested passphrase guesses, so Caddy logs no protocol header and no message path, over
// HTTP/2 or HTTP/3, not even at debug level or when the upstream fails.
func TestCaddyfilesLogNoRequestSecrets(t *testing.T) {
	sentinels := map[string]string{
		"Authorization":            "Nylzo f11-sentinel-authorization",
		api.CreateHeaderName:       "f11-sentinel-create",
		api.VerifierHeaderName:     "f11-sentinel-verifier",
		api.RegistrationHeaderName: "f11-sentinel-registration",
		api.KE1HeaderName:          "f11-sentinel-ke1",
		api.KE3HeaderName:          "f11-sentinel-ke3",
		api.LeaseHeaderName:        "f11-sentinel-lease",
		api.WorkHeaderName:         "f11-sentinel-pow",
		api.TTLHeaderName:          "f11-sentinel-ttl",
		api.ShareKeyHeaderName:     "f11-sentinel-share-key",
	}
	responses := map[string]string{
		api.LeaseHeaderName:  "f11-sentinel-response-lease",
		api.ShareHeaderName:  "f11-sentinel-response-share",
		api.PolicyHeaderName: "f11-sentinel-response-policy",
	}
	const messageID = "f11-sentinel-message-id"
	burn, claim := api.MessagePath(messageID)+api.BurnAction, api.MessagePath(messageID)+api.ClaimAction
	// The monitor site is left out: it needs the monitor's client certificate file.
	for name, profile := range map[string]struct {
		parts     []string
		redirects []string
	}{
		"Compose": {parts: []string{profileCaddyfiles["Compose"]}},
		"Hetzner Flatcar with www": {
			parts: []string{
				profileCaddyfiles["Hetzner Flatcar"],
				filepath.Join("..", "hetzner-flatcar", "host", "proxy", "www.Caddyfile"),
			},
			redirects: []string{"www.localhost"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			logFile := filepath.Join(t.TempDir(), "caddy.log")
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				if !strings.HasSuffix(r.URL.Path, api.ClaimAction) {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				for header, value := range responses {
					w.Header().Set(header, value)
				}
				w.Header().Set("Content-Type", "application/octet-stream")
				_, _ = io.WriteString(w, "ciphertext")
			}))
			t.Cleanup(backend.Close)
			address, roots := startCaddyWith(
				t,
				composeCaddyfile(t, profile.parts...),
				backend.Listener.Addr().String(),
				"",
				otherAddresses,
				logEverythingTo(logFile),
			)
			_, port, splitErr := net.SplitHostPort(address)
			if splitErr != nil {
				t.Fatal(splitErr)
			}
			client := &http.Client{
				Transport: &http.Transport{
					DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
						return (&net.Dialer{}).DialContext(ctx, network, address)
					},
					TLSClientConfig:   &tls.Config{RootCAs: roots},
					ForceAttemptHTTP2: true,
				},
				CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
				Timeout:       10 * time.Second,
			}
			h3 := &http3.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, ServerName: "localhost"}}
			t.Cleanup(func() { _ = h3.Close() })
			overQUIC := &http.Client{Transport: h3, Timeout: 10 * time.Second}
			send := func(client *http.Client, protocol int, host, path string, body io.Reader, want int) http.Header {
				t.Helper()
				target := "https://" + net.JoinHostPort(host, port) + path
				request, err := http.NewRequest(http.MethodPost, target, body)
				if err != nil {
					t.Fatal(err)
				}
				for header, value := range sentinels {
					request.Header.Set(header, value)
				}
				response, err := client.Do(request)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = io.Copy(io.Discard, response.Body)
				_ = response.Body.Close()
				if response.StatusCode != want || response.ProtoMajor != protocol {
					t.Fatalf("POST %s got %d over %s, want %d over HTTP/%d", target, response.StatusCode,
						response.Proto, want, protocol)
				}
				return response.Header
			}
			send(client, 2, "localhost", burn, http.NoBody, http.StatusNoContent)
			send(overQUIC, 3, "localhost", burn, http.NoBody, http.StatusNoContent)
			claimed := send(client, 2, "localhost", claim, http.NoBody, http.StatusOK)
			if claimed.Get(api.LeaseHeaderName) != responses[api.LeaseHeaderName] {
				t.Fatal("the claim response lost its lease, so its headers prove nothing")
			}
			oversized := io.LimitReader(zeros{}, 7<<20)
			send(client, 2, "localhost", api.MessagesPath, oversized, http.StatusRequestEntityTooLarge)
			for _, host := range profile.redirects {
				awaitCertificate(t, address, roots, host)
				send(client, 2, host, claim, http.NoBody, http.StatusPermanentRedirect)
			}
			backend.Close()
			send(client, 2, "localhost", claim, http.NoBody, http.StatusBadGateway)
			if err := caddy.Stop(); err != nil {
				t.Fatal(err)
			}
			logged, err := os.ReadFile(logFile)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(logged, []byte(`"level":"debug"`)) {
				t.Fatalf("the log holds no debug line, so it proves nothing:\n%s", logged)
			}
			for header, value := range sentinels {
				if bytes.Contains(logged, []byte(value)) {
					t.Errorf("Caddy logged %s", header)
				}
			}
			for header, value := range responses {
				if bytes.Contains(logged, []byte(value)) {
					t.Errorf("Caddy logged the response header %s", header)
				}
			}
			if bytes.Contains(logged, []byte(messageID)) {
				t.Error("Caddy logged a message path")
			}
		})
	}
}

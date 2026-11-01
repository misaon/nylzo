package main

import (
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
	"sync"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
)

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
	_, _ = io.WriteString(w, "ok")
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
	}
	return value
}

func startCaddy(t *testing.T, caddyfile string, backend string) (address string, roots *x509.CertPool) {
	t.Helper()
	t.Setenv("NYLZO_DOMAIN", "localhost")
	source, err := os.ReadFile(caddyfile)
	if err != nil {
		t.Fatal(err)
	}
	adapted, _, err := caddyconfig.GetAdapter("caddyfile").Adapt(source, map[string]any{"filename": caddyfile})
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(adapted, &config); err != nil {
		t.Fatal(err)
	}
	httpPort, httpsPort := freePort(t), freePort(t)
	storage := t.TempDir()
	config["storage"] = map[string]any{"module": "file_system", "root": storage}
	applications := config["apps"].(map[string]any)
	applications["pki"] = map[string]any{"certificate_authorities": map[string]any{"local": map[string]any{"install_trust": false}}}
	server := applications["http"].(map[string]any)
	server["http_port"], server["https_port"] = httpPort, httpsPort
	rewrite(config, func(key string, value any) any {
		switch {
		case key == "listen":
			listeners := value.([]any)
			for i, listener := range listeners {
				port := httpsPort
				if listener == ":80" {
					port = httpPort
				}
				listeners[i] = fmt.Sprintf("127.0.0.1:%d", port)
			}
		case key == "dial":
			return backend
		case key == "issuers":
			return []any{map[string]any{"module": "internal"}}
		}
		return value
	})
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := caddy.Load(encoded, true); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = caddy.Stop() })
	address = fmt.Sprintf("localhost:%d", httpsPort)
	roots = x509.NewCertPool()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if pem, err := os.ReadFile(filepath.Join(storage, "pki", "authorities", "local", "root.crt")); err == nil && roots.AppendCertsFromPEM(pem) {
			connection, err := tls.Dial("tcp", address, &tls.Config{RootCAs: roots, ServerName: "localhost"})
			if err == nil {
				_ = connection.Close()
				return address, roots
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("Caddy did not serve a certificate in time")
	return "", nil
}

func TestCaddyfilesProtectTheEdge(t *testing.T) {
	for name, caddyfile := range map[string]string{
		"Hetzner Flatcar": filepath.Join("..", "hetzner-flatcar", "host", "proxy", "Caddyfile"),
		"Compose":         filepath.Join("..", "compose", "Caddyfile"),
	} {
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
			connection, err := tls.Dial("tcp", address, &tls.Config{RootCAs: roots, ServerName: "localhost", CurvePreferences: []tls.CurveID{tls.X25519MLKEM768}})
			if err != nil {
				t.Fatalf("X25519MLKEM768 handshake: %v", err)
			}
			_ = connection.Close()

			client := &http.Client{Transport: &http.Transport{TLSClientConfig: base.Clone(), ForceAttemptHTTP2: true}, Timeout: 10 * time.Second}
			request, _ := http.NewRequest(http.MethodGet, "https://"+address+"/check", nil)
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
			oversized, _ := http.NewRequest(http.MethodPost, "https://"+address+"/api/v1/messages", io.LimitReader(zeros{}, 7<<20))
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
			response, err = (&http.Client{Transport: first, Timeout: 10 * time.Second}).Get("https://" + address + "/first")
			if err != nil {
				t.Fatalf("HTTP/3: %v", err)
			}
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			time.Sleep(300 * time.Millisecond)
			_ = first.Close()
			var early *quic.Conn
			second := &http3.Transport{TLSClientConfig: quicConfig, Dial: func(ctx context.Context, address string, config *tls.Config, quicConfig *quic.Config) (*quic.Conn, error) {
				connection, err := quic.DialAddrEarly(ctx, address, config, quicConfig)
				early = connection
				return connection, err
			}}
			defer second.Close()
			request, _ = http.NewRequest(http3.MethodGet0RTT, "https://"+address+"/early", nil)
			response, err = (&http.Client{Transport: second, Timeout: 10 * time.Second}).Do(request)
			if err != nil {
				t.Fatalf("HTTP/3 resumption: %v", err)
			}
			_ = response.Body.Close()
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
			response, err = client.Get("https://" + address + "/unavailable")
			if err != nil {
				t.Fatal(err)
			}
			_ = response.Body.Close()
			if response.StatusCode != http.StatusBadGateway {
				t.Fatalf("status %d without an upstream, want 502", response.StatusCode)
			}
			for header, want := range map[string]string{
				"Content-Security-Policy":   "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'",
				"X-Content-Type-Options":    "nosniff",
				"Strict-Transport-Security": "max-age=63072000; includeSubDomains; preload",
				"Cache-Control":             "no-store",
				"Server":                    "",
			} {
				if got := response.Header.Get(header); got != want {
					t.Errorf("Caddy's 502 has %s %q, want %q", header, got, want)
				}
			}
		})
	}
}

func TestCaddyfilesBoundSlowAndLargeRequests(t *testing.T) {
	for name, caddyfile := range map[string]string{
		"Hetzner Flatcar": filepath.Join("..", "hetzner-flatcar", "host", "proxy", "Caddyfile"),
		"Compose":         filepath.Join("..", "compose", "Caddyfile"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("NYLZO_DOMAIN", "localhost")
			source, err := os.ReadFile(caddyfile)
			if err != nil {
				t.Fatal(err)
			}
			adapted, _, err := caddyconfig.GetAdapter("caddyfile").Adapt(source, map[string]any{"filename": caddyfile})
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
			if err := json.Unmarshal(adapted, &config); err != nil {
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
						t.Errorf("%s timeout %s is %s, want a bound of at most six minutes", name, setting, time.Duration(value))
					}
				}
				if server.MaxHeaderBytes <= 0 || server.MaxHeaderBytes > 16<<10 {
					t.Errorf("%s max_header_size is %d", name, server.MaxHeaderBytes)
				}
			}
		})
	}
}

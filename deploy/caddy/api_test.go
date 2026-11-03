package main

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/quic-go/quic-go/http3"
)

func dataClients(t *testing.T, roots *x509.CertPool) map[string]*http.Client {
	t.Helper()
	http1 := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}}
	http2 := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}, ForceAttemptHTTP2: true}
	http3Transport := &http3.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}}
	t.Cleanup(http1.CloseIdleConnections)
	t.Cleanup(http2.CloseIdleConnections)
	t.Cleanup(func() { _ = http3Transport.Close() })
	return map[string]*http.Client{
		"HTTP/1.1": {Transport: http1, Timeout: 2 * time.Second},
		"HTTP/2.0": {Transport: http2, Timeout: 2 * time.Second},
		"HTTP/3.0": {Transport: http3Transport, Timeout: 2 * time.Second},
	}
}

func TestSuccessfulNoContentSurvivesTheDataBoundary(t *testing.T) {
	for _, caddyfile := range []string{
		filepath.Join("..", "compose", "Caddyfile"),
		filepath.Join("..", "hetzner-flatcar", "host", "proxy", "Caddyfile"),
	} {
		t.Run(caddyfile, func(t *testing.T) {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Set-Cookie", "attacker=true")
				w.Header().Set("Refresh", "0;url=https://attacker.invalid")
				w.WriteHeader(http.StatusNoContent)
			}))
			defer backend.Close()
			address, roots := startCaddy(t, caddyfile, backend.Listener.Addr().String())
			for protocol, client := range dataClients(t, roots) {
				for _, action := range []string{"ack", "burn", "cancel"} {
					request, err := http.NewRequest(http.MethodPost, "https://"+address+"/api/v1/messages/test/"+action, http.NoBody)
					if err != nil {
						t.Fatal(err)
					}
					response, err := client.Do(request)
					if err != nil {
						t.Fatal(err)
					}
					body, err := io.ReadAll(response.Body)
					_ = response.Body.Close()
					if err != nil || response.StatusCode != http.StatusNoContent || len(body) != 0 || response.Proto != protocol {
						t.Fatalf("%s %s: status %d, protocol %s, body %q, error %v", protocol, action, response.StatusCode, response.Proto, body, err)
					}
					for _, name := range []string{"Set-Cookie", "Refresh", "Content-Type", "Content-Length"} {
						if response.Header.Get(name) != "" {
							t.Errorf("%s %s: unexpected %s", protocol, action, name)
						}
					}
				}
			}
		})
	}
}

func TestRejectedResponsesCloseUpstreamWithoutReadingTheirBody(t *testing.T) {
	for _, caddyfile := range []string{
		filepath.Join("..", "compose", "Caddyfile"),
		filepath.Join("..", "hetzner-flatcar", "host", "proxy", "Caddyfile"),
	} {
		t.Run(caddyfile, func(t *testing.T) {
			closed := make(chan string, 8)
			stop := make(chan struct{})
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer func() { closed <- r.URL.Query().Get("test") }()
				w.Header().Set("Content-Type", "text/html")
				w.Header().Set("Content-Length", "1000000000")
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				for {
					select {
					case <-stop:
						return
					case <-r.Context().Done():
						return
					case <-time.After(20 * time.Millisecond):
						if r.URL.Query().Get("body") == "drip" {
							if _, err := io.WriteString(w, "<script>bad()</script>"); err != nil {
								return
							}
							w.(http.Flusher).Flush()
						}
					}
				}
			}))
			defer backend.Close()
			defer close(stop)
			address, roots := startCaddy(t, caddyfile, backend.Listener.Addr().String())
			for protocol, client := range dataClients(t, roots) {
				for _, mode := range []string{"withhold", "drip"} {
					name := protocol + "/" + mode
					response, err := client.Get("https://" + address + "/api/v1/instance?test=" + name + "&body=" + mode)
					if err != nil {
						t.Fatal(err)
					}
					body, err := io.ReadAll(response.Body)
					_ = response.Body.Close()
					if err != nil || response.StatusCode != http.StatusBadGateway || string(body) != badGatewayBody || response.Proto != protocol {
						t.Fatalf("%s: status %d, body %q, protocol %s, error %v", name, response.StatusCode, body, response.Proto, err)
					}
					select {
					case got := <-closed:
						if got != name {
							t.Fatalf("closed %s, want %s", got, name)
						}
					case <-time.After(time.Second):
						t.Fatalf("%s: the refused upstream body remains open", name)
					}
				}
			}
		})
	}
}

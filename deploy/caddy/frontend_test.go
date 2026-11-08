package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/textproto"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/quic-go/quic-go/http3"

	"github.com/misaon/nylzo/internal/server/web"
	"github.com/misaon/nylzo/internal/server/webclient"
)

func TestFrontendIsIndependentOfTheBackend(t *testing.T) {
	build, err := webclient.Load(web.Files())
	if err != nil {
		t.Fatal(err)
	}
	pages, err := build.Render("https://localhost", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, configuration := range []string{
		filepath.Join("..", "hetzner-flatcar", "host", "proxy", "Caddyfile"),
		filepath.Join("..", "compose", "Caddyfile"),
	} {
		t.Run(configuration, func(t *testing.T) {
			var requests atomic.Int64
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "text/html")
				_, _ = io.WriteString(w, `<script>globalThis.compromised=true</script>`)
			}))
			defer backend.Close()
			address, roots := startCaddy(t, configuration, backend.Listener.Addr().String())
			client := &http.Client{
				Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}},
				Timeout:   10 * time.Second,
			}
			check := func(path string, expected []byte) http.Header {
				t.Helper()
				response, getErr := client.Get("https://" + address + path)
				if getErr != nil {
					t.Fatal(getErr)
				}
				body, readErr := io.ReadAll(response.Body)
				_ = response.Body.Close()
				if readErr != nil || response.StatusCode != http.StatusOK || !bytes.Equal(body, expected) {
					t.Fatalf(
						"%s: status %d, body matches embedded release %v, error %v",
						path,
						response.StatusCode,
						bytes.Equal(body, expected),
						readErr,
					)
				}
				return response.Header
			}
			for _, path := range []string{"/", "/cs/", "/r", "/sent", "/transparency", "/terms", "/agents"} {
				header := check(path, pages.For(path).Body())
				if header.Get("Content-Security-Policy") != pages.For(path).Policy() {
					t.Fatalf("%s: the frontend lost its content security policy", path)
				}
			}
			for name, asset := range build.Assets {
				check("/assets/"+name, asset.Body)
			}
			check("/robots.txt", webclient.Robots("https://localhost"))
			check("/sitemap.xml", webclient.Sitemap("https://localhost"))
			response, getErr := client.Get("https://" + address + webclient.ManifestPath)
			if getErr != nil {
				t.Fatal(getErr)
			}
			var manifest webclient.Manifest
			decodeErr := json.NewDecoder(response.Body).Decode(&manifest)
			_ = response.Body.Close()
			if decodeErr != nil || manifest.PublicURL != "https://localhost" || manifest.Built != build.Built {
				t.Fatalf("client manifest did not come from the embedded frontend: %+v, %v", manifest, decodeErr)
			}
			if requests.Load() != 0 {
				t.Fatal("a frontend request reached the hostile backend")
			}
			backend.Close()
			check("/r", pages.For("/r").Body())
		})
	}
}

func TestBackendResponsesCannotBecomeExecutableContent(t *testing.T) {
	for _, configuration := range []string{
		filepath.Join("..", "hetzner-flatcar", "host", "proxy", "Caddyfile"),
		filepath.Join("..", "compose", "Caddyfile"),
	} {
		t.Run(configuration, func(t *testing.T) {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for name, value := range map[string]string{
					"Content-Type": "application/json", "Set-Cookie": "attacker=true; Path=/", "Clear-Site-Data": `"*"`,
					"Content-Security-Policy": "default-src * 'unsafe-inline'", "X-Content-Type-Options": "unsafe",
					"Refresh": "0;url=https://attacker.invalid", "Location": "https://attacker.invalid",
					"Link": "</api/script>; rel=preload; as=script", "Service-Worker-Allowed": "/",
					"Access-Control-Allow-Origin": "*", "Cache-Control": "public, max-age=31536000",
					"Retry-After": "60", "X-Nylzo-Lease": "lease", "X-Nylzo-Share": "share", "X-Nylzo-Policy": "policy",
				} {
					w.Header().Set(name, value)
				}
				switch r.URL.Query().Get("attack") {
				case "html":
					w.Header().Set("Content-Type", "text/html")
				case "javascript":
					w.Header().Set("Content-Type", "text/javascript")
				case "encoding":
					w.Header().Set("Content-Encoding", "gzip")
				case "redirect":
					w.WriteHeader(http.StatusFound)
				case "early-hints":
					w.WriteHeader(http.StatusEarlyHints)
				case "upgrade":
					w.Header().Set("Connection", "Upgrade")
					w.Header().Set("Upgrade", "websocket")
					w.WriteHeader(http.StatusSwitchingProtocols)
					return
				default:
				}
				if strings.HasSuffix(r.URL.Path, "/blob") || strings.HasSuffix(r.URL.Path, "/claim") {
					w.Header().Set("Content-Type", "application/octet-stream")
				}
				_, _ = io.WriteString(w, `{"data":"<script>globalThis.compromised=true</script>"}`)
			}))
			defer backend.Close()
			address, roots := startCaddy(t, configuration, backend.Listener.Addr().String())
			client := &http.Client{
				Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}}, Timeout: 10 * time.Second,
				CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
			}
			for _, attack := range []string{"headers", "html", "javascript", "redirect", "encoding", "early-hints", "blob", "claim"} {
				path := "/api/v1/instance"
				method := http.MethodGet
				switch attack {
				case "blob":
					path = "/api/v1/messages/test/blob"
				case "claim":
					path = "/api/v1/messages/test/claim"
					method = http.MethodPost
				default:
				}
				request, requestErr := http.NewRequest(method, "https://"+address+path+"?attack="+attack, http.NoBody)
				if requestErr != nil {
					t.Fatal(requestErr)
				}
				var hints atomic.Int64
				request = request.WithContext(httptrace.WithClientTrace(request.Context(), &httptrace.ClientTrace{
					Got1xxResponse: func(int, textproto.MIMEHeader) error {
						hints.Add(1)
						return nil
					},
				}))
				response, err := client.Do(request)
				if err != nil {
					t.Fatal(err)
				}
				if hints.Load() != 0 {
					t.Fatal("untrusted informational response headers reached the browser")
				}
				body, err := io.ReadAll(response.Body)
				_ = response.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				want := http.StatusOK
				if attack == "html" || attack == "javascript" || attack == "redirect" || attack == "encoding" {
					want = http.StatusBadGateway
					if string(body) != `{"error":"bad_gateway"}` {
						t.Fatalf("%s: refused response leaked upstream content: %s", attack, body)
					}
				}
				if response.StatusCode != want || response.Header.Get("X-Content-Type-Options") != "nosniff" ||
					!strings.Contains(
						response.Header.Get("Content-Security-Policy"),
						"; sandbox",
					) || response.Header.Get("Cache-Control") != "no-store" {
					t.Fatalf("%s: unsafe response: %d, %v", attack, response.StatusCode, response.Header)
				}
				for _, name := range []string{"Set-Cookie", "Clear-Site-Data", "Location", "Refresh", "Link", "Service-Worker-Allowed", "Access-Control-Allow-Origin"} {
					if response.Header.Get(name) != "" {
						t.Errorf("%s: untrusted %s reached the browser", attack, name)
					}
				}
				if want == http.StatusOK {
					for _, name := range []string{"Retry-After", "X-Nylzo-Lease", "X-Nylzo-Share", "X-Nylzo-Policy"} {
						if response.Header.Get(name) == "" {
							t.Errorf("%s: protocol header %s was lost", attack, name)
						}
					}
				}
			}
			request, err := http.NewRequest(
				http.MethodGet,
				"https://"+address+"/api/upgrade?attack=upgrade",
				http.NoBody,
			)
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Connection", "Upgrade")
			request.Header.Set("Upgrade", "websocket")
			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			_ = response.Body.Close()
			if response.StatusCode != http.StatusBadGateway || response.Header.Get("Upgrade") != "" {
				t.Fatal("an upstream protocol upgrade bypassed the response boundary")
			}
		})
	}
}

func TestMaintenanceFileTurnsPagesAndDataAway(t *testing.T) {
	for _, configuration := range []string{
		filepath.Join("..", "hetzner-flatcar", "host", "proxy", "Caddyfile"),
		filepath.Join("..", "compose", "Caddyfile"),
	} {
		t.Run(configuration, func(t *testing.T) {
			var requests atomic.Int64
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "text/plain")
				_, _ = io.WriteString(w, "ok")
			}))
			defer backend.Close()
			maintenance := filepath.Join(t.TempDir(), "maintenance")
			address, roots := startCaddyWithMaintenance(t, configuration, backend.Listener.Addr().String(), maintenance)
			client := &http.Client{
				Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}},
				Timeout:   10 * time.Second,
			}
			get := func(path, destination string) (*http.Response, *http.Request) {
				t.Helper()
				request, _ := http.NewRequest(http.MethodGet, "https://"+address+path, http.NoBody)
				request.Header.Set("Sec-Fetch-Dest", destination)
				response, err := client.Do(request)
				if err != nil {
					t.Fatal(err)
				}
				return response, request
			}
			pages := embeddedPages(t, "https://localhost")

			response, _ := get("/sent", "document")
			_ = response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("page without the maintenance file: %d", response.StatusCode)
			}
			if err := os.WriteFile(maintenance, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{"/", "/sent", "/r", "/missing"} {
				pageResponse, request := get(path, "document")
				expectSameResponse(t, pageResponse, http.StatusServiceUnavailable, func(w http.ResponseWriter) {
					pages.ServeMaintenance(w, request)
				})
			}
			response, _ = get("/api/v1/instance", "empty")
			body, _ := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if response.StatusCode != http.StatusServiceUnavailable || response.Header.Get("Retry-After") != "30" ||
				string(body) != `{"error":"unavailable"}` || requests.Load() != 0 {
				t.Fatalf("API during maintenance: %d %q %s, backend requests %d",
					response.StatusCode, response.Header.Get("Retry-After"), body, requests.Load())
			}
			response, _ = get("/healthz", "empty")
			_ = response.Body.Close()
			if response.StatusCode != http.StatusOK || requests.Load() != 1 {
				t.Fatalf(
					"health check during maintenance: %d, backend requests %d",
					response.StatusCode,
					requests.Load(),
				)
			}
			if err := os.Remove(maintenance); err != nil {
				t.Fatal(err)
			}
			response, _ = get("/sent", "document")
			_ = response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("page after the maintenance file was removed: %d", response.StatusCode)
			}
		})
	}
}

func TestFrontendTakesTheOperatorContactForItsAllowlist(t *testing.T) {
	for source, want := range map[string]string{
		"nylzo_frontend https://localhost {\n contact security@example.org\n}": "security@example.org",
		"nylzo_frontend https://localhost {\n contact\n}":                      "",
		"nylzo_frontend https://localhost":                                     "",
	} {
		var frontend Frontend
		if err := frontend.UnmarshalCaddyfile(
			caddyfile.NewTestDispenser(source),
		); err != nil ||
			frontend.Contact != want {
			t.Fatalf("%q: contact %q, %v", source, frontend.Contact, err)
		}
	}
	for source, want := range map[string]string{
		"nylzo_error {\n contact security@example.org\n}": "security@example.org",
		"nylzo_error {\n contact\n}":                      "",
		"nylzo_error":                                     "",
	} {
		var page ErrorPage
		if err := page.UnmarshalCaddyfile(caddyfile.NewTestDispenser(source)); err != nil || page.Contact != want {
			t.Fatalf("%q: contact %q, %v", source, page.Contact, err)
		}
	}
	var frontend Frontend
	if err := frontend.UnmarshalCaddyfile(
		caddyfile.NewTestDispenser("nylzo_frontend https://localhost {\n contact a@b.example c@d.example\n}"),
	); err == nil {
		t.Fatal("two contacts were accepted")
	}
	var page ErrorPage
	if err := page.UnmarshalCaddyfile(caddyfile.NewTestDispenser("nylzo_error {\n status 500\n}")); err == nil {
		t.Fatal("an unknown subdirective was accepted")
	}
}

func TestClassicalTLSReachesOnlyPublicDocuments(t *testing.T) {
	for _, configuration := range []string{
		filepath.Join("..", "hetzner-flatcar", "host", "proxy", "Caddyfile"),
		filepath.Join("..", "compose", "Caddyfile"),
	} {
		t.Run(configuration, func(t *testing.T) {
			var requests atomic.Int64
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{}`)
			}))
			defer backend.Close()
			address, roots := startCaddy(t, configuration, backend.Listener.Addr().String())
			classical := &tls.Config{RootCAs: roots, CurvePreferences: []tls.CurveID{tls.X25519}}
			clients := map[string]*http.Client{
				"TCP": {
					Transport: &http.Transport{TLSClientConfig: classical, ForceAttemptHTTP2: true},
					Timeout:   10 * time.Second,
				},
				"HTTP/3": {Transport: &http3.Transport{TLSClientConfig: classical}, Timeout: 10 * time.Second},
			}
			const policy = "connect-src https://localhost/.well-known/nylzo/transparency.json https://localhost/.well-known/nylzo/client.json"
			build, err := webclient.Load(web.Files())
			if err != nil {
				t.Fatal(err)
			}
			pages := embeddedPages(t, "https://localhost")
			for transport, client := range clients {
				do := func(method, path, destination string) *http.Response {
					t.Helper()
					request, _ := http.NewRequest(method, "https://"+address+path, http.NoBody)
					request.Header.Set("Sec-Fetch-Dest", destination)
					response, doErr := client.Do(request)
					if doErr != nil {
						t.Fatalf("%s %s %s: %v", transport, method, path, doErr)
					}
					if response.TLS == nil || response.TLS.CurveID != tls.X25519 {
						t.Fatalf("%s: the connection did not use X25519 alone", transport)
					}
					return response
				}
				public := []string{
					"/",
					"/cs/",
					"/terms",
					"/agents",
					"/agents.md",
					"/robots.txt",
					"/sitemap.xml",
					webclient.ManifestPath,
				}
				for name := range build.Assets {
					public = append(public, "/assets/"+name)
				}
				for _, path := range public {
					response := do(http.MethodGet, path, "document")
					_ = response.Body.Close()
					if response.StatusCode != http.StatusOK ||
						!slices.Contains(response.Header.Values("Content-Security-Policy"), policy) {
						t.Errorf("%s %s: status %d, policies %q", transport, path, response.StatusCode,
							response.Header.Values("Content-Security-Policy"))
					}
				}
				if requests.Load() != 0 {
					t.Fatalf("%s: a public document reached the backend", transport)
				}
				declaration := do(http.MethodGet, "/.well-known/nylzo/transparency.json", "empty")
				_ = declaration.Body.Close()
				if declaration.StatusCode != http.StatusOK || requests.Swap(0) != 1 {
					t.Errorf("%s: the transparency declaration did not come from the backend: %d", transport,
						declaration.StatusCode)
				}
				for _, test := range []struct{ method, path string }{
					{http.MethodGet, "/api/v1/instance"},
					{http.MethodPost, "/api/v1/messages"},
					{http.MethodGet, "/api/v1/messages/AAAAAAAAAAAAAAAAAAAAAA/blob"},
					{http.MethodGet, "/healthz"},
					{http.MethodPost, "/"},
				} {
					refused := do(test.method, test.path, "empty")
					body, _ := io.ReadAll(refused.Body)
					_ = refused.Body.Close()
					if refused.StatusCode != http.StatusForbidden || len(body) != 0 ||
						refused.Header.Get("Content-Security-Policy") != webclient.NonHTMLCSP ||
						refused.Header.Get("X-Content-Type-Options") != "nosniff" {
						t.Errorf("%s %s %s: %d with %d bytes, %v", transport, test.method, test.path,
							refused.StatusCode, len(body), refused.Header)
					}
				}
				for _, path := range []string{"/r", "/sent", "/m/AAAAAAAAAAAAAAAAAAAAAA", "/missing"} {
					request, _ := http.NewRequest(http.MethodGet, "https://localhost"+path, http.NoBody)
					request.Header.Set("Sec-Fetch-Dest", "document")
					//nolint:bodyclose // expectSameResponse closes it.
					navigation := do(http.MethodGet, path, "document")
					expectSameResponse(t, navigation, http.StatusForbidden, func(w http.ResponseWriter) {
						pages.ServeUnsupported(w, request)
					})
				}
				if requests.Load() != 0 {
					t.Fatalf("%s: a refused request reached the backend", transport)
				}
			}
			postQuantum := &http.Client{
				Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}},
				Timeout:   10 * time.Second,
			}
			response, err := postQuantum.Get("https://" + address + "/")
			if err != nil {
				t.Fatal(err)
			}
			_ = response.Body.Close()
			if response.TLS.CurveID != tls.X25519MLKEM768 ||
				len(response.Header.Values("Content-Security-Policy")) != 1 {
				t.Fatalf("a post-quantum page got %s and policies %q", response.TLS.CurveID,
					response.Header.Values("Content-Security-Policy"))
			}
			response, err = postQuantum.Get("https://" + address + "/api/v1/instance")
			if err != nil {
				t.Fatal(err)
			}
			_ = response.Body.Close()
			if response.StatusCode != http.StatusOK || requests.Load() != 1 {
				t.Fatalf("the API refused a post-quantum connection: %d", response.StatusCode)
			}
		})
	}
}

package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/misaon/nylzo/internal/core"
)

func TestOriginAllowsHTTPSAndLoopbackOnly(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"https://Nylzo.Example":      "https://nylzo.example",
		"https://nylzo.example/":     "https://nylzo.example",
		"https://nylzo.example:8443": "https://nylzo.example:8443",
		"http://localhost:8080":      "http://localhost:8080",
		"http://127.0.0.1:9000":      "http://127.0.0.1:9000",
		"http://[::1]:9000":          "http://[::1]:9000",
	} {
		if got, err := Origin(in); err != nil || got != want {
			t.Fatalf("%s: %q %v, want %q", in, got, err, want)
		}
	}
	for in, want := range map[string]error{
		"http://nylzo.example":           ErrInsecureServer,
		"http://10.0.0.1":                ErrInsecureServer,
		"http://localhost.nylzo.example": ErrInsecureServer,
		"http://app.localhost":           ErrInsecureServer,
		"https://nylzo.example/api":      ErrServerURL,
		"https://user@nylzo.example":     ErrServerURL,
		"https://nylzo.example/?a=1":     ErrServerURL,
		"https://nylzo.example/#1.0.abc": ErrServerURL,
		"https://nylzo.example?":         ErrServerURL,
		"ftp://nylzo.example":            ErrServerURL,
		"nylzo.example":                  ErrServerURL,
	} {
		if _, err := Origin(in); !errors.Is(err, want) {
			t.Fatalf("%s: %v, want %v", in, err, want)
		}
	}
}

func TestErrorsCarryTheServerReason(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"passphrase","retry_after":0,"attempts_left":3}`))
	}))
	t.Cleanup(ts.Close)
	c, err := New(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Instance(t.Context())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Kind != "passphrase" || apiErr.AttemptsLeft != 3 || apiErr.RetryAfter.Seconds() != 7 {
		t.Fatalf("%+v", err)
	}
}

func TestEnvelopesAreBoundedByTheProtocol(t *testing.T) {
	t.Parallel()
	limit := core.ObjectLimit(core.MaxFileBytes)
	policy := core.Encode([]byte(`{"opens_max":1,"ttl_s":60,"kdf_id":0,"flags":0}`))
	for name, c := range map[string]struct {
		length string
		body   int64
	}{
		"declared too large":                     {strconv.FormatInt(limit+1, 10), 1},
		"longer than the limit without a length": {"", limit + 1},
	} {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Nylzo-Policy", policy)
			if c.length != "" {
				w.Header().Set("Content-Length", c.length)
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(strings.Repeat("x", int(c.body))))
		}))
		resp, err := http.Get(ts.URL)
		if err != nil {
			ts.Close()
			if c.length != "" {
				continue
			}
			t.Fatal(err)
		}
		_, err = readEnvelope(resp)
		resp.Body.Close()
		ts.Close()
		if !errors.Is(err, ErrResponse) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	for _, header := range []string{"", "!!", core.Encode([]byte(`{"opens_max":1,"ttl_s":60,"kdf_id":9,"flags":0}`)), core.Encode([]byte(`{"opens_max":0,"ttl_s":60,"kdf_id":0,"flags":0}`)), core.Encode([]byte(`{"ttl_s":60,"kdf_id":0,"flags":0}`))} {
		if _, err := decodePolicy(header); !errors.Is(err, ErrResponse) {
			t.Fatalf("policy %q accepted", header)
		}
	}
}

func TestRedirectIsAnError(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.RedirectHandler("https://elsewhere.example/", http.StatusTemporaryRedirect))
	t.Cleanup(ts.Close)
	c, err := New(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Instance(context.Background()); !errors.Is(err, ErrRedirect) {
		t.Fatalf("redirect: %v", err)
	}
}

func TestPlainHTTPDialsOnlyLoopback(t *testing.T) {
	t.Parallel()
	for address, ok := range map[string]bool{
		"127.0.0.1:8080": true, "127.8.9.10:1": true, "[::1]:8080": true, "[::ffff:127.0.0.1]:80": true,
		"203.0.113.7:80": false, "[2001:db8::1]:443": false, "10.0.0.1:80": false, "localhost:80": false,
	} {
		if err := loopbackOnly("tcp", address, nil); (err == nil) != ok {
			t.Fatalf("%s: %v", address, err)
		}
	}
	c, err := New("http://localhost:1")
	if err != nil {
		t.Fatal(err)
	}
	transport := c.http.Transport.(*http.Transport)
	if transport.Proxy != nil || transport.DialContext == nil {
		t.Fatal("plain http to localhost may go through a proxy or dial anything")
	}
	if _, err := transport.DialContext(t.Context(), "tcp", "192.0.2.1:80"); !errors.Is(err, ErrInsecureServer) {
		t.Fatalf("plain http dialed a non-loopback address: %v", err)
	}
}

func TestServerStringsAreValidated(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"state":"\u001b]0;pwned\u0007","opens_used":0,"opens_max":1,"failed_passphrase_attempts":0}`,
		`{"state":"available","created_at":"\u001b[2Kfake","opens_used":0,"opens_max":1,"failed_passphrase_attempts":0}`,
	} {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v1/challenge" {
				w.Write([]byte(`{"challenge":"` + core.Encode(make([]byte, core.ChallengeLen)) + `","expires_at":"2026-10-24T00:00:00Z"}`))
				return
			}
			w.Write([]byte(body))
		}))
		c, err := New(ts.URL)
		if err != nil {
			t.Fatal(err)
		}
		_, err = c.Status(t.Context(), core.NewID(), core.NewSecret())
		ts.Close()
		if !errors.Is(err, ErrResponse) {
			t.Fatalf("%s accepted: %v", body, err)
		}
	}
}

func TestMetadataStringsAreValidated(t *testing.T) {
	t.Parallel()
	for _, body := range []string{`{"state":"weird","kdf_id":0}`, `{"state":"available","expires_at":"soon","kdf_id":0}`} {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v1/challenge" {
				w.Write([]byte(`{"challenge":"` + core.Encode(make([]byte, core.ChallengeLen)) + `","expires_at":"2026-10-24T00:00:00Z"}`))
				return
			}
			w.Write([]byte(body))
		}))
		c, err := New(ts.URL)
		if err != nil {
			t.Fatal(err)
		}
		_, err = c.Metadata(t.Context(), core.NewID(), core.NewSecret())
		ts.Close()
		if !errors.Is(err, ErrResponse) {
			t.Fatalf("%s accepted: %v", body, err)
		}
	}
}

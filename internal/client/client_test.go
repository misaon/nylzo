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
		"http://app.localhost":       "http://app.localhost",
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

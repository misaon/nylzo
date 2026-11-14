package client

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/core/envelope"
	"github.com/misaon/nylzo/internal/core/share"
	"github.com/misaon/nylzo/internal/core/signing"
)

func serve(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := New(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestMutationRequestMarkers(t *testing.T) {
	t.Parallel()
	for _, method := range []string{
		http.MethodGet, http.MethodHead, http.MethodOptions,
		http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete,
	} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			client := serve(t, func(w http.ResponseWriter, r *http.Request) {
				want := "XMLHttpRequest"
				if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
					want = ""
				}
				if got := r.Header.Get("X-Requested-With"); got != want {
					t.Errorf("%s request marker = %q, want %q", method, got, want)
				}
				w.WriteHeader(http.StatusNoContent)
			})
			response, err := client.do(t.Context(), method, "/", nil, nil, shortTimeout)
			if err != nil {
				t.Fatal(err)
			}
			if closeErr := response.Body.Close(); closeErr != nil {
				t.Fatal(closeErr)
			}
		})
	}
}

func TestOriginAllowsHTTPSAndLoopbackOnly(t *testing.T) {
	t.Parallel()
	for server, want := range map[string]string{
		"https://Nylzo.Example":      "https://nylzo.example",
		"https://nylzo.example/":     "https://nylzo.example",
		"https://nylzo.example:8443": "https://nylzo.example:8443",
		"http://localhost:8080":      "http://localhost:8080",
		"http://127.0.0.1:9000":      "http://127.0.0.1:9000",
		"http://[::1]:9000":          "http://[::1]:9000",
	} {
		if origin, err := Origin(server); err != nil || origin != want {
			t.Fatalf("%s: %q %v, want %q", server, origin, err, want)
		}
	}
	for server, want := range map[string]error{
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
		if _, err := Origin(server); !errors.Is(err, want) {
			t.Fatalf("%s: %v, want %v", server, err, want)
		}
	}
}

func TestPlainHTTPDialsOnlyLoopback(t *testing.T) {
	t.Parallel()
	for address, allowed := range map[string]bool{
		"127.0.0.1:8080": true, "127.8.9.10:1": true, "[::1]:8080": true, "[::ffff:127.0.0.1]:80": false,
		"203.0.113.7:80": false, "[2001:db8::1]:443": false, "10.0.0.1:80": false, "localhost:80": false,
	} {
		if err := loopbackOnly("tcp", address, nil); (err == nil) != allowed {
			t.Fatalf("%s: %v", address, err)
		}
	}
	client, err := New("http://localhost:1")
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := client.http.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil || transport.DialContext == nil {
		t.Fatal("plain http to localhost may go through a proxy or dial anything")
	}
	if _, err = transport.DialContext(t.Context(), "tcp", "192.0.2.1:80"); !errors.Is(err, ErrInsecureServer) {
		t.Fatalf("plain http dialed a non-loopback address: %v", err)
	}
}

func TestRedirectIsAnError(t *testing.T) {
	t.Parallel()
	client := serve(t, http.RedirectHandler("https://elsewhere.example/", http.StatusTemporaryRedirect).ServeHTTP)
	if _, err := client.Instance(t.Context(), nil); !errors.Is(err, ErrRedirect) {
		t.Fatalf("redirect: %v", err)
	}
}

// A-04: a difficulty above the spec maximum fails before any key is derived, not after minutes of solving. F13: a
// deadline that does not end the TTL counted from the issue of the id, or another epoch than its own, is refused before
// anything is sealed.
func TestInstanceRefusesWhatTheSpecForbids(t *testing.T) {
	t.Parallel()
	creation := &Creation{TTLSeconds: 3600, ShareKey: share.NewKey().PublicKey()}
	wrapped := core.Encode(make([]byte, share.WrappedLen))
	response := core.Encode(make([]byte, 64))
	epochOf := func(deadline string) string {
		parsed, err := time.Parse(time.RFC3339, deadline)
		if err != nil {
			return "1"
		}
		return strconv.FormatInt(parsed.Unix()/60-1, 10)
	}
	issued := func(deadline string) string {
		return `{"limits":{},"pow":{},"create":{"id":"AAAAAAAAAAAAAAAAAAAAAA","id_token":"t",` +
			`"expires_at":"2026-11-12T10:10:00Z","epoch":` + epochOf(deadline) + `,"deadline":"` + deadline +
			`","share":"` + wrapped + `","registration":"` + response + `"}}`
	}
	valid := issued("2026-11-12T11:00:00Z")
	for name, test := range map[string]struct {
		body     string
		creation *Creation
		valid    bool
	}{
		"limits only":                {`{"limits":{},"pow":{"difficulty":8}}`, nil, true},
		"an issued id":               {valid, creation, true},
		"a deadline rounded up":      {issued("2026-11-12T11:01:00Z"), creation, true},
		"difficulty above 8":         {`{"limits":{},"pow":{"difficulty":9}}`, nil, false},
		"difficulty below 0":         {`{"limits":{},"pow":{"difficulty":-1}}`, nil, false},
		"no id when asked":           {`{"limits":{},"pow":{}}`, creation, false},
		"a deadline before the TTL":  {issued("2026-11-12T10:59:00Z"), creation, false},
		"a deadline after the TTL":   {issued("2026-11-12T11:02:00Z"), creation, false},
		"a deadline inside a minute": {issued("2026-11-12T11:00:30Z"), creation, false},
		"an epoch of another minute": {strings.Replace(valid, `"epoch":`, `"epoch":1`, 1), creation, false},
		"an id without an epoch":     {strings.Replace(valid, `"epoch":`, `"skipped":`, 1), creation, false},
		"a short share":              {strings.Replace(valid, wrapped, "AAAA", 1), creation, false},
		"a server without deadlines": {strings.Replace(valid, `"deadline":`, `"skipped":`, 1), creation, false},
		"no registration":            {strings.Replace(valid, `,"registration":"`+response+`"`, "", 1), creation, false},
	} {
		server := serve(t, func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(test.body)) })
		if _, err := server.Instance(t.Context(), test.creation); (err == nil) != test.valid {
			t.Errorf("%s: %v", name, err)
		}
	}
	outdated := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(strings.Replace(valid, `"deadline":`, `"skipped":`, 1)))
	})
	if _, err := outdated.Instance(t.Context(), creation); !errors.Is(err, ErrServerOutdated) {
		t.Fatalf("an instance that issues no deadline: %v", err)
	}
}

func TestErrorsCarryTheServerReason(t *testing.T) {
	t.Parallel()
	client := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"passphrase","retry_after":0,"attempts_left":3}`))
	})
	_, err := client.Instance(t.Context(), nil)
	apiErr, ok := errors.AsType[*APIError](err)
	if !ok || apiErr.Kind != "passphrase" || apiErr.AttemptsLeft != 3 || apiErr.RetryAfter.Seconds() != 7 {
		t.Fatalf("%+v", err)
	}
}

func TestServerStringsAreValidated(t *testing.T) {
	t.Parallel()
	challenge := `{"challenge":"` + core.Encode(
		make([]byte, signing.ChallengeLen),
	) + `","expires_at":"2026-10-24T00:00:00Z"}`
	for _, body := range []string{
		`{"state":"\u001b]0;pwned\u0007","opens_used":0,"opens_max":1,"failed_passphrase_attempts":0}`,
		`{"state":"available","created_at":"\u001b[2Kfake","opens_max":1,"failed_passphrase_attempts":0}`,
		`{"state":"weird","kdf_id":0}`,
		`{"state":"available","expires_at":"soon","kdf_id":0}`,
	} {
		client := serve(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v1/challenge" {
				w.Write([]byte(challenge))
				return
			}
			w.Write([]byte(body))
		})
		if _, err := client.Status(t.Context(), core.NewID(), core.NewSecret()); !errors.Is(err, ErrResponse) {
			t.Fatalf("status %s accepted: %v", body, err)
		}
		if _, err := client.Metadata(t.Context(), core.NewID(), core.NewSecret()); !errors.Is(err, ErrResponse) {
			t.Fatalf("metadata %s accepted: %v", body, err)
		}
	}
}

// The client takes the state lost, which a server answers for a message whose key it lost (F21), from metadata, status
// and a gone answer alike.
func TestTheLostStateIsAccepted(t *testing.T) {
	t.Parallel()
	challenge := `{"challenge":"` + core.Encode(
		make([]byte, signing.ChallengeLen),
	) + `","expires_at":"2026-10-24T00:00:00Z"}`
	answer := func(status int, body string) *Client {
		return serve(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v1/challenge" {
				w.Write([]byte(challenge))
				return
			}
			w.WriteHeader(status)
			w.Write([]byte(body))
		})
	}
	live := answer(http.StatusOK, `{"state":"lost","kdf_id":0}`)
	if metadata, err := live.Metadata(t.Context(), core.NewID(), core.NewSecret()); err != nil ||
		metadata.State != api.StateLost {
		t.Fatalf("metadata: %+v %v", metadata, err)
	}
	if status, err := live.Status(t.Context(), core.NewID(), core.NewSecret()); err != nil ||
		status.State != api.StateLost {
		t.Fatalf("status: %+v %v", status, err)
	}
	gone := answer(http.StatusGone, `{"error":"gone","state":"lost"}`)
	apiErr, ok := errors.AsType[*APIError](gone.Cancel(t.Context(), core.NewID(), core.NewSecret()))
	if !ok || apiErr.State != api.StateLost {
		t.Fatalf("gone: %+v", apiErr)
	}
}

func TestMetadataRefusesASizeTheProtocolDoesNotAllow(t *testing.T) {
	t.Parallel()
	challenge := `{"challenge":"` + core.Encode(
		make([]byte, signing.ChallengeLen),
	) + `","expires_at":"2026-10-24T00:00:00Z"}`
	for _, size := range []int64{-1, envelope.MaxObjectBytes + 1} {
		client := serve(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v1/challenge" {
				w.Write([]byte(challenge))
				return
			}
			w.Write([]byte(`{"state":"available","kdf_id":0,"size":` + strconv.FormatInt(size, 10) + `}`))
		})
		if _, err := client.Metadata(t.Context(), core.NewID(), core.NewSecret()); !errors.Is(err, ErrResponse) {
			t.Fatalf("size %d accepted: %v", size, err)
		}
	}
}

func TestEnvelopesAreBoundedByTheProtocol(t *testing.T) {
	t.Parallel()
	limit := envelope.ObjectLimit(envelope.MaxFileBytes)
	policy := core.Encode([]byte(`{"opens_max":1,"ttl_s":60,"kdf_id":0,"flags":0}`))
	for name, test := range map[string]struct {
		length string
		body   int64
	}{
		"declared too large":                     {strconv.FormatInt(limit+1, 10), 1},
		"longer than the limit without a length": {"", limit + 1},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Nylzo-Policy", policy)
			if test.length != "" {
				w.Header().Set("Content-Length", test.length)
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(strings.Repeat("x", int(test.body))))
		}))
		response, err := http.Get(server.URL)
		if err != nil {
			server.Close()
			if test.length != "" {
				continue
			}
			t.Fatal(err)
		}
		_, err = readEnvelope(response)
		response.Body.Close()
		server.Close()
		if !errors.Is(err, ErrResponse) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	for _, header := range []string{
		"",
		"!!",
		core.Encode([]byte(`{"opens_max":1,"ttl_s":60,"kdf_id":9,"flags":0}`)),
		core.Encode([]byte(`{"opens_max":1,"ttl_s":60,"kdf_id":257,"flags":0}`)),
		core.Encode([]byte(`{"opens_max":0,"ttl_s":60,"kdf_id":0,"flags":0}`)),
		core.Encode([]byte(`{"ttl_s":60,"kdf_id":0,"flags":0}`)),
		core.Encode([]byte(`{"opens_max":1,"ttl_s":60,"kdf_id":0,"flags":2}`)),
		core.Encode([]byte(`{"opens_max":1,"ttl_s":60,"kdf_id":0,"flags":0,"deadline":1797000000}`)),
		core.Encode([]byte(`{"opens_max":1,"ttl_s":60,"kdf_id":0,"flags":2,"deadline":0}`)),
		core.Encode([]byte(`{"opens_max":1,"ttl_s":60,"kdf_id":0,"flags":2,"deadline":4294967296}`)),
	} {
		if _, err := decodePolicy(header); !errors.Is(err, ErrResponse) {
			t.Fatalf("policy %q accepted", header)
		}
	}
	sealed, err := decodePolicy(
		core.Encode([]byte(`{"opens_max":1,"ttl_s":60,"kdf_id":0,"flags":2,"deadline":1797000000}`)),
	)
	if err != nil || sealed.Policy.Deadline != 1_797_000_000 || sealed.Flags != envelope.FlagDeadline {
		t.Fatalf("a policy that seals its deadline: %+v %v", sealed, err)
	}
}

// F13: the message must expire at the deadline the client sealed, and a reader names the flags it opens.
func TestCreateRefusesAnotherExpiryAndReadersNameTheirFlags(t *testing.T) {
	t.Parallel()
	creator := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":"AAAAAAAAAAAAAAAAAAAAAA","expires_at":"2026-11-12T11:01:00Z"}`))
	})
	request := &CreateRequest{Deadline: time.Date(2026, 11, 12, 11, 0, 0, 0, time.UTC)}
	if _, err := creator.Create(t.Context(), request); !errors.Is(err, ErrResponse) {
		t.Fatalf("an expiry other than the sealed deadline: %v", err)
	}
	challenge := `{"challenge":"` + core.Encode(make([]byte, signing.ChallengeLen)) + `"}`
	var flags []string
	reader := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == api.ChallengePath {
			w.Write([]byte(challenge))
			return
		}
		flags = append(flags, r.Header.Get(api.FlagsHeaderName))
		w.WriteHeader(http.StatusGone)
		w.Write([]byte(`{"error":"gone","state":"expired"}`))
	})
	id := core.NewID()
	reader.Claim(t.Context(), id, core.NewSecret(), &Login{}, 0, nil)
	reader.Blob(t.Context(), id, core.NewSecret(), "lease", 0)
	if len(flags) != 2 || flags[0] != "2" || flags[1] != "2" {
		t.Fatalf("readers sent X-Nylzo-Flags %q", flags)
	}
}

func TestRetriedDownloadDrawsANewShareKeyAndReportsTheOneAnswered(t *testing.T) {
	t.Parallel()
	challenge := `{"challenge":"` + core.Encode(
		make([]byte, signing.ChallengeLen),
	) + `","expires_at":"2026-10-24T00:00:00Z"}`
	policy := core.Encode([]byte(`{"opens_max":1,"ttl_s":60,"kdf_id":0,"flags":0}`))
	var sent []string
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/challenge" {
			w.Write([]byte(challenge))
			return
		}
		sent = append(sent, r.Header.Get(api.ShareKeyHeaderName))
		if len(sent) == 1 {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"not_found"}`))
			return
		}
		w.Header().Set("X-Nylzo-Policy", policy)
		w.WriteHeader(http.StatusOK)
	})
	fetched, err := client.Blob(t.Context(), core.NewID(), core.NewSecret(), core.Encode(make([]byte, 32)), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sent) != 2 || sent[0] == sent[1] || sent[0] == "" {
		t.Fatalf("share keys sent: %q", sent)
	}
	if fetched.ShareKey == nil || share.FormatKey(fetched.ShareKey.PublicKey()) != sent[1] {
		t.Fatal("the download did not report the share key of the answered attempt")
	}
}

func TestAClaimSignsTheChallengeOfItsHandshakeAndNeverRetries(t *testing.T) {
	t.Parallel()
	var claims atomic.Int32
	login := &Login{Challenge: [signing.ChallengeLen]byte{7}, KE3: [64]byte{3}}
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == api.ChallengePath {
			t.Error("a claim fetched a challenge of its own")
		}
		claims.Add(1)
		signature, err := signing.ParseAuthorization(r.Header.Get("Authorization"))
		components := signing.Components{
			ShareKey: r.Header.Get(api.ShareKeyHeaderName),
			KE3:      r.Header.Get(api.KE3HeaderName),
		}
		if err != nil || signature.Challenge != login.Challenge || components.KE3 != core.Encode(login.KE3[:]) ||
			!signature.Verify(signing.PurposeRead, r.Method, r.URL.Path, nil, components) {
			t.Error("the claim does not sign the handshake's challenge and its KE3")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not_found"}`))
	})
	if _, err := client.Claim(
		t.Context(),
		core.NewID(),
		core.NewSecret(),
		login,
		0,
		nil,
	); !IsKind(
		err,
		api.ErrorNotFound,
	) {
		t.Fatalf("got %v", err)
	}
	if claims.Load() != 1 {
		t.Fatalf("a claim with the challenge of a handshake was sent %d times", claims.Load())
	}
}

func TestSignedRetryBindsFreshAttemptHeaders(t *testing.T) {
	t.Parallel()
	var attempts atomic.Int32
	var drawn atomic.Int32
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == api.ChallengePath {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"challenge":%q}`, core.Encode(make([]byte, signing.ChallengeLen)))
			return
		}
		components := signing.Components{
			ShareKey: r.Header.Get(api.ShareKeyHeaderName),
			Lease:    r.Header.Get(api.LeaseHeaderName),
		}
		if components.ShareKey != strconv.Itoa(int(attempts.Load()+1)) || components.Lease != "lease-token" {
			t.Errorf("wrong attempt components: %+v", components)
		}
		signature, err := signing.ParseAuthorization(r.Header.Get("Authorization"))
		if err != nil || !signature.Verify(signing.PurposeRead, r.Method, r.URL.Path, nil, components) {
			t.Error("read signature does not cover the attempt headers")
		}
		if attempts.Add(1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"not_found"}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	request := signedRequest{
		method: http.MethodPost, path: "/claim", purpose: signing.PurposeRead,
		seed:    core.NewSecret(),
		headers: map[string]string{api.LeaseHeaderName: "lease-token"},
		attemptHeaders: func() map[string]string {
			return map[string]string{api.ShareKeyHeaderName: strconv.Itoa(int(drawn.Add(1)))}
		},
	}
	response, err := client.signed(t.Context(), &request, shortTimeout)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if attempts.Load() != 2 || drawn.Load() != 2 {
		t.Fatal("retry did not generate and sign fresh headers")
	}
}

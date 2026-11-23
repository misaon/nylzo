package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/core/signing"
)

var validChallenge = fmt.Sprintf(`{"challenge":%q}`, core.Encode(make([]byte, signing.ChallengeLen)))

func cutShort(w http.ResponseWriter, header http.Header) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		panic("the test server cannot hand over its connection")
	}
	connection, buffered, err := hijacker.Hijack()
	if err != nil {
		panic(err)
	}
	defer connection.Close()
	fmt.Fprint(buffered, "HTTP/1.1 200 OK\r\nContent-Length: 100\r\n")
	for name, values := range header {
		for _, value := range values {
			fmt.Fprintf(buffered, "%s: %s\r\n", name, value)
		}
	}
	fmt.Fprint(buffered, "\r\n{")
	buffered.Flush()
}

func answerChallengeThen(challenge string, answer http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == api.ChallengePath {
			w.Write([]byte(challenge))
			return
		}
		answer(w, r)
	}
}

func TestTheClientRefusesBrokenAnswers(t *testing.T) {
	t.Parallel()
	id, seed := core.NewID(), core.NewSecret()
	login := &Login{}
	cut := func(w http.ResponseWriter, _ *http.Request) { cutShort(w, nil) }
	notJSON := func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("{")) }
	failing := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }
	policy := core.Encode([]byte(`{"opens_max":1,"ttl_s":60,"kdf_id":0,"deadline":1797000000}`))
	for name, test := range map[string]struct {
		answer http.HandlerFunc
		call   func(context.Context, *Client) error
	}{
		"a fetch answered without content": {
			func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) },
			func(ctx context.Context, c *Client) error { _, err := c.Fetch(ctx, "/", "identity", 10); return err },
		},
		"a fetch cut short": {
			cut,
			func(ctx context.Context, c *Client) error { _, err := c.Fetch(ctx, "/", "identity", 1000); return err },
		},
		"a fetch of a path that is no URL": {
			notJSON,
			func(ctx context.Context, c *Client) error {
				_, err := c.Fetch(ctx, "/\x7f", "identity", 10)
				return err
			},
		},
		"an instance cut short": {
			cut,
			func(ctx context.Context, c *Client) error { _, err := c.Instance(ctx, nil); return err },
		},
		"a create answered without JSON": {
			notJSON,
			func(ctx context.Context, c *Client) error { _, err := c.Create(ctx, &CreateRequest{}); return err },
		},
		"a failed challenge before metadata": {
			failing,
			func(ctx context.Context, c *Client) error { _, err := c.Metadata(ctx, id, seed); return err },
		},
		"a challenge without JSON before a status": {
			notJSON,
			func(ctx context.Context, c *Client) error { _, err := c.Status(ctx, id, seed); return err },
		},
		"a challenge of the wrong length before a cancel": {
			answerChallengeThen(`{"challenge":"AAAA"}`, failing),
			func(ctx context.Context, c *Client) error { return c.Cancel(ctx, id, seed) },
		},
		"a claim without a lease": {
			func(w http.ResponseWriter, _ *http.Request) { w.Header().Set(api.PolicyHeaderName, policy) },
			func(ctx context.Context, c *Client) error { _, err := c.Claim(ctx, id, seed, login, 0); return err },
		},
		"a failed download": {
			answerChallengeThen(validChallenge, failing),
			func(ctx context.Context, c *Client) error { _, err := c.Blob(ctx, id, seed, "lease", 0); return err },
		},
		"a download cut short": {
			answerChallengeThen(validChallenge, func(w http.ResponseWriter, _ *http.Request) {
				cutShort(w, http.Header{api.PolicyHeaderName: {policy}})
			}),
			func(ctx context.Context, c *Client) error { _, err := c.Blob(ctx, id, seed, "lease", 0); return err },
		},
		"a download with a damaged share": {
			answerChallengeThen(validChallenge, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set(api.PolicyHeaderName, policy)
				w.Header().Set(api.ShareHeaderName, "AAAA")
			}),
			func(ctx context.Context, c *Client) error { _, err := c.Blob(ctx, id, seed, "lease", 0); return err },
		},
		"a download with a damaged policy": {
			answerChallengeThen(validChallenge, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set(api.PolicyHeaderName, "AAAA")
			}),
			func(ctx context.Context, c *Client) error { _, err := c.Blob(ctx, id, seed, "lease", 0); return err },
		},
	} {
		server := serve(t, test.answer)
		if err := test.call(t.Context(), server); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestPlainHTTPRefusesADialAddressWithoutAPort(t *testing.T) {
	t.Parallel()
	if err := loopbackOnly("tcp", "127.0.0.1", nil); !errors.Is(err, ErrInsecureServer) {
		t.Fatalf("an address without a port: %v", err)
	}
	var unreachable net.ListenConfig
	listener, err := unreachable.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	closed, err := New("http://127.0.0.1:" + strconv.Itoa(port))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err = closed.Instance(ctx, nil); err == nil || errors.Is(err, ErrRedirect) {
		t.Fatalf("a closed port: %v", err)
	}
}

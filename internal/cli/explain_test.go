package cli

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/cli/console"
	"github.com/misaon/nylzo/internal/client"
	"github.com/misaon/nylzo/internal/core/keys"
)

func TestExplainNetworkFailuresWithoutRepeatingRemoteText(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		err  error
		want string
	}{
		{"DNS", &net.DNSError{Err: "secret remote text", Name: "private.example.test"}, "the server address cannot be resolved"},
		{"deadline", context.DeadlineExceeded, "the server did not answer in time"},
		{"connection", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("secret remote text")}, "cannot connect to the server"},
		{"hostname", x509.HostnameError{Certificate: &x509.Certificate{DNSNames: []string{"private.example.test"}}, Host: "private.example.test"}, "the server's TLS certificate is not valid for its address"},
		{"authority", x509.UnknownAuthorityError{Cert: &x509.Certificate{}}, "the server's TLS certificate is not from a trusted authority"},
		{"expired", x509.CertificateInvalidError{Cert: &x509.Certificate{}, Reason: x509.Expired}, "the server's TLS certificate is invalid or has expired"},
		{"terminal", console.ErrNoTerminal, "there is no terminal to ask on; use a file instead"},
		{"passphrase", keys.ErrMalformedPassphrase, "the passphrase is not valid UTF-8"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := explain(fmt.Errorf("request failed: %w", test.err)); got != test.want {
				t.Fatalf("explain = %q, want %q", got, test.want)
			}
		})
	}
}

func TestExplainLocalRefusalsAndUnknownErrors(t *testing.T) {
	t.Parallel()
	for _, err := range []error{client.ErrInsecureServer, client.ErrServerURL, client.ErrRedirect, client.ErrResponse, errors.New("local synthetic failure")} {
		if got := explain(err); got != err.Error() {
			t.Fatalf("local error %v became %q", err, got)
		}
	}
}

func TestExplainWeakPassphrasesIncludesAUsefulLocalHint(t *testing.T) {
	t.Parallel()
	for problem, fragment := range map[keys.PassphraseProblem]string{
		keys.PassphraseShort:                     "at least 15 characters",
		keys.PassphraseRepetitive:                "repeats too few",
		keys.PassphraseSequential:                "run of letters",
		keys.PassphraseContext:                   "mostly the word nylzo",
		keys.PassphraseCommon:                    "commonly used passwords",
		keys.PassphraseUnassigned:                "Unicode " + keys.AssignedUnicodeVersion,
		keys.PassphraseProblem("future-problem"): "too weak",
	} {
		t.Run(string(problem), func(t *testing.T) {
			t.Parallel()
			got := explain(fmt.Errorf("create: %w", &keys.WeakPassphraseError{Problem: problem}))
			hasHint := strings.Contains(got, "--generate-passphrase-file makes a strong one")
			if !strings.Contains(got, fragment) || !hasHint {
				t.Fatalf("weak passphrase explanation: %q", got)
			}
		})
	}
}

func TestExplainAPIFailuresAndRetryAdvice(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		kind, state string
		attempts    int
		retry       time.Duration
		want        string
	}{
		{"gone", "opened", 0, 0, "the message was already opened"},
		{"gone", "expired", 0, 0, "the message has expired"},
		{"gone", "cancelled", 0, 0, "the sender cancelled the message"},
		{"gone", "deleted", 0, 0, "the message was deleted"},
		{"gone", "untrusted state", 0, 0, "the message is no longer available"},
		{"not_found", "", 0, 0, "no such message: the link or token is wrong, or the message is gone"},
		{"passphrase", "", 2, 0, "wrong passphrase; 2 attempts left before the message makes every reader wait"},
		{"passphrase", "", 2, 30 * time.Second, "wrong passphrase, or too many wrong attempts; try again in 30s"},
		{"rate_limited", "", 0, 30 * time.Second, "the server limits requests from your address; try again in 30s"},
		{"unavailable", "", 0, 0, "the server is busy or has reached its daily limit"},
		{"too_large", "", 0, 0, "the message is larger than the server accepts"},
		{"pow", "", 0, 0, "the server refused the proof of work; try again"},
		{"id_token", "", 0, 0, "the server refused the new message id; try again"},
		{"conflict", "", 0, 0, "the server refused the new message id; try again"},
		{"internal", "", 0, 0, "the server failed; try again later"},
		{"untrusted error", "", 0, 0, "the server answered with status 599"},
	} {
		t.Run(test.kind+test.state, func(t *testing.T) {
			t.Parallel()
			err := &client.APIError{
				Status: 599, Kind: test.kind, State: test.state,
				AttemptsLeft: test.attempts, RetryAfter: test.retry,
			}
			if got := explain(fmt.Errorf("server: %w", err)); got != test.want {
				t.Fatalf("explain = %q, want %q", got, test.want)
			}
		})
	}
}

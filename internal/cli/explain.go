package cli

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io/fs"
	"net"

	"github.com/misaon/nylzo/internal/client"
	"github.com/misaon/nylzo/internal/core"
)

var endedStates = map[string]string{
	"opened":    "the message was already opened",
	"expired":   "the message has expired",
	"cancelled": "the sender cancelled the message",
	"deleted":   "the message was deleted",
}

func explain(err error) string {
	var apiErr *client.APIError
	var hostErr x509.HostnameError
	var authorityErr x509.UnknownAuthorityError
	var invalidErr x509.CertificateInvalidError
	var dnsErr *net.DNSError
	var opErr *net.OpError
	switch {
	case errors.As(err, &apiErr):
		return explainAPI(apiErr)
	case errors.As(err, &hostErr):
		return "the server's TLS certificate is not valid for its address"
	case errors.As(err, &authorityErr):
		return "the server's TLS certificate is not from a trusted authority"
	case errors.As(err, &invalidErr):
		return "the server's TLS certificate is invalid or has expired"
	case errors.Is(err, client.ErrInsecureServer), errors.Is(err, client.ErrServerURL), errors.Is(err, client.ErrRedirect), errors.Is(err, client.ErrResponse):
		return err.Error()
	case errors.As(err, &dnsErr):
		return "the server address cannot be resolved"
	case errors.Is(err, context.DeadlineExceeded):
		return "the server did not answer in time"
	case errors.As(err, &opErr):
		return "cannot connect to the server"
	case errors.Is(err, core.ErrWeakPassphrase):
		return fmt.Sprintf("the passphrase must have at least %d characters", core.MinPassphraseChars)
	case errors.Is(err, core.ErrMalformedPassphrase):
		return "the passphrase is not valid UTF-8"
	case errors.Is(err, ErrNoTerminal):
		return "there is no terminal to ask on; use a file instead"
	case errors.Is(err, fs.ErrNotExist):
		return err.Error()
	}
	return err.Error()
}

func explainAPI(e *client.APIError) string {
	wait := ""
	if e.RetryAfter > 0 {
		wait = fmt.Sprintf("; try again in %s", e.RetryAfter)
	}
	switch e.Kind {
	case "gone":
		if text, ok := endedStates[e.State]; ok {
			return text
		}
		return "the message is no longer available"
	case "not_found":
		return "no such message: the link or token is wrong, or the message is gone"
	case "passphrase":
		if e.AttemptsLeft > 0 && e.RetryAfter == 0 {
			return fmt.Sprintf("wrong passphrase; %d attempts left before the message makes every reader wait", e.AttemptsLeft)
		}
		return "wrong passphrase, or too many wrong attempts" + wait
	case "rate_limited":
		return "the server limits requests from your address" + wait
	case "unavailable":
		return "the server is busy or has reached its daily limit" + wait
	case "too_large":
		return "the message is larger than the server accepts"
	case "pow":
		return "the server refused the proof of work; try again"
	case "id_token", "conflict":
		return "the server refused the new message id; try again"
	case "internal":
		return "the server failed; try again later"
	}
	return fmt.Sprintf("the server answered with status %d", e.Status)
}

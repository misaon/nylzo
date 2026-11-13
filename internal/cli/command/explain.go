package command

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"

	"github.com/misaon/nylzo/internal/cli/console"
	"github.com/misaon/nylzo/internal/client"
	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/core/keys"
)

type explanationError struct {
	explanation string
}

func (e *explanationError) Error() string {
	return e.explanation
}

func Explained(err error) error {
	return &explanationError{explanation: Explain(err)}
}

var endedStates = map[string]string{
	api.StateOpened:    "the message was already opened",
	api.StateExpired:   "the message has expired",
	api.StateCancelled: "the sender cancelled the message",
	api.StateDeleted:   "the message was deleted",
}

// Explain turns an error into a message that never repeats text from the server or a certificate.
func Explain(err error) string {
	if apiErr, ok := errors.AsType[*client.APIError](err); ok {
		return explainAPI(apiErr)
	}
	if text, ok := explainCertificate(err); ok {
		return text
	}
	clientRefused := errors.Is(err, client.ErrInsecureServer) || errors.Is(err, client.ErrServerURL) ||
		errors.Is(err, client.ErrRedirect) || errors.Is(err, client.ErrResponse)
	if clientRefused {
		return err.Error()
	}
	if _, ok := errors.AsType[*net.DNSError](err); ok {
		return "the server address cannot be resolved"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "the server did not answer in time"
	}
	if _, ok := errors.AsType[*net.OpError](err); ok {
		return "cannot connect to the server"
	}
	return explainLocal(err)
}

func explainCertificate(err error) (string, bool) {
	if _, ok := errors.AsType[x509.HostnameError](err); ok {
		return "the server's TLS certificate is not valid for its address", true
	}
	if _, ok := errors.AsType[x509.UnknownAuthorityError](err); ok {
		return "the server's TLS certificate is not from a trusted authority", true
	}
	if _, ok := errors.AsType[x509.CertificateInvalidError](err); ok {
		return "the server's TLS certificate is invalid or has expired", true
	}
	return "", false
}

func explainLocal(err error) string {
	switch {
	case errors.Is(err, keys.ErrWeakPassphrase):
		return weakPassphraseText(err)
	case errors.Is(err, keys.ErrMalformedPassphrase):
		return "the passphrase is not valid UTF-8"
	case errors.Is(err, console.ErrNoTerminal):
		return "there is no terminal to ask on; use a file instead"
	default:
		return err.Error()
	}
}

func weakPassphraseText(err error) string {
	const hint = "; --generate-passphrase-file makes a strong one"
	weak, _ := errors.AsType[*keys.WeakPassphraseError](err)
	switch weak.Problem {
	case keys.PassphraseShort:
		return fmt.Sprintf("the passphrase must have at least %d characters%s", keys.MinPassphraseCharacters, hint)
	case keys.PassphraseRepetitive:
		return "the passphrase repeats too few different characters" + hint
	case keys.PassphraseSequential:
		return "the passphrase is a run of letters, digits or keyboard keys" + hint
	case keys.PassphraseContext:
		return "the passphrase is mostly the word nylzo" + hint
	case keys.PassphraseCommon:
		return "the passphrase is on the list of commonly used passwords" + hint
	case keys.PassphraseUnassigned:
		return fmt.Sprintf(
			"the passphrase has a character that Unicode %s does not know, so another device could read it differently%s",
			keys.AssignedUnicodeVersion,
			hint,
		)
	default:
		return "the passphrase is too weak" + hint
	}
}

var apiErrors = map[string]string{
	api.ErrorNotFound:       "no such message: the link or token is wrong, or the message is gone",
	api.ErrorTooLarge:       "the message is larger than the server accepts",
	api.ErrorWork:           "the server refused the proof of work; try again",
	api.ErrorIDToken:        "the server refused the new message id; try again",
	api.ErrorConflict:       "the server refused the new message id; try again",
	api.ErrorInternal:       "the server failed; try again later",
	api.ErrorClientOutdated: "this nylzo is older than the server; update it",
}

func explainAPI(apiErr *client.APIError) string {
	wait := ""
	if apiErr.RetryAfter > 0 {
		wait = fmt.Sprintf("; try again in %s", apiErr.RetryAfter)
	}
	switch apiErr.Kind {
	case api.ErrorGone:
		if text, ok := endedStates[apiErr.State]; ok {
			return text
		}
		return "the message is no longer available"
	case api.ErrorPassphrase:
		if apiErr.AttemptsLeft > 0 && apiErr.RetryAfter == 0 {
			return fmt.Sprintf(
				"wrong passphrase; %d attempts left before the message makes every reader wait",
				apiErr.AttemptsLeft,
			)
		}
		return "wrong passphrase, or too many wrong attempts" + wait
	case api.ErrorRateLimited:
		return "the server limits requests from your address" + wait
	case api.ErrorUnavailable:
		return "the server is busy or has reached its daily limit" + wait
	default:
		if text, ok := apiErrors[apiErr.Kind]; ok {
			return text
		}
		return fmt.Sprintf("the server answered with status %d", apiErr.Status)
	}
}

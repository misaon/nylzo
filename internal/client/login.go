package client

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/core/keys"
	"github.com/misaon/nylzo/internal/core/opaque"
	"github.com/misaon/nylzo/internal/core/signing"
)

// ErrServerAuthentication is a KE2 whose server mac does not verify: the answer did not come from the server that
// holds the registration of the message.
var ErrServerAuthentication = errors.New("client: the server's answer does not match the registration of the message")

// Login is a finished handshake: the challenge and KE3 that its claim or burn signs, and P (spec section 6.4).
type Login struct {
	Challenge [signing.ChallengeLen]byte
	KE3       [opaque.KE3Len]byte
	ExportKey keys.ExportKey
}

func (l *Login) Wipe() {
	clear(l.ExportKey[:])
}

type handshakeAnswer struct {
	ke2          [opaque.KE2Len]byte
	challenge    [signing.ChallengeLen]byte
	attemptsLeft int
	retryAfter   time.Duration
}

// LogIn runs the handshake for the OPRF input password and finishes it. A wrong passphrase leaves the envelope shut;
// it fails like a refusal of the server, with the counters that the handshake reported, and sends no KE3. Without a
// passphrase only a server with other keys leaves it shut, so that fails as ErrServerAuthentication.
func (c *Client) LogIn(
	ctx context.Context,
	id core.ID,
	read *[core.SeedLen]byte,
	kdf core.KDF,
	password []byte,
	config *opaque.Config,
) (Login, error) {
	ke1, state, err := opaque.GenerateKE1(password)
	defer state.Wipe()
	if err != nil {
		return Login{}, err
	}
	answer, err := c.handshake(ctx, id, read, ke1[:])
	if err != nil {
		return Login{}, err
	}
	result, err := state.GenerateKE3(answer.ke2[:], config)
	defer result.Wipe()
	switch {
	case errors.Is(err, opaque.ErrEnvelopeRecovery) && kdf == core.KDFNone:
		return Login{}, ErrServerAuthentication
	case errors.Is(err, opaque.ErrEnvelopeRecovery):
		return Login{}, &APIError{
			Status:       http.StatusForbidden,
			Kind:         api.ErrorPassphrase,
			RetryAfter:   answer.retryAfter,
			AttemptsLeft: answer.attemptsLeft,
		}
	case errors.Is(err, opaque.ErrServerAuthentication):
		return Login{}, ErrServerAuthentication
	case err != nil:
		return Login{}, ErrResponse
	}
	return Login{Challenge: answer.challenge, KE3: result.KE3, ExportKey: result.ExportKey}, nil
}

func (c *Client) handshake(
	ctx context.Context,
	id core.ID,
	read *[core.SeedLen]byte,
	ke1 []byte,
) (handshakeAnswer, error) {
	request := signedRequest{
		method:  http.MethodPost,
		path:    api.MessagePath(id.String()) + api.HandshakeAction,
		purpose: signing.PurposeRead,
		seed:    read,
		headers: map[string]string{api.KE1HeaderName: core.Encode(ke1)},
	}
	response, err := c.signed(ctx, &request, shortTimeout)
	if err != nil {
		return handshakeAnswer{}, err
	}
	var body api.Handshake
	if readErr := readJSON(response, &body); readErr != nil {
		return handshakeAnswer{}, readErr
	}
	ke2, ke2Err := core.DecodeExact(body.KE2, opaque.KE2Len)
	challenge, challengeErr := core.DecodeExact(body.Challenge, signing.ChallengeLen)
	if ke2Err != nil || challengeErr != nil || body.AttemptsLeft < 0 || body.RetryAfter < 0 {
		return handshakeAnswer{}, ErrResponse
	}
	return handshakeAnswer{
		ke2:          [opaque.KE2Len]byte(ke2),
		challenge:    [signing.ChallengeLen]byte(challenge),
		attemptsLeft: body.AttemptsLeft,
		retryAfter:   time.Duration(body.RetryAfter) * time.Second,
	}, nil
}

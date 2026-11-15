package client

import (
	"context"
	"crypto/hpke"
	"encoding/base64"
	"encoding/json"
	"io"
	"math"
	"net/http"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/core/envelope"
	"github.com/misaon/nylzo/internal/core/share"
	"github.com/misaon/nylzo/internal/core/signing"
)

// Envelope is a served object with the expiry share, still wrapped to the client's one-time key.
type Envelope struct {
	Policy envelope.Policy
	KDF    core.KDF
	Object []byte
	Share  *[share.WrappedLen]byte
	// ShareKey is the one-time key of the attempt the server answered, which Share is wrapped to.
	ShareKey hpke.PrivateKey
}

type Claimed struct {
	Envelope

	Lease string
}

// Claim finishes a login with its KE3 and always sends a share key, so its headers do not show whether the message
// has a passphrase.
func (c *Client) Claim(
	ctx context.Context,
	id core.ID,
	read *[core.SeedLen]byte,
	login *Login,
	size int64,
) (Claimed, error) {
	sent := &sentShareKey{}
	request := signedRequest{
		method:         http.MethodPost,
		path:           api.MessagePath(id.String()) + api.ClaimAction,
		purpose:        signing.PurposeRead,
		seed:           read,
		challenge:      &login.Challenge,
		headers:        map[string]string{api.KE3HeaderName: core.Encode(login.KE3[:])},
		attemptHeaders: sent.headers,
	}
	response, err := c.signed(ctx, &request, TransferTime(size))
	if err != nil {
		return Claimed{}, err
	}
	defer response.Body.Close()
	lease := response.Header.Get(api.LeaseHeaderName)
	if lease == "" {
		return Claimed{}, ErrResponse
	}
	received, err := readEnvelope(response)
	if err != nil {
		return Claimed{Lease: lease}, err
	}
	received.ShareKey = sent.key
	return Claimed{Lease: lease, Envelope: received}, nil
}

func (c *Client) Blob(
	ctx context.Context,
	id core.ID,
	read *[core.SeedLen]byte,
	lease string,
	size int64,
) (Envelope, error) {
	sent := &sentShareKey{}
	request := signedRequest{
		method:         http.MethodGet,
		path:           api.MessagePath(id.String()) + api.BlobAction,
		purpose:        signing.PurposeRead,
		seed:           read,
		headers:        map[string]string{api.LeaseHeaderName: lease},
		attemptHeaders: sent.headers,
	}
	response, err := c.signed(ctx, &request, TransferTime(size))
	if err != nil {
		return Envelope{}, err
	}
	defer response.Body.Close()
	received, err := readEnvelope(response)
	received.ShareKey = sent.key
	return received, err
}

type sentShareKey struct {
	key hpke.PrivateKey
}

func (s *sentShareKey) headers() map[string]string {
	s.key = share.NewKey()
	return map[string]string{api.ShareKeyHeaderName: share.FormatKey(s.key.PublicKey())}
}

func (c *Client) Ack(ctx context.Context, id core.ID, read *[core.SeedLen]byte, lease string) error {
	request := signedRequest{
		method:  http.MethodPost,
		path:    api.MessagePath(id.String()) + api.AckAction,
		purpose: signing.PurposeRead,
		seed:    read,
		headers: map[string]string{api.LeaseHeaderName: lease},
	}
	return c.finish(ctx, &request)
}

func readEnvelope(response *http.Response) (Envelope, error) {
	received, err := decodePolicy(response.Header.Get(api.PolicyHeaderName))
	if err != nil {
		return Envelope{}, err
	}
	if wrapped := response.Header.Get(api.ShareHeaderName); wrapped != "" {
		decoded, shareErr := core.DecodeExact(wrapped, share.WrappedLen)
		if shareErr != nil {
			return Envelope{}, ErrResponse
		}
		received.Share = (*[share.WrappedLen]byte)(decoded)
	}
	limit := envelope.ObjectLimit(envelope.MaxFileBytes)
	if response.ContentLength > limit {
		return Envelope{}, ErrResponse
	}
	object, err := readObject(response, limit)
	if err != nil {
		return Envelope{}, err
	}
	length := int64(len(object))
	if length > limit || (response.ContentLength >= 0 && length != response.ContentLength) {
		return Envelope{}, ErrResponse
	}
	received.Object = object
	return received, nil
}

// readObject reads a body of known length into one allocation and falls back to a bounded read for a chunked one.
func readObject(response *http.Response, limit int64) ([]byte, error) {
	if response.ContentLength < 0 {
		return io.ReadAll(io.LimitReader(response.Body, limit+1))
	}
	object := make([]byte, response.ContentLength)
	if _, err := io.ReadFull(response.Body, object); err != nil {
		return nil, err
	}
	return object, nil
}

func decodePolicy(header string) (Envelope, error) {
	data, err := base64.RawURLEncoding.Strict().DecodeString(header)
	if err != nil || header == "" {
		return Envelope{}, ErrResponse
	}
	var policy api.PolicyHeader
	err = json.Unmarshal(data, &policy)
	if err != nil || policy.OpensMax == nil || policy.TTLSeconds == nil || policy.KDFID == nil ||
		policy.Deadline == nil {
		return Envelope{}, ErrResponse
	}
	opensMax, ttl, kdf, deadline := *policy.OpensMax, *policy.TTLSeconds, *policy.KDFID, *policy.Deadline
	profile, kdfOK := kdfOf(kdf)
	if opensMax < 1 || opensMax > math.MaxUint16 || ttl < 1 || ttl > math.MaxUint32 || !kdfOK ||
		deadline < 1 || deadline > math.MaxUint32 {
		return Envelope{}, ErrResponse
	}
	return Envelope{
		Policy: envelope.Policy{OpensMax: uint16(opensMax), TTLSeconds: uint32(ttl), Deadline: uint32(deadline)},
		KDF:    profile,
	}, nil
}

func kdfOf(value int) (core.KDF, bool) {
	if value < 0 || value > math.MaxUint8 {
		return 0, false
	}
	kdf := core.KDF(value)
	return kdf, kdf.Valid()
}

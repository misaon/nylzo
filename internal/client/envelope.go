package client

import (
	"context"
	"crypto/ecdh"
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
	Flags  int
	Object []byte
	Share  *[share.WrappedLen]byte
	// ShareKey is the one-time key of the attempt the server answered, which Share is wrapped to.
	ShareKey *ecdh.PrivateKey
}

type Claimed struct {
	Envelope

	Lease string
}

// Claim always sends a verifier and a share key, so its headers do not show whether the message has a passphrase.
func (c *Client) Claim(
	ctx context.Context,
	id core.ID,
	read, verifier *[core.SeedLen]byte,
	size int64,
	onLease func(string),
) (Claimed, error) {
	sent := &sentShareKey{}
	request := signedRequest{
		method:         http.MethodPost,
		path:           api.MessagePath(id.String()) + api.ClaimAction,
		purpose:        signing.PurposeRead,
		seed:           read,
		verifier:       verifier,
		attemptHeaders: sent.headers,
	}
	response, err := c.signed(ctx, &request, transferTime(size))
	if err != nil {
		return Claimed{}, err
	}
	defer response.Body.Close()
	lease := response.Header.Get(api.LeaseHeaderName)
	if lease == "" {
		return Claimed{}, ErrResponse
	}
	if onLease != nil {
		onLease(lease)
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
	response, err := c.signed(ctx, &request, transferTime(size))
	if err != nil {
		return Envelope{}, err
	}
	defer response.Body.Close()
	received, err := readEnvelope(response)
	received.ShareKey = sent.key
	return received, err
}

type sentShareKey struct {
	key *ecdh.PrivateKey
}

func (s *sentShareKey) headers() map[string]string {
	s.key = share.NewKey()
	return map[string]string{api.ShareKeyHeaderName: core.Encode(s.key.PublicKey().Bytes())}
}

func (c *Client) Ack(ctx context.Context, id core.ID, read *[core.SeedLen]byte, lease string) error {
	request := signedRequest{
		method:  http.MethodPost,
		path:    api.MessagePath(id.String()) + api.AckAction,
		purpose: signing.PurposeRead,
		seed:    read,
		headers: map[string]string{api.LeaseHeaderName: lease},
	}
	response, err := c.signed(ctx, &request, shortTimeout)
	if err != nil {
		return err
	}
	return drain(response)
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
	if err != nil || policy.OpensMax == nil || policy.TTLSeconds == nil || policy.KDFID == nil || policy.Flags == nil {
		return Envelope{}, ErrResponse
	}
	opensMax, ttl, kdf := *policy.OpensMax, *policy.TTLSeconds, *policy.KDFID
	if opensMax < 1 || opensMax > math.MaxUint16 || ttl < 1 || ttl > math.MaxUint32 || kdf < 0 ||
		kdf > math.MaxUint8 || !core.KDF(kdf).Valid() {
		return Envelope{}, ErrResponse
	}
	return Envelope{
		Policy: envelope.Policy{OpensMax: uint16(opensMax), TTLSeconds: uint32(ttl)},
		KDF:    core.KDF(kdf),
		Flags:  *policy.Flags,
	}, nil
}

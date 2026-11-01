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
	"github.com/misaon/nylzo/internal/core/envelope"
	"github.com/misaon/nylzo/internal/core/share"
	"github.com/misaon/nylzo/internal/core/signing"
)

const shareKeyHeader = "X-Nylzo-Share-Key"

// Envelope is a served object with the expiry share, still wrapped to the client's one-time key.
type Envelope struct {
	Policy envelope.Policy
	KDF    core.KDF
	Flags  int
	Object []byte
	Share  *[share.WrappedLen]byte
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
	shareKey *ecdh.PublicKey,
	size int64,
	onLease func(string),
) (Claimed, error) {
	request := signedRequest{
		method:   http.MethodPost,
		path:     messagePath(id) + "/claim",
		purpose:  signing.PurposeRead,
		seed:     read,
		verifier: verifier,
		headers:  map[string]string{shareKeyHeader: core.Encode(shareKey.Bytes())},
	}
	response, err := c.signed(ctx, &request, transferTime(size))
	if err != nil {
		return Claimed{}, err
	}
	defer response.Body.Close()
	lease := response.Header.Get("X-Nylzo-Lease")
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
	return Claimed{Lease: lease, Envelope: received}, nil
}

func (c *Client) Blob(
	ctx context.Context,
	id core.ID,
	read *[core.SeedLen]byte,
	shareKey *ecdh.PublicKey,
	lease string,
	size int64,
) (Envelope, error) {
	request := signedRequest{
		method:  http.MethodGet,
		path:    messagePath(id) + "/blob",
		purpose: signing.PurposeRead,
		seed:    read,
		headers: map[string]string{"X-Nylzo-Lease": lease, shareKeyHeader: core.Encode(shareKey.Bytes())},
	}
	response, err := c.signed(ctx, &request, transferTime(size))
	if err != nil {
		return Envelope{}, err
	}
	defer response.Body.Close()
	return readEnvelope(response)
}

func (c *Client) Ack(ctx context.Context, id core.ID, read *[core.SeedLen]byte, lease string) error {
	request := signedRequest{
		method:  http.MethodPost,
		path:    messagePath(id) + "/ack",
		purpose: signing.PurposeRead,
		seed:    read,
		headers: map[string]string{"X-Nylzo-Lease": lease},
	}
	response, err := c.signed(ctx, &request, shortTimeout)
	if err != nil {
		return err
	}
	return drain(response)
}

func readEnvelope(response *http.Response) (Envelope, error) {
	received, err := decodePolicy(response.Header.Get("X-Nylzo-Policy"))
	if err != nil {
		return Envelope{}, err
	}
	if wrapped := response.Header.Get("X-Nylzo-Share"); wrapped != "" {
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
	object, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
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

func decodePolicy(header string) (Envelope, error) {
	data, err := base64.RawURLEncoding.Strict().DecodeString(header)
	if err != nil || header == "" {
		return Envelope{}, ErrResponse
	}
	var policy struct {
		OpensMax *int `json:"opens_max"`
		TTL      *int `json:"ttl_s"`
		KDF      *int `json:"kdf_id"`
		Flags    *int `json:"flags"`
	}
	err = json.Unmarshal(data, &policy)
	if err != nil || policy.OpensMax == nil || policy.TTL == nil || policy.KDF == nil || policy.Flags == nil {
		return Envelope{}, ErrResponse
	}
	opensMax, ttl, kdf := *policy.OpensMax, *policy.TTL, *policy.KDF
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

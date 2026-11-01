package vectors

import (
	"crypto/ecdh"
	"encoding/hex"
	"errors"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/envelope"
	"github.com/misaon/nylzo/internal/core/share"
)

type ShareCase struct {
	Name          string  `json:"name"`
	ClientPrivate string  `json:"client_private_hex"`
	ClientPublic  string  `json:"client_public"`
	ServerPrivate *string `json:"server_private_hex,omitempty"`
	E             *string `json:"e_hex,omitempty"`
	Wrapped       string  `json:"wrapped"`
	Valid         bool    `json:"valid"`
}

type CompressedLength struct {
	Original  int `json:"original_len"`
	Needed    int `json:"needed_len"`
	ObjectLen int `json:"object_len"`
}

func (f *File) share() error {
	client, err := ecdh.X25519().NewPrivateKey(deterministic("share/client", share.KeyLen))
	if err != nil {
		return err
	}
	server, err := ecdh.X25519().NewPrivateKey(deterministic("share/server", share.KeyLen))
	if err != nil {
		return err
	}
	expiry := deterministic32("share/e")
	wrapped, err := share.WrapWith(server, expiry, client.PublicKey())
	if err != nil {
		return err
	}
	unwrapped, err := share.Unwrap(client, &wrapped)
	if err != nil || unwrapped != *expiry {
		return errors.Join(errors.New("vectors: the share does not unwrap"), err)
	}
	f.Share = append(f.Share, ShareCase{
		Name:          "wrapped-share",
		ClientPrivate: hex.EncodeToString(client.Bytes()),
		ClientPublic:  core.Encode(client.PublicKey().Bytes()),
		ServerPrivate: new(hex.EncodeToString(server.Bytes())),
		E:             new(hex.EncodeToString(expiry[:])),
		Wrapped:       core.Encode(wrapped[:]),
		Valid:         true,
	})
	lowOrder := wrapped
	clear(lowOrder[:share.KeyLen])
	if _, err = share.Unwrap(client, &lowOrder); err == nil {
		return errors.New("vectors: a low-order server key unwrapped")
	}
	f.Share = append(f.Share, ShareCase{
		Name:          "low-order-server-key",
		ClientPrivate: hex.EncodeToString(client.Bytes()),
		ClientPublic:  core.Encode(client.PublicKey().Bytes()),
		Wrapped:       core.Encode(lowOrder[:]),
	})
	return nil
}

func (f *File) compressedLength() error {
	const block = envelope.Pad
	for _, pair := range [][2]int{
		{block, block},
		{2 * block, block},
		{2 * block, 2 * block},
		{3 * block, block},
		{3 * block, 2 * block},
		{16 * block, block},
		{16 * block, 3 * block},
		{16 * block, 8 * block},
		{16 * block, 9 * block},
		{82 * block, 11 * block},
		{82 * block, 41 * block},
		{82 * block, 42 * block},
	} {
		f.CompressedLength = append(f.CompressedLength, CompressedLength{
			Original:  pair[0],
			Needed:    pair[1],
			ObjectLen: envelope.CompressedLength(pair[0], pair[1]),
		})
	}
	return nil
}

package vectors

import (
	"bytes"
	"crypto/hpke"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/envelope"
	"github.com/misaon/nylzo/internal/core/share"
)

var (
	errShareKeyAccepted  = errors.New("is accepted")
	errShareKeyKind      = errors.New("is refused for another reason")
	errShareNotUnwrapped = errors.New("vectors: the share does not unwrap")
	errBadShareUnwrapped = errors.New("vectors: a damaged share unwrapped")
)

const (
	mlkemKeyLen        = share.KeyLen - x25519Len
	mlkemCiphertextLen = share.EncapsulatedLen - x25519Len
	x25519Len          = 32
)

type ShareCase struct {
	Name       string  `json:"name"`
	ClientSeed string  `json:"client_seed_hex"`
	ClientKey  string  `json:"client_key"`
	E          *string `json:"e_hex,omitempty"`
	Wrapped    string  `json:"wrapped"`
	Valid      bool    `json:"valid"`
}

// ShareKeyRejected is an X-Nylzo-Share-Key value a server must refuse. Outdated marks the X25519 key of a client from
// before share suites, which the server answers with client_outdated instead of bad_request.
type ShareKeyRejected struct {
	Name     string `json:"name"`
	Key      string `json:"key"`
	Outdated bool   `json:"outdated,omitempty"`
}

// smallOrderPoints are the X25519 points whose every shared secret is zero (RFC 7748 section 5).
var smallOrderPoints = []struct {
	name string
	hex  string
}{
	{"zero", "0000000000000000000000000000000000000000000000000000000000000000"},
	{"one", "0100000000000000000000000000000000000000000000000000000000000000"},
	{"order-8-first", "e0eb7a7c3b41b8ae1656e3faf19fc46ada098deb9c32b1fd866205165f49b800"},
	{"order-8-second", "5f9c95bca3508c24b1d0b1559c83ef5b04445cc4581c8e86d8224eddd09f1157"},
	{"p-minus-one", "ecffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff7f"},
	{"p", "edffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff7f"},
	{"p-plus-one", "eeffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff7f"},
}

func deterministicShareKey(label string) (hpke.PrivateKey, []byte, error) {
	seed := deterministic(label, 32)
	key, err := share.NewKeyFromSeed(seed)
	return key, seed, err
}

func (f *File) shareKeyRejected() error {
	key, _, keyErr := deterministicShareKey("share/rejected")
	if keyErr != nil {
		return keyErr
	}
	public := key.PublicKey().Bytes()
	encoded := core.Encode(public)
	add := func(name, value string, outdated bool) error {
		_, err := share.ParseKey(value)
		switch {
		case err == nil:
			return fmt.Errorf("vectors: the share key %s %w", name, errShareKeyAccepted)
		case errors.Is(err, share.ErrOutdated) != outdated:
			return fmt.Errorf("vectors: the share key %s %w", name, errShareKeyKind)
		}
		f.ShareKeyRejected = append(f.ShareKeyRejected, ShareKeyRejected{Name: name, Key: value, Outdated: outdated})
		return nil
	}
	errs := []error{
		add("x25519-without-suite", core.Encode(public[mlkemKeyLen:]), true),
		add("x-wing-without-suite", encoded, false),
		add("empty", "", false),
		add("unknown-suite", "2."+encoded, false),
		add("empty-suite", "."+encoded, false),
		add("short", suiteKey(public[:share.KeyLen-1]), false),
		add("ml-kem-coefficient-q", suiteKey(withCoefficientQ(public)), false),
	}
	for _, point := range smallOrderPoints {
		half, err := hex.DecodeString(point.hex)
		if err != nil {
			return err
		}
		lowOrder := append(bytes.Clone(public[:mlkemKeyLen]), half...)
		errs = append(errs, add("x25519-"+point.name, suiteKey(lowOrder), false))
		lowOrder[share.KeyLen-1] |= 0x80
		errs = append(errs, add("x25519-"+point.name+"-top-bit", suiteKey(lowOrder), false))
	}
	return errors.Join(errs...)
}

func suiteKey(raw []byte) string {
	return strconv.Itoa(share.Suite) + "." + core.Encode(raw)
}

// withCoefficientQ sets the first coefficient of the ML-KEM half to q = 3329, which the modulus check of FIPS 203
// section 7.2 refuses.
func withCoefficientQ(public []byte) []byte {
	changed := bytes.Clone(public)
	changed[0] = 0x01
	changed[1] = changed[1]&0xf0 | 0x0d
	return changed
}

type CompressedLength struct {
	Original  int `json:"original_len"`
	Needed    int `json:"needed_len"`
	ObjectLen int `json:"object_len"`
}

func (f *File) share() error {
	client, seed, err := deterministicShareKey("share/client")
	if err != nil {
		return err
	}
	expiry := deterministic32("share/e")
	sealed, err := sealDeterministic(
		client.PublicKey().Bytes(),
		expiry[:],
		deterministic("share/encapsulation", 32),
		deterministic("share/ephemeral", x25519Len),
	)
	if err != nil {
		return err
	}
	wrapped := [share.WrappedLen]byte(sealed)
	unwrapped, err := share.Unwrap(client, &wrapped)
	if err != nil || unwrapped != *expiry {
		return errors.Join(errShareNotUnwrapped, err)
	}
	clientKey := share.FormatKey(client.PublicKey())
	f.Share = append(f.Share, ShareCase{
		Name:       "wrapped-share",
		ClientSeed: hex.EncodeToString(seed),
		ClientKey:  clientKey,
		E:          new(hex.EncodeToString(expiry[:])),
		Wrapped:    core.Encode(wrapped[:]),
		Valid:      true,
	})
	damaged := []struct {
		name   string
		damage func(*[share.WrappedLen]byte)
	}{
		{"low-order-encapsulation", func(w *[share.WrappedLen]byte) {
			clear(w[mlkemCiphertextLen:share.EncapsulatedLen])
		}},
		{"changed-ml-kem-ciphertext", func(w *[share.WrappedLen]byte) { w[0] ^= 1 }},
		{"changed-x25519-ciphertext", func(w *[share.WrappedLen]byte) { w[mlkemCiphertextLen] ^= 1 }},
		{"changed-tag", func(w *[share.WrappedLen]byte) { w[share.WrappedLen-1] ^= 1 }},
	}
	for _, change := range damaged {
		bad := wrapped
		change.damage(&bad)
		if _, err = share.Unwrap(client, &bad); err == nil {
			return fmt.Errorf("%w: %s", errBadShareUnwrapped, change.name)
		}
		f.Share = append(f.Share, ShareCase{
			Name:       change.name,
			ClientSeed: hex.EncodeToString(seed),
			ClientKey:  clientKey,
			Wrapped:    core.Encode(bad[:]),
		})
	}
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

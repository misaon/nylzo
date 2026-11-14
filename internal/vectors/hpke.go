package vectors

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/mlkem"
	"crypto/mlkem/mlkemtest"
	"crypto/sha256"
	"crypto/sha3"
	"encoding/binary"

	"github.com/misaon/nylzo/internal/core/share"
)

const (
	shareInfo  = "nylzo/v2/share"
	xwingLabel = `\./` + `/^\`
)

// hpkeSuite is RFC 9180 suite_id for MLKEM768-X25519, HKDF-SHA256 and AES-256-GCM.
var hpkeSuite = []byte("HPKE\x64\x7a\x00\x01\x00\x02")

// sealDeterministic is HPKE.SealBase of share suite 1 with chosen encapsulation randomness, which crypto/hpke does not
// take, so the committed vectors stay stable; share() opens every result with crypto/hpke.
func sealDeterministic(clientKey, plaintext, mlkemRandom, ephemeralSeed []byte) ([]byte, error) {
	pqKey, err := mlkem.NewEncapsulationKey768(clientKey[:mlkemKeyLen])
	if err != nil {
		return nil, err
	}
	pqSecret, pqCiphertext, err := mlkemtest.Encapsulate768(pqKey, mlkemRandom)
	if err != nil {
		return nil, err
	}
	peer, err := ecdh.X25519().NewPublicKey(clientKey[mlkemKeyLen:])
	if err != nil {
		return nil, err
	}
	ephemeral, err := ecdh.X25519().NewPrivateKey(ephemeralSeed)
	if err != nil {
		return nil, err
	}
	classicalSecret, err := ephemeral.ECDH(peer)
	if err != nil {
		return nil, err
	}
	classicalCiphertext := ephemeral.PublicKey().Bytes()
	combiner := sha3.New256()
	for _, part := range [][]byte{pqSecret, classicalSecret, classicalCiphertext, peer.Bytes(), []byte(xwingLabel)} {
		combiner.Write(part)
	}
	scheduled, err := keySchedule(combiner.Sum(nil), []byte(shareInfo))
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(scheduled.key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	sealed := make([]byte, 0, share.WrappedLen)
	sealed = append(append(sealed, pqCiphertext...), classicalCiphertext...)
	return aead.Seal(sealed, scheduled.nonce, plaintext, nil), nil
}

type scheduledKey struct {
	key, nonce []byte
}

// keySchedule is the base mode KeySchedule of RFC 9180 section 5.1 with an empty PSK.
func keySchedule(sharedSecret, info []byte) (scheduledKey, error) {
	pskIDHash, err := labeledExtract(nil, "psk_id_hash", nil)
	if err != nil {
		return scheduledKey{}, err
	}
	infoHash, err := labeledExtract(nil, "info_hash", info)
	if err != nil {
		return scheduledKey{}, err
	}
	context := append(append([]byte{0}, pskIDHash...), infoHash...)
	secret, err := labeledExtract(sharedSecret, "secret", nil)
	if err != nil {
		return scheduledKey{}, err
	}
	key, err := labeledExpand(secret, "key", context, 32)
	if err != nil {
		return scheduledKey{}, err
	}
	nonce, err := labeledExpand(secret, "base_nonce", context, 12)
	return scheduledKey{key: key, nonce: nonce}, err
}

func labeledExtract(salt []byte, label string, ikm []byte) ([]byte, error) {
	labeled := append(append(append([]byte("HPKE-v1"), hpkeSuite...), label...), ikm...)
	return hkdf.Extract(sha256.New, labeled, salt)
}

func labeledExpand(prk []byte, label string, info []byte, length uint16) ([]byte, error) {
	labeled := binary.BigEndian.AppendUint16(nil, length)
	labeled = append(append(append(append(labeled, "HPKE-v1"...), hpkeSuite...), label...), info...)
	return hkdf.Expand(sha256.New, prk, string(labeled), int(length))
}

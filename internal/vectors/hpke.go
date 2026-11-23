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
	"fmt"

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
func sealDeterministic(clientKey, plaintext, mlkemRandom, ephemeralSeed []byte) []byte {
	pqKey := must(mlkem.NewEncapsulationKey768(clientKey[:mlkemKeyLen]))
	pqSecret, pqCiphertext, err := mlkemtest.Encapsulate768(pqKey, mlkemRandom)
	require(err == nil, fmt.Errorf("vectors: ML-KEM encapsulation: %w", err))
	peer := must(ecdh.X25519().NewPublicKey(clientKey[mlkemKeyLen:]))
	ephemeral := must(ecdh.X25519().NewPrivateKey(ephemeralSeed))
	classicalSecret := must(ephemeral.ECDH(peer))
	classicalCiphertext := ephemeral.PublicKey().Bytes()
	combiner := sha3.New256()
	for _, part := range [][]byte{pqSecret, classicalSecret, classicalCiphertext, peer.Bytes(), []byte(xwingLabel)} {
		combiner.Write(part)
	}
	scheduled := keySchedule(combiner.Sum(nil), []byte(shareInfo))
	aead := must(cipher.NewGCM(must(aes.NewCipher(scheduled.key))))
	sealed := make([]byte, 0, share.WrappedLen)
	sealed = append(append(sealed, pqCiphertext...), classicalCiphertext...)
	return aead.Seal(sealed, scheduled.nonce, plaintext, nil)
}

type scheduledKey struct {
	key, nonce []byte
}

// keySchedule is the base mode KeySchedule of RFC 9180 section 5.1 with an empty PSK.
func keySchedule(sharedSecret, info []byte) scheduledKey {
	pskIDHash := labeledExtract(nil, "psk_id_hash", nil)
	infoHash := labeledExtract(nil, "info_hash", info)
	context := append(append([]byte{0}, pskIDHash...), infoHash...)
	secret := labeledExtract(sharedSecret, "secret", nil)
	return scheduledKey{
		key:   labeledExpand(secret, "key", context, 32),
		nonce: labeledExpand(secret, "base_nonce", context, 12),
	}
}

func labeledExtract(salt []byte, label string, ikm []byte) []byte {
	labeled := append(append(append([]byte("HPKE-v1"), hpkeSuite...), label...), ikm...)
	return must(hkdf.Extract(sha256.New, labeled, salt))
}

func labeledExpand(prk []byte, label string, info []byte, length uint16) []byte {
	labeled := binary.BigEndian.AppendUint16(nil, length)
	labeled = append(append(append(append(labeled, "HPKE-v1"...), hpkeSuite...), label...), info...)
	return must(hkdf.Expand(sha256.New, prk, string(labeled), int(length)))
}

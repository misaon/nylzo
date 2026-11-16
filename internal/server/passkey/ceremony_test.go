package passkey

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/mldsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"slices"
	"testing"
)

var testRelyingParty = RelyingParty{ID: "nylzo.test", Origin: "https://nylzo.test"}

type ceremony struct {
	relyingParty        RelyingParty
	authenticatorRPID   string
	clientData          map[string]any
	expectedChallenge   []byte
	flags               authenticatorFlags
	signCount           uint32
	credentialID        []byte
	attestedID          []byte
	coseKey             []byte
	attestedData        bool
	authenticatorSuffix []byte
	key                 crypto.Signer
	algorithm           Algorithm
}

func newCeremony(t *testing.T, algorithm Algorithm, ceremonyType string) *ceremony {
	t.Helper()
	challenge := rand.Text()
	credentialID := []byte(rand.Text())
	flags := flagUserPresent | flagUserVerified | flagBackupEligible | flagBackedUp
	if ceremonyType == ceremonyCreate {
		flags |= flagAttestedCredentialData
	}
	return &ceremony{
		relyingParty:      testRelyingParty,
		authenticatorRPID: testRelyingParty.ID,
		clientData: map[string]any{
			"type":        ceremonyType,
			"challenge":   base64.RawURLEncoding.EncodeToString([]byte(challenge)),
			"origin":      testRelyingParty.Origin,
			"crossOrigin": false,
		},
		expectedChallenge: []byte(challenge),
		flags:             flags,
		credentialID:      credentialID,
		attestedID:        credentialID,
		coseKey:           []byte{0xa1, 0x01, 0x01},
		attestedData:      ceremonyType == ceremonyCreate,
		key:               generateKey(t, algorithm),
		algorithm:         algorithm,
	}
}

func (c *ceremony) setChallenge(challenge []byte) {
	c.clientData["challenge"] = base64.RawURLEncoding.EncodeToString(challenge)
	c.expectedChallenge = challenge
}

func generateKey(t *testing.T, algorithm Algorithm) crypto.Signer {
	t.Helper()
	var key crypto.Signer
	var err error
	switch algorithm {
	case ES256:
		key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	case EdDSA:
		_, key, err = ed25519.GenerateKey(rand.Reader)
	case MLDSA44, MLDSA65, MLDSA87:
		parameters, _ := mldsaParameters(algorithm)
		key, err = mldsa.GenerateKey(parameters)
	default:
		t.Fatalf("no key for algorithm %d", algorithm)
	}
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func (c *ceremony) subjectPublicKeyInfo(t *testing.T) []byte {
	t.Helper()
	encoded, err := x509.MarshalPKIXPublicKey(c.key.Public())
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func (c *ceremony) clientDataJSON(t *testing.T) []byte {
	t.Helper()
	encoded, err := json.Marshal(c.clientData)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func (c *ceremony) authenticatorData() []byte {
	rpIDHash := sha256.Sum256([]byte(c.authenticatorRPID))
	data := append(rpIDHash[:], byte(c.flags))
	data = binary.BigEndian.AppendUint32(data, c.signCount)
	if c.attestedData {
		data = append(data, make([]byte, aaguidSize)...)
		data = binary.BigEndian.AppendUint16(data, uint16(len(c.attestedID)))
		data = append(data, c.attestedID...)
		data = append(data, c.coseKey...)
	}
	return append(data, c.authenticatorSuffix...)
}

func (c *ceremony) registration(t *testing.T) *Registration {
	t.Helper()
	return &Registration{
		ClientDataJSON:    c.clientDataJSON(t),
		AuthenticatorData: c.authenticatorData(),
		PublicKey:         c.subjectPublicKeyInfo(t),
		Algorithm:         c.algorithm,
		CredentialID:      c.credentialID,
	}
}

func (c *ceremony) assertion(t *testing.T) *Assertion {
	t.Helper()
	clientDataJSON := c.clientDataJSON(t)
	authenticatorData := c.authenticatorData()
	clientDataHash := sha256.Sum256(clientDataJSON)
	message := slices.Concat(authenticatorData, clientDataHash[:])
	var signature []byte
	var err error
	if c.algorithm == ES256 {
		digest := sha256.Sum256(message)
		signature, err = c.key.Sign(rand.Reader, digest[:], crypto.SHA256)
	} else {
		signature, err = c.key.Sign(rand.Reader, message, crypto.Hash(0))
	}
	if err != nil {
		t.Fatal(err)
	}
	return &Assertion{
		CredentialID:      c.credentialID,
		ClientDataJSON:    clientDataJSON,
		AuthenticatorData: authenticatorData,
		Signature:         signature,
	}
}

func (c *ceremony) credential(t *testing.T) *Credential {
	t.Helper()
	return &Credential{
		ID:             c.credentialID,
		PublicKey:      c.subjectPublicKeyInfo(t),
		Algorithm:      c.algorithm,
		BackupEligible: c.flags&flagBackupEligible != 0,
		BackedUp:       c.flags&flagBackedUp != 0,
	}
}

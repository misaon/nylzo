package passkey

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

type hexBytes []byte

func (h *hexBytes) UnmarshalText(text []byte) error {
	decoded, err := hex.DecodeString(string(text))
	*h = decoded
	return err
}

type vectorCeremony struct {
	Challenge         hexBytes `json:"challenge"`
	ClientDataJSON    hexBytes `json:"clientDataJSON"`
	AuthenticatorData hexBytes `json:"authenticatorData"`
	Signature         hexBytes `json:"signature"`
}

// WebAuthn Level 3 §16.2, §16.4 and §16.5; registration authenticatorData is the attestationObject's authData.
type vectorFile struct {
	RelyingPartyID string `json:"relyingPartyID"`
	Origin         string `json:"origin"`
	NoneES256      struct {
		CredentialPrivateKey hexBytes       `json:"credentialPrivateKey"`
		CredentialID         hexBytes       `json:"credentialID"`
		Registration         vectorCeremony `json:"registration"`
		Authentication       vectorCeremony `json:"authentication"`
	} `json:"noneES256"`
	CrossOriginAuthentication vectorCeremony `json:"crossOriginAuthentication"`
	TopOriginAuthentication   vectorCeremony `json:"topOriginAuthentication"`
}

func loadVectors(t *testing.T) (vectorFile, RelyingParty, []byte) {
	t.Helper()
	content, err := os.ReadFile("testdata/webauthn3-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors vectorFile
	if err = json.Unmarshal(content, &vectors); err != nil {
		t.Fatal(err)
	}
	privateKey, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), vectors.NoneES256.CredentialPrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	subjectPublicKeyInfo, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return vectors, RelyingParty{ID: vectors.RelyingPartyID, Origin: vectors.Origin}, subjectPublicKeyInfo
}

func TestRegistrationMatchesW3CVectorAndStillNeedsUserVerification(t *testing.T) {
	vectors, relyingParty, subjectPublicKeyInfo := loadVectors(t)
	registration := vectors.NoneES256.Registration

	err := relyingParty.verifyClientData(registration.ClientDataJSON, ceremonyCreate, registration.Challenge)
	if err != nil {
		t.Fatalf("client data: %v", err)
	}
	authenticatorData, err := relyingParty.parseAuthenticatorData(registration.AuthenticatorData)
	if err != nil {
		t.Fatalf("authenticator data: %v", err)
	}
	credentialID, err := authenticatorData.attestedCredentialID()
	if err != nil || !bytes.Equal(credentialID, vectors.NoneES256.CredentialID) {
		t.Fatalf("attested credential id %x, %v", credentialID, err)
	}
	if _, err = parsePublicKey(subjectPublicKeyInfo, ES256); err != nil {
		t.Fatalf("public key: %v", err)
	}
	challenge, err := ChallengeOf(registration.ClientDataJSON)
	if err != nil || !bytes.Equal(challenge, registration.Challenge) {
		t.Fatalf("ChallengeOf = %x, %v", challenge, err)
	}

	_, err = relyingParty.VerifyRegistration(&Registration{
		ClientDataJSON:    registration.ClientDataJSON,
		AuthenticatorData: registration.AuthenticatorData,
		PublicKey:         subjectPublicKeyInfo,
		Algorithm:         ES256,
		CredentialID:      vectors.NoneES256.CredentialID,
	}, registration.Challenge)
	if !errors.Is(err, ErrRefused) || authenticatorData.has(flagUserVerified) {
		t.Fatalf("a registration without UV was accepted: %v", err)
	}
}

func TestAssertionMatchesW3CVectorAndStillNeedsUserVerification(t *testing.T) {
	vectors, relyingParty, subjectPublicKeyInfo := loadVectors(t)
	authentication := vectors.NoneES256.Authentication

	err := relyingParty.verifyClientData(authentication.ClientDataJSON, ceremonyGet, authentication.Challenge)
	if err != nil {
		t.Fatalf("client data: %v", err)
	}
	if _, err = relyingParty.parseAuthenticatorData(authentication.AuthenticatorData); err != nil {
		t.Fatalf("authenticator data: %v", err)
	}
	publicKey, err := parsePublicKey(subjectPublicKeyInfo, ES256)
	if err != nil {
		t.Fatal(err)
	}
	err = verifySignature(
		publicKey, authentication.AuthenticatorData, authentication.ClientDataJSON, authentication.Signature,
	)
	if err != nil {
		t.Fatalf("signature: %v", err)
	}
	tampered := bytes.Clone(authentication.ClientDataJSON)
	tampered[len(tampered)-2] ^= 1
	err = verifySignature(publicKey, authentication.AuthenticatorData, tampered, authentication.Signature)
	if !errors.Is(err, ErrRefused) {
		t.Fatal("a signature over other client data was accepted")
	}

	credential := &Credential{
		ID:             vectors.NoneES256.CredentialID,
		PublicKey:      subjectPublicKeyInfo,
		Algorithm:      ES256,
		BackupEligible: true,
		BackedUp:       true,
	}
	_, err = relyingParty.VerifyAssertion(&Assertion{
		CredentialID:      vectors.NoneES256.CredentialID,
		ClientDataJSON:    authentication.ClientDataJSON,
		AuthenticatorData: authentication.AuthenticatorData,
		Signature:         authentication.Signature,
	}, authentication.Challenge, credential)
	if !errors.Is(err, ErrRefused) {
		t.Fatal("an assertion without UV was accepted")
	}
}

func TestClientDataRefusesW3CFramedVectors(t *testing.T) {
	vectors, relyingParty, _ := loadVectors(t)
	for name, ceremony := range map[string]vectorCeremony{
		"crossOrigin": vectors.CrossOriginAuthentication,
		"topOrigin":   vectors.TopOriginAuthentication,
	} {
		t.Run(name, func(t *testing.T) {
			err := relyingParty.verifyClientData(ceremony.ClientDataJSON, ceremonyGet, ceremony.Challenge)
			if !errors.Is(err, ErrRefused) {
				t.Fatalf("framed client data was accepted: %v", err)
			}
		})
	}
}

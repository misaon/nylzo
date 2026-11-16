package passkey

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"slices"
)

type RelyingParty struct {
	ID     string
	Origin string
}

type Algorithm int

const (
	ES256   Algorithm = -7
	EdDSA   Algorithm = -8
	MLDSA44 Algorithm = -48
	MLDSA65 Algorithm = -49
	MLDSA87 Algorithm = -50
)

func Algorithms() []Algorithm {
	return []Algorithm{MLDSA65, MLDSA87, MLDSA44, EdDSA, ES256}
}

type Credential struct {
	ID             []byte
	PublicKey      []byte
	Algorithm      Algorithm
	SignCount      uint32
	BackupEligible bool
	BackedUp       bool
}

type Registration struct {
	ClientDataJSON    []byte
	AuthenticatorData []byte
	PublicKey         []byte
	Algorithm         Algorithm
	CredentialID      []byte
}

type Assertion struct {
	CredentialID      []byte
	ClientDataJSON    []byte
	AuthenticatorData []byte
	Signature         []byte
}

var ErrRefused = errors.New("passkey: the ceremony was refused")

const (
	minimumCredentialIDSize = 1
	maximumCredentialIDSize = 1023
)

func (rp RelyingParty) VerifyRegistration(registration *Registration, challenge []byte) (Credential, error) {
	if registration == nil {
		return Credential{}, refused("no registration")
	}
	if err := checkCredentialID(registration.CredentialID); err != nil {
		return Credential{}, err
	}
	if err := rp.verifyClientData(registration.ClientDataJSON, ceremonyCreate, challenge); err != nil {
		return Credential{}, err
	}
	authenticatorData, err := rp.userVerifiedAuthenticatorData(registration.AuthenticatorData)
	if err != nil {
		return Credential{}, err
	}
	attestedCredentialID, err := authenticatorData.attestedCredentialID()
	if err != nil {
		return Credential{}, err
	}
	if subtle.ConstantTimeCompare(attestedCredentialID, registration.CredentialID) != 1 {
		return Credential{}, refused("the credential id differs from the authenticator data")
	}
	if _, err = parsePublicKey(registration.PublicKey, registration.Algorithm); err != nil {
		return Credential{}, err
	}
	return Credential{
		ID:             slices.Clone(registration.CredentialID),
		PublicKey:      slices.Clone(registration.PublicKey),
		Algorithm:      registration.Algorithm,
		SignCount:      authenticatorData.signCount,
		BackupEligible: authenticatorData.has(flagBackupEligible),
		BackedUp:       authenticatorData.has(flagBackedUp),
	}, nil
}

func (rp RelyingParty) VerifyAssertion(
	assertion *Assertion,
	challenge []byte,
	credential *Credential,
) (Credential, error) {
	if assertion == nil || credential == nil {
		return Credential{}, refused("no assertion or credential")
	}
	if err := checkCredentialID(assertion.CredentialID); err != nil {
		return Credential{}, err
	}
	if subtle.ConstantTimeCompare(assertion.CredentialID, credential.ID) != 1 {
		return Credential{}, refused("the assertion names another credential")
	}
	if err := rp.verifyClientData(assertion.ClientDataJSON, ceremonyGet, challenge); err != nil {
		return Credential{}, err
	}
	authenticatorData, err := rp.userVerifiedAuthenticatorData(assertion.AuthenticatorData)
	if err != nil {
		return Credential{}, err
	}
	if authenticatorData.has(flagBackupEligible) != credential.BackupEligible {
		return Credential{}, refused("the backup eligibility changed")
	}
	publicKey, err := parsePublicKey(credential.PublicKey, credential.Algorithm)
	if err != nil {
		return Credential{}, err
	}
	err = verifySignature(publicKey, assertion.AuthenticatorData, assertion.ClientDataJSON, assertion.Signature)
	if err != nil {
		return Credential{}, err
	}
	if !signCountIncreased(credential.SignCount, authenticatorData.signCount) {
		return Credential{}, refused("the signature counter did not increase")
	}
	updated := *credential
	updated.SignCount = authenticatorData.signCount
	updated.BackedUp = authenticatorData.has(flagBackedUp)
	return updated, nil
}

func checkCredentialID(credentialID []byte) error {
	if len(credentialID) < minimumCredentialIDSize || len(credentialID) > maximumCredentialIDSize {
		return refused("the credential id has an invalid length")
	}
	return nil
}

func signCountIncreased(stored, received uint32) bool {
	return (stored == 0 && received == 0) || received > stored
}

func refused(reason string) error {
	return fmt.Errorf("%w: %s", ErrRefused, reason)
}

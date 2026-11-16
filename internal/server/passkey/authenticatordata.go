package passkey

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
)

type authenticatorFlags byte

const (
	flagUserPresent            authenticatorFlags = 0x01
	flagUserVerified           authenticatorFlags = 0x04
	flagBackupEligible         authenticatorFlags = 0x08
	flagBackedUp               authenticatorFlags = 0x10
	flagAttestedCredentialData authenticatorFlags = 0x40
)

const (
	rpIDHashSize                 = sha256.Size
	flagsOffset                  = rpIDHashSize
	signCountOffset              = flagsOffset + 1
	minimumAuthenticatorDataSize = signCountOffset + 4
	maximumAuthenticatorDataSize = 4096
	aaguidSize                   = 16
	credentialIDLengthSize       = 2
)

type parsedAuthenticatorData struct {
	flags     authenticatorFlags
	signCount uint32
	trailer   []byte
}

func (rp RelyingParty) parseAuthenticatorData(data []byte) (parsedAuthenticatorData, error) {
	if len(data) < minimumAuthenticatorDataSize || len(data) > maximumAuthenticatorDataSize {
		return parsedAuthenticatorData{}, refused("the authenticator data has an invalid length")
	}
	expectedRPIDHash := sha256.Sum256([]byte(rp.ID))
	if subtle.ConstantTimeCompare(data[:rpIDHashSize], expectedRPIDHash[:]) != 1 {
		return parsedAuthenticatorData{}, refused("the authenticator data names another relying party")
	}
	parsed := parsedAuthenticatorData{
		flags:     authenticatorFlags(data[flagsOffset]),
		signCount: binary.BigEndian.Uint32(data[signCountOffset:minimumAuthenticatorDataSize]),
		trailer:   data[minimumAuthenticatorDataSize:],
	}
	if parsed.has(flagBackedUp) && !parsed.has(flagBackupEligible) {
		return parsedAuthenticatorData{}, refused("the credential is backed up but not backup eligible")
	}
	return parsed, nil
}

func (rp RelyingParty) userVerifiedAuthenticatorData(data []byte) (parsedAuthenticatorData, error) {
	parsed, err := rp.parseAuthenticatorData(data)
	if err != nil {
		return parsedAuthenticatorData{}, err
	}
	if !parsed.has(flagUserPresent) {
		return parsedAuthenticatorData{}, refused("the user was not present")
	}
	if !parsed.has(flagUserVerified) {
		return parsedAuthenticatorData{}, refused("the user was not verified")
	}
	return parsed, nil
}

func (data parsedAuthenticatorData) has(flag authenticatorFlags) bool {
	return data.flags&flag != 0
}

func (data parsedAuthenticatorData) attestedCredentialID() ([]byte, error) {
	if !data.has(flagAttestedCredentialData) {
		return nil, refused("the authenticator data has no attested credential")
	}
	const idOffset = aaguidSize + credentialIDLengthSize
	if len(data.trailer) < idOffset {
		return nil, refused("the attested credential data is truncated")
	}
	idLength := int(binary.BigEndian.Uint16(data.trailer[aaguidSize:idOffset]))
	if idLength < minimumCredentialIDSize || idLength > maximumCredentialIDSize {
		return nil, refused("the attested credential id has an invalid length")
	}
	if len(data.trailer) <= idOffset+idLength {
		return nil, refused("the attested credential data has no public key")
	}
	return data.trailer[idOffset : idOffset+idLength], nil
}

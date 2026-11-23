package passkey

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/x509"
	"errors"
	"strconv"
	"strings"
	"testing"
)

func TestEveryAlgorithmRegistersAndAsserts(t *testing.T) {
	for _, algorithm := range Algorithms() {
		t.Run(strconv.Itoa(int(algorithm)), func(t *testing.T) {
			registering := newCeremony(t, algorithm, ceremonyCreate)
			credential, err := testRelyingParty.VerifyRegistration(
				registering.registration(t), registering.expectedChallenge,
			)
			if err != nil {
				t.Fatalf("registration: %v", err)
			}
			if !bytes.Equal(credential.ID, registering.credentialID) || !credential.BackupEligible {
				t.Fatalf("registered credential %+v", credential)
			}

			for signCount := range uint32(2) {
				asserting := newCeremony(t, algorithm, ceremonyGet)
				asserting.key = registering.key
				asserting.credentialID = registering.credentialID
				asserting.signCount = signCount + 1
				credential, err = testRelyingParty.VerifyAssertion(
					asserting.assertion(t), asserting.expectedChallenge, &credential,
				)
				if err != nil {
					t.Fatalf("assertion %d: %v", signCount, err)
				}
				if credential.SignCount != signCount+1 {
					t.Fatalf("sign count %d, want %d", credential.SignCount, signCount+1)
				}
			}
		})
	}
}

func TestRegistrationRefusesEveryBrokenRule(t *testing.T) {
	cases := map[string]struct {
		prepare func(attempt *ceremony)
		tamper  func(registration *Registration)
	}{
		"wrong type": {prepare: func(attempt *ceremony) { attempt.clientData["type"] = ceremonyGet }},
		"wrong origin": {
			prepare: func(attempt *ceremony) { attempt.clientData["origin"] = "https://nylzo.test:8443" },
		},
		"wrong challenge":       {prepare: func(attempt *ceremony) { attempt.expectedChallenge[0] ^= 1 }},
		"wrong relying party":   {prepare: func(attempt *ceremony) { attempt.authenticatorRPID = "nylzo.example" }},
		"no user verification":  {prepare: func(attempt *ceremony) { attempt.flags &^= flagUserVerified }},
		"no user presence":      {prepare: func(attempt *ceremony) { attempt.flags &^= flagUserPresent }},
		"no attested data":      {prepare: func(attempt *ceremony) { attempt.flags &^= flagAttestedCredentialData }},
		"backed up, ineligible": {prepare: func(attempt *ceremony) { attempt.flags &^= flagBackupEligible }},
		"credential id differs": {
			prepare: func(attempt *ceremony) { attempt.attestedID = []byte("another credential id") },
		},
		"empty credential id": {
			prepare: func(attempt *ceremony) { attempt.credentialID, attempt.attestedID = nil, nil },
		},
		"no public key bytes":     {prepare: func(attempt *ceremony) { attempt.coseKey = nil }},
		"attested data cut short": {prepare: func(attempt *ceremony) { attempt.attestedData = false }},
		"oversized attested id": {
			prepare: func(attempt *ceremony) { attempt.attestedID = make([]byte, maximumCredentialIDSize+1) },
		},
		"relying party not configured": {prepare: func(attempt *ceremony) { attempt.relyingParty = RelyingParty{} }},
		"public key not a subject public key info": {
			tamper: func(registration *Registration) { registration.PublicKey = []byte("not a key") },
		},
		"key exchange public key": {
			tamper: func(registration *Registration) { registration.PublicKey = keyExchangePublicKeyInfo(t) },
		},
		"key of another algorithm": {
			tamper: func(registration *Registration) { registration.Algorithm = MLDSA65 },
		},
		"unsupported algorithm": {tamper: func(registration *Registration) { registration.Algorithm = -257 }},
		"oversized public key": {
			tamper: func(registration *Registration) {
				registration.PublicKey = append(registration.PublicKey, make([]byte, maximumPublicKeySize)...)
			},
		},
		"oversized client data": {
			prepare: func(attempt *ceremony) { attempt.clientData["padding"] = strings.Repeat("a", maximumClientDataSize) },
		},
	}
	for name, broken := range cases {
		t.Run(name, func(t *testing.T) {
			attempt := newCeremony(t, ES256, ceremonyCreate)
			if broken.prepare != nil {
				broken.prepare(attempt)
			}
			registration := attempt.registration(t)
			if broken.tamper != nil {
				broken.tamper(registration)
			}
			_, err := attempt.relyingParty.VerifyRegistration(registration, attempt.expectedChallenge)
			if !errors.Is(err, ErrRefused) {
				t.Fatalf("broken registration was accepted: %v", err)
			}
		})
	}
}

func keyExchangePublicKeyInfo(t *testing.T) []byte {
	t.Helper()
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := x509.MarshalPKIXPublicKey(key.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestCeremoniesWithoutTheirDataAreRefused(t *testing.T) {
	credential := newCeremony(t, ES256, ceremonyGet).credential(t)
	if _, err := testRelyingParty.VerifyRegistration(nil, []byte(rand.Text())); !errors.Is(err, ErrRefused) {
		t.Errorf("no registration: %v", err)
	}
	if _, err := testRelyingParty.VerifyAssertion(nil, []byte(rand.Text()), credential); !errors.Is(err, ErrRefused) {
		t.Errorf("no assertion: %v", err)
	}
}

func TestKeysAndSignaturesOfAnotherKindAreRefused(t *testing.T) {
	for _, algorithm := range []Algorithm{ES256, EdDSA, -257} {
		if _, ok := mldsaParameters(algorithm); ok {
			t.Errorf("algorithm %d has ML-DSA parameters", algorithm)
		}
	}
	mldsaKey := generateKey(t, MLDSA44).Public()
	if keyMatchesAlgorithm(mldsaKey, ES256) {
		t.Error("an ML-DSA key matched ES256")
	}
	keyExchange, err := x509.ParsePKIXPublicKey(keyExchangePublicKeyInfo(t))
	if err != nil {
		t.Fatal(err)
	}
	if err = verifySignature(
		keyExchange,
		[]byte("data"),
		[]byte("{}"),
		[]byte("signature"),
	); !errors.Is(
		err,
		ErrRefused,
	) {
		t.Errorf("a key that cannot sign verified a signature: %v", err)
	}
}

func TestChallengeOfRefusesBrokenClientData(t *testing.T) {
	for _, clientDataJSON := range []string{"not json", `{"challenge":"not base64url!"}`} {
		if challenge, err := ChallengeOf([]byte(clientDataJSON)); !errors.Is(err, ErrRefused) || challenge != nil {
			t.Errorf("%s gave %x, %v", clientDataJSON, challenge, err)
		}
	}
}

func TestAssertionRefusesEveryBrokenRule(t *testing.T) {
	cases := map[string]struct {
		prepare func(attempt *ceremony, credential *Credential)
		tamper  func(assertion *Assertion)
	}{
		"wrong type": {prepare: func(attempt *ceremony, _ *Credential) { attempt.clientData["type"] = ceremonyCreate }},
		"wrong origin": {
			prepare: func(attempt *ceremony, _ *Credential) { attempt.clientData["origin"] = "http://nylzo.test" },
		},
		"cross-origin frame": {
			prepare: func(attempt *ceremony, _ *Credential) { attempt.clientData["crossOrigin"] = true },
		},
		"top origin": {
			prepare: func(attempt *ceremony, _ *Credential) { attempt.clientData["topOrigin"] = "https://a.test" },
		},
		"short challenge": {
			prepare: func(attempt *ceremony, _ *Credential) {
				attempt.setChallenge(attempt.expectedChallenge[:minimumChallengeSize-1])
			},
		},
		"wrong challenge": {prepare: func(attempt *ceremony, _ *Credential) { attempt.expectedChallenge[0] ^= 1 }},
		"wrong relying party": {
			prepare: func(attempt *ceremony, _ *Credential) { attempt.authenticatorRPID = "evil.test" },
		},
		"no user verification": {
			prepare: func(attempt *ceremony, _ *Credential) { attempt.flags &^= flagUserVerified },
		},
		"no user presence": {prepare: func(attempt *ceremony, _ *Credential) { attempt.flags &^= flagUserPresent }},
		"backup eligibility changed": {
			prepare: func(attempt *ceremony, _ *Credential) { attempt.flags &^= flagBackupEligible | flagBackedUp },
		},
		"sign count not increasing": {
			prepare: func(attempt *ceremony, credential *Credential) { attempt.signCount, credential.SignCount = 7, 7 },
		},
		"sign count went backwards": {
			prepare: func(_ *ceremony, credential *Credential) { credential.SignCount = 7 },
		},
		"credential id differs": {
			prepare: func(_ *ceremony, credential *Credential) { credential.ID = []byte("another credential id") },
		},
		"oversized credential id": {
			prepare: func(attempt *ceremony, credential *Credential) {
				attempt.credentialID = make([]byte, maximumCredentialIDSize+1)
				credential.ID = attempt.credentialID
			},
		},
		"key of another algorithm": {
			prepare: func(_ *ceremony, credential *Credential) { credential.Algorithm = EdDSA },
		},
		"tampered signature": {tamper: func(assertion *Assertion) { assertion.Signature[8] ^= 1 }},
		"tampered authenticator data": {
			tamper: func(assertion *Assertion) { assertion.AuthenticatorData[signCountOffset] ^= 1 },
		},
		"oversized client data": {
			prepare: func(attempt *ceremony, _ *Credential) {
				attempt.clientData["padding"] = strings.Repeat("a", maximumClientDataSize)
			},
		},
		"oversized authenticator data": {
			prepare: func(attempt *ceremony, _ *Credential) {
				attempt.authenticatorSuffix = make([]byte, maximumAuthenticatorDataSize)
			},
		},
		"oversized signature": {
			tamper: func(assertion *Assertion) {
				assertion.Signature = append(assertion.Signature, make([]byte, maximumSignatureSize)...)
			},
		},
	}
	for name, broken := range cases {
		t.Run(name, func(t *testing.T) {
			attempt := newCeremony(t, ES256, ceremonyGet)
			attempt.signCount = 1
			credential := attempt.credential(t)
			if broken.prepare != nil {
				broken.prepare(attempt, credential)
			}
			assertion := attempt.assertion(t)
			if broken.tamper != nil {
				broken.tamper(assertion)
			}
			_, err := attempt.relyingParty.VerifyAssertion(assertion, attempt.expectedChallenge, credential)
			if !errors.Is(err, ErrRefused) {
				t.Fatalf("broken assertion was accepted: %v", err)
			}
		})
	}
}

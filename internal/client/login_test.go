package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/core/opaque"
	"github.com/misaon/nylzo/internal/core/signing"
)

var (
	loginConfig          = opaque.Config{Context: []byte("nylzo client test"), Stretch: opaque.Identity}
	credentialIdentifier = []byte("message")
)

type opaqueServer struct {
	keys   opaque.ServerKeys
	record [opaque.RecordLen]byte
}

func registered(t *testing.T, password []byte) *opaqueServer {
	t.Helper()
	keys, err := opaque.DeriveServerKeys(bytes.Repeat([]byte{1}, opaque.SeedLen), new([opaque.OPRFSeedLen]byte))
	if err != nil {
		t.Fatal(err)
	}
	request, state, err := opaque.CreateRegistrationRequest(password)
	if err != nil {
		t.Fatal(err)
	}
	response, err := opaque.CreateRegistrationResponse(request[:], &keys, credentialIdentifier)
	if err != nil {
		t.Fatal(err)
	}
	registration, err := opaque.FinalizeRegistrationRequest(&state, response[:], &loginConfig)
	if err != nil {
		t.Fatal(err)
	}
	return &opaqueServer{keys: keys, record: registration.Record}
}

func (s *opaqueServer) answer(t *testing.T, change func(ke2 []byte)) http.HandlerFunc {
	t.Helper()
	return answerChallengeThen(validChallenge, func(w http.ResponseWriter, r *http.Request) {
		ke1, err := core.DecodeExact(r.Header.Get(api.KE1HeaderName), opaque.KE1Len)
		if err != nil {
			t.Error(err)
			return
		}
		ke2, _, err := opaque.GenerateKE2(&s.keys, s.record[:], credentialIdentifier, ke1, &loginConfig)
		if err != nil {
			t.Error(err)
			return
		}
		change(ke2[:])
		json.NewEncoder(w).Encode(api.Handshake{
			KE2:          core.Encode(ke2[:]),
			Challenge:    core.Encode(make([]byte, signing.ChallengeLen)),
			AttemptsLeft: 2,
		})
	})
}

func TestLogInTellsAServerWithOtherKeysFromAWrongPassphrase(t *testing.T) {
	t.Parallel()
	password := []byte("the password of the message")
	server := registered(t, password)
	unchanged := func([]byte) {}
	for name, test := range map[string]struct {
		answer http.HandlerFunc
		kdf    core.KDF
		want   func(error) bool
	}{
		"the right server": {server.answer(t, unchanged), core.KDFScrypt, func(err error) bool { return err == nil }},
		"another record without a passphrase": {
			registered(t, []byte("another password")).answer(t, unchanged),
			core.KDFNone,
			func(err error) bool { return errors.Is(err, ErrServerAuthentication) },
		},
		"another record with a passphrase": {
			registered(t, []byte("another password")).answer(t, unchanged),
			core.KDFScrypt,
			func(err error) bool { return IsKind(err, api.ErrorPassphrase) },
		},
		"a forged server MAC": {
			server.answer(t, func(ke2 []byte) { ke2[len(ke2)-1] ^= 1 }),
			core.KDFScrypt,
			func(err error) bool { return errors.Is(err, ErrServerAuthentication) },
		},
		"a key share that is no point": {
			server.answer(t, func(ke2 []byte) {
				copy(ke2[len(ke2)-opaque.MACLen-opaque.ElementLen:], bytes.Repeat([]byte{0xff}, opaque.ElementLen))
			}),
			core.KDFScrypt,
			func(err error) bool { return errors.Is(err, ErrResponse) },
		},
		"a handshake without a KE2": {
			answerChallengeThen(validChallenge, func(w http.ResponseWriter, _ *http.Request) {
				w.Write([]byte(`{"ke2":"AAAA","challenge":"AAAA"}`))
			}),
			core.KDFScrypt,
			func(err error) bool { return errors.Is(err, ErrResponse) },
		},
		"a failed handshake": {
			answerChallengeThen(validChallenge, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			}),
			core.KDFScrypt,
			func(err error) bool { return IsKind(err, "unexpected") },
		},
	} {
		client := serve(t, test.answer)
		login, err := client.LogIn(t.Context(), core.NewID(), core.NewSecret(), test.kdf, password, &loginConfig)
		if !test.want(err) {
			t.Errorf("%s: %v", name, err)
		}
		if err == nil && login.ExportKey == ([opaque.ExportKeyLen]byte{}) {
			t.Errorf("%s: logged in without an export key", name)
		}
	}
}

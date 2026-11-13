package opaque

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/gtank/ristretto255"
)

type oprfVector struct {
	Name              string `json:"name"`
	Input             string `json:"input"`
	Blind             string `json:"blind"`
	BlindedElement    string `json:"blinded_element"`
	EvaluationElement string `json:"evaluation_element"`
	Output            string `json:"output"`
}

type opaqueVector struct {
	Name                 string `json:"name"`
	Context              string `json:"context"`
	OPRFSeed             string `json:"oprf_seed"`
	CredentialIdentifier string `json:"credential_identifier"`
	Password             string `json:"password"`
	EnvelopeNonce        string `json:"envelope_nonce"`
	MaskingNonce         string `json:"masking_nonce"`
	ServerPrivateKey     string `json:"server_private_key"`
	ServerPublicKey      string `json:"server_public_key"`
	ServerNonce          string `json:"server_nonce"`
	ClientNonce          string `json:"client_nonce"`
	ClientKeyshareSeed   string `json:"client_keyshare_seed"`
	ServerKeyshareSeed   string `json:"server_keyshare_seed"`
	BlindRegistration    string `json:"blind_registration"`
	BlindLogin           string `json:"blind_login"`
	OPRFKey              string `json:"oprf_key"`
	RegistrationRequest  string `json:"registration_request"`
	RegistrationResponse string `json:"registration_response"`
	RegistrationUpload   string `json:"registration_upload"`
	KE1                  string `json:"ke1"`
	KE2                  string `json:"ke2"`
	KE3                  string `json:"ke3"`
	ExportKey            string `json:"export_key"`
	SessionKey           string `json:"session_key"`
}

type rfcVectors struct {
	OPRF struct {
		Seed    string       `json:"seed"`
		KeyInfo string       `json:"key_info"`
		SK      string       `json:"sk"`
		Vectors []oprfVector `json:"vectors"`
	} `json:"oprf_rfc9497"`
	OPAQUE []opaqueVector `json:"opaque_rfc9807"`
}

func loadRFCVectors(t *testing.T) *rfcVectors {
	t.Helper()
	data, err := os.ReadFile("../../../spec/opaque-rfc-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var file rfcVectors
	if err = json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	return &file
}

func unhex(t *testing.T, text string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(text)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func expectHex(t *testing.T, what string, got []byte, want string) {
	t.Helper()
	if hex.EncodeToString(got) != want {
		t.Errorf("%s = %x, want %s", what, got, want)
	}
}

func TestOPRFMatchesRFC9497(t *testing.T) {
	file := loadRFCVectors(t)
	key, _, err := deriveKeyPair(unhex(t, file.OPRF.Seed), string(unhex(t, file.OPRF.KeyInfo)))
	if err != nil {
		t.Fatal(err)
	}
	expectHex(t, "skS", key.Bytes(), file.OPRF.SK)
	for _, vector := range file.OPRF.Vectors {
		t.Run(vector.Name, func(t *testing.T) {
			factor, blindErr := blindFrom((*[ScalarLen]byte)(unhex(t, vector.Blind)))
			if blindErr != nil {
				t.Fatal(blindErr)
			}
			input := unhex(t, vector.Input)
			blinded, blindedErr := blind(input, factor)
			if blindedErr != nil {
				t.Fatal(blindedErr)
			}
			expectHex(t, "blinded", blinded, vector.BlindedElement)
			evaluated, evaluateErr := blindEvaluate(key, blinded)
			if evaluateErr != nil {
				t.Fatal(evaluateErr)
			}
			expectHex(t, "evaluated", evaluated, vector.EvaluationElement)
			output, finalizeErr := finalize(input, factor, evaluated)
			if finalizeErr != nil {
				t.Fatal(finalizeErr)
			}
			expectHex(t, "output", output, vector.Output)
		})
	}
}

type rfcRun struct {
	config      Config
	keys        ServerKeys
	credential  []byte
	record      [RecordLen]byte
	ke1         [KE1Len]byte
	ke2         [KE2Len]byte
	login       ClientLogin
	serverLogin ServerLogin
}

func vectorServerKeys(t *testing.T, vector *opaqueVector) ServerKeys {
	t.Helper()
	private, err := ristretto255.NewScalar().SetCanonicalBytes(unhex(t, vector.ServerPrivateKey))
	if err != nil {
		t.Fatal(err)
	}
	keys := ServerKeys{PrivateKey: private, OPRFSeed: [OPRFSeedLen]byte(unhex(t, vector.OPRFSeed))}
	copy(keys.PublicKey[:], ristretto255.NewIdentityElement().ScalarBaseMult(private).Bytes())
	expectHex(t, "server public key", keys.PublicKey[:], vector.ServerPublicKey)
	return keys
}

func runRegistration(t *testing.T, vector *opaqueVector, run *rfcRun) {
	t.Helper()
	password := unhex(t, vector.Password)
	request, state, err := CreateRegistrationRequestWith(
		password,
		(*[ScalarLen]byte)(unhex(t, vector.BlindRegistration)),
	)
	if err != nil {
		t.Fatal(err)
	}
	expectHex(t, "registration request", request[:], vector.RegistrationRequest)
	response, err := CreateRegistrationResponse(request[:], &run.keys, run.credential)
	if err != nil {
		t.Fatal(err)
	}
	expectHex(t, "registration response", response[:], vector.RegistrationResponse)
	nonce := [NonceLen]byte(unhex(t, vector.EnvelopeNonce))
	finished, err := FinalizeRegistrationRequestWith(&state, response[:], &run.config, &nonce)
	if err != nil {
		t.Fatal(err)
	}
	expectHex(t, "registration record", finished.Record[:], vector.RegistrationUpload)
	expectHex(t, "export key at registration", finished.ExportKey[:], vector.ExportKey)
	run.record = finished.Record
}

func runLogin(t *testing.T, vector *opaqueVector, run *rfcRun) {
	t.Helper()
	nonce := [NonceLen]byte(unhex(t, vector.ClientNonce))
	seed := [SeedLen]byte(unhex(t, vector.ClientKeyshareSeed))
	ke1, login, err := GenerateKE1With(
		unhex(t, vector.Password),
		(*[ScalarLen]byte)(unhex(t, vector.BlindLogin)),
		&nonce,
		&seed,
	)
	if err != nil {
		t.Fatal(err)
	}
	expectHex(t, "KE1", ke1[:], vector.KE1)
	random := ServerRandomness{
		MaskingNonce: [NonceLen]byte(unhex(t, vector.MaskingNonce)),
		ServerNonce:  [NonceLen]byte(unhex(t, vector.ServerNonce)),
		KeyshareSeed: [SeedLen]byte(unhex(t, vector.ServerKeyshareSeed)),
	}
	ke2, serverLogin, err := GenerateKE2With(&run.keys, run.record[:], run.credential, ke1[:], &run.config, &random)
	if err != nil {
		t.Fatal(err)
	}
	expectHex(t, "KE2", ke2[:], vector.KE2)
	run.ke1, run.ke2, run.login, run.serverLogin = ke1, ke2, login, serverLogin
}

func TestOPAQUEMatchesRFC9807(t *testing.T) {
	for _, vector := range loadRFCVectors(t).OPAQUE {
		t.Run(vector.Name, func(t *testing.T) {
			run := &rfcRun{
				config:     Config{Context: unhex(t, vector.Context), Stretch: Identity},
				credential: unhex(t, vector.CredentialIdentifier),
			}
			run.keys = vectorServerKeys(t, &vector)
			key, err := oprfKey(&run.keys.OPRFSeed, run.credential)
			if err != nil {
				t.Fatal(err)
			}
			expectHex(t, "OPRF key", key.Bytes(), vector.OPRFKey)
			runRegistration(t, &vector, run)
			runLogin(t, &vector, run)
			result, err := run.login.GenerateKE3(run.ke2[:], &run.config)
			if err != nil {
				t.Fatal(err)
			}
			expectHex(t, "KE3", result.KE3[:], vector.KE3)
			expectHex(t, "export key", result.ExportKey[:], vector.ExportKey)
			expectHex(t, "client session key", result.SessionKey[:], vector.SessionKey)
			expectHex(t, "server session key", run.serverLogin.SessionKey[:], vector.SessionKey)
			if err = run.serverLogin.ServerFinish(result.KE3[:]); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func rfcLogin(t *testing.T) (*opaqueVector, *rfcRun) {
	t.Helper()
	vector := loadRFCVectors(t).OPAQUE[0]
	run := &rfcRun{
		config:     Config{Context: unhex(t, vector.Context), Stretch: Identity},
		credential: unhex(t, vector.CredentialIdentifier),
	}
	run.keys = vectorServerKeys(t, &vector)
	runRegistration(t, &vector, run)
	runLogin(t, &vector, run)
	return &vector, run
}

func TestLoginFailures(t *testing.T) {
	t.Run("a wrong password does not open the envelope", func(t *testing.T) {
		vector, run := rfcLogin(t)
		ke1, login, err := GenerateKE1(append(unhex(t, vector.Password), 'x'))
		if err != nil {
			t.Fatal(err)
		}
		ke2, _, err := GenerateKE2(&run.keys, run.record[:], run.credential, ke1[:], &run.config)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = login.GenerateKE3(ke2[:], &run.config); !errors.Is(err, ErrEnvelopeRecovery) {
			t.Fatalf("got %v, want ErrEnvelopeRecovery", err)
		}
	})
	t.Run("a changed server mac fails server authentication", func(t *testing.T) {
		_, run := rfcLogin(t)
		run.ke2[KE2Len-1] ^= 1
		if _, err := run.login.GenerateKE3(run.ke2[:], &run.config); !errors.Is(err, ErrServerAuthentication) {
			t.Fatalf("got %v, want ErrServerAuthentication", err)
		}
	})
	t.Run("another context fails server authentication", func(t *testing.T) {
		_, run := rfcLogin(t)
		other := Config{Context: []byte("another"), Stretch: Identity}
		if _, err := run.login.GenerateKE3(run.ke2[:], &other); !errors.Is(err, ErrServerAuthentication) {
			t.Fatalf("got %v, want ErrServerAuthentication", err)
		}
	})
	t.Run("a changed KE3 fails client authentication", func(t *testing.T) {
		_, run := rfcLogin(t)
		result, err := run.login.GenerateKE3(run.ke2[:], &run.config)
		if err != nil {
			t.Fatal(err)
		}
		result.KE3[0] ^= 1
		if err = run.serverLogin.ServerFinish(result.KE3[:]); !errors.Is(err, ErrClientAuthentication) {
			t.Fatalf("got %v, want ErrClientAuthentication", err)
		}
		if err = run.serverLogin.ServerFinish(result.KE3[:KE3Len-1]); !errors.Is(err, ErrClientAuthentication) {
			t.Fatalf("short KE3: got %v", err)
		}
	})
	t.Run("an identity server keyshare is refused", func(t *testing.T) {
		_, run := rfcLogin(t)
		copy(run.ke2[credentialResponseLen+NonceLen:], make([]byte, ElementLen))
		if _, err := run.login.GenerateKE3(run.ke2[:], &run.config); !errors.Is(err, ErrDeserialize) {
			t.Fatalf("got %v, want ErrDeserialize", err)
		}
	})
	t.Run("an input Finalize cannot prefix with its length is refused, the longest one completes", func(t *testing.T) {
		_, run := rfcLogin(t)
		tooLong := make([]byte, MaxInputLen+1)
		longest := tooLong[:MaxInputLen]
		if _, _, err := CreateRegistrationRequest(tooLong); !errors.Is(err, ErrInputLength) {
			t.Fatalf("registration: got %v, want ErrInputLength", err)
		}
		if _, _, err := GenerateKE1(tooLong); !errors.Is(err, ErrInputLength) {
			t.Fatalf("KE1: got %v, want ErrInputLength", err)
		}
		request, state, err := CreateRegistrationRequest(longest)
		if err != nil {
			t.Fatal(err)
		}
		response, err := CreateRegistrationResponse(request[:], &run.keys, run.credential)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = FinalizeRegistrationRequest(&state, response[:], &run.config); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("a short KE2 is refused", func(t *testing.T) {
		_, run := rfcLogin(t)
		if _, err := run.login.GenerateKE3(run.ke2[:KE2Len-1], &run.config); err == nil {
			t.Fatal("a short KE2 was accepted")
		}
	})
}

func TestServerRefusesMalformedInput(t *testing.T) {
	vector, run := rfcLogin(t)
	identity := make([]byte, ElementLen)
	if _, err := CreateRegistrationResponse(identity, &run.keys, run.credential); !errors.Is(err, ErrDeserialize) {
		t.Fatalf("registration with the identity: %v", err)
	}
	ke1 := bytes.Clone(run.ke1[:])
	copy(ke1[ElementLen+NonceLen:], identity)
	if _, _, err := GenerateKE2(
		&run.keys,
		run.record[:],
		run.credential,
		ke1,
		&run.config,
	); !errors.Is(
		err,
		ErrDeserialize,
	) {
		t.Fatalf("KE1 with an identity keyshare: %v", err)
	}
	record := bytes.Clone(run.record[:])
	copy(record, identity)
	if _, _, err := GenerateKE2(
		&run.keys,
		record,
		run.credential,
		run.ke1[:],
		&run.config,
	); !errors.Is(
		err,
		ErrDeserialize,
	) {
		t.Fatalf("record with an identity public key: %v", err)
	}
	if _, _, err := CreateRegistrationRequestWith(
		unhex(t, vector.Password),
		new([ScalarLen]byte),
	); !errors.Is(
		err,
		ErrDeserialize,
	) {
		t.Fatalf("a zero blind: %v", err)
	}
}

package vectors

import (
	"bytes"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/gtank/ristretto255"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/keys"
	"github.com/misaon/nylzo/internal/core/opaque"
	"github.com/misaon/nylzo/internal/pepper"
)

const infoOPRFSeed = "nylzo/v3/opaque/oprf-seed"

var errOPAQUERun = errors.New("does not complete")

// OPAQUE is one registration and one handshake of spec section 2.1 with Nylzo's configuration, the server keys
// derived from Pepper as in spec section 5.1. Messages that travel in headers are base64url, everything else hex.
type OPAQUE struct {
	Name                 string `json:"name"`
	ID                   string `json:"id"`
	S                    string `json:"s"`
	KDFID                int    `json:"kdf_id"`
	Passphrase           string `json:"passphrase,omitempty"`
	PassphraseSalt       string `json:"salt_pw_hex"`
	Password             string `json:"password_hex"`
	Context              string `json:"context_hex"`
	E                    string `json:"e_hex"`
	CredentialIdentifier string `json:"credential_identifier_hex"`
	Pepper               string `json:"pepper_hex"`
	OPRFSeed             string `json:"oprf_seed_hex"`
	ServerKeySeed        string `json:"server_key_seed_hex"`
	ServerPublicKey      string `json:"server_public_key_hex"`
	BlindRegistration    string `json:"blind_registration_hex"`
	EnvelopeNonce        string `json:"envelope_nonce_hex"`
	RegistrationRequest  string `json:"registration_request"`
	RegistrationResponse string `json:"registration_response"`
	Registration         string `json:"registration"`
	BlindLogin           string `json:"blind_login_hex"`
	ClientNonce          string `json:"client_nonce_hex"`
	ClientKeyshareSeed   string `json:"client_keyshare_seed_hex"`
	MaskingNonce         string `json:"masking_nonce_hex"`
	ServerNonce          string `json:"server_nonce_hex"`
	ServerKeyshareSeed   string `json:"server_keyshare_seed_hex"`
	KE1                  string `json:"ke1"`
	KE2                  string `json:"ke2"`
	KE3                  string `json:"ke3"`
	P                    string `json:"p_hex"`
	Heavy                bool   `json:"heavy"`
}

type opaqueCase struct {
	name       string
	kdf        core.KDF
	passphrase string
}

var opaqueCases = []opaqueCase{
	{"no-passphrase", core.KDFNone, ""},
	{"scrypt-ascii", core.KDFScrypt, fourWordPassphrase},
	{"scrypt-nfc-decomposed", core.KDFScrypt, "héslo s češtinou"},
}

func (f *File) opaque() {
	for _, vector := range opaqueCases {
		f.OPAQUE = append(f.OPAQUE, opaqueVector(vector))
	}
}

func deterministicBlind(label string) *[opaque.ScalarLen]byte {
	return (*[opaque.ScalarLen]byte)(deterministicBlindBytes(label))
}

func deterministicBlindBytes(label string) []byte {
	return must(ristretto255.NewScalar().SetUniformBytes(deterministic(label, 64))).Bytes()
}

type opaqueInputs struct {
	id         core.ID
	secret     *[core.SecretLen]byte
	share      *[core.ShareLen]byte
	credential []byte
	pepper     []byte
	password   []byte
	config     opaque.Config
	serverKeys opaque.ServerKeys
	keySeed    pepper.Tag
}

func opaqueSetup(vector opaqueCase) *opaqueInputs {
	inputs := &opaqueInputs{
		id:     deterministicID("opaque/" + vector.name),
		secret: deterministic32("opaque/s/" + vector.name),
		share:  deterministic32("opaque/e/" + vector.name),
		pepper: deterministic("pepper/current", pepper.SecretLen),
	}
	inputs.credential = keys.CredentialIdentifier(inputs.id, inputs.share)
	ring := must(pepper.NewRing(inputs.pepper, nil))
	inputs.password = must(keys.Password(inputs.secret, vector.kdf, vector.passphrase))
	inputs.config = must(keys.OPAQUEConfig(inputs.id, vector.kdf))
	inputs.serverKeys = must(ring.OPAQUEKeys(inputs.id[:], ring.CurrentID()))
	inputs.keySeed = ring.MAC(pepper.OPAQUEServer, inputs.id[:])
	oprfSeed := must(hkdf.Key(sha256.New, inputs.pepper, nil, infoOPRFSeed, opaque.OPRFSeedLen))
	require(
		bytes.Equal(oprfSeed, inputs.serverKeys.OPRFSeed[:]),
		fmt.Errorf("vectors: OPRF seed of %q %w", vector.name, errOPAQUERun),
	)
	return inputs
}

type opaqueRun struct {
	request      [opaque.RegistrationRequestLen]byte
	response     [opaque.RegistrationResponseLen]byte
	registration opaque.Registration
	ke1          [opaque.KE1Len]byte
	ke2          [opaque.KE2Len]byte
	ke3          [opaque.KE3Len]byte
	random       opaque.ServerRandomness
}

func opaqueVector(vector opaqueCase) OPAQUE {
	inputs := opaqueSetup(vector)
	label := "opaque/" + vector.name + "/"
	run := &opaqueRun{}
	run.register(inputs, label)
	run.logIn(inputs, label, vector.name)
	salt := keys.PassphraseSalt(inputs.secret)
	return OPAQUE{
		Name:                 vector.name,
		ID:                   inputs.id.String(),
		S:                    hex.EncodeToString(inputs.secret[:]),
		KDFID:                int(vector.kdf),
		Passphrase:           vector.passphrase,
		PassphraseSalt:       hex.EncodeToString(salt[:]),
		Password:             hex.EncodeToString(inputs.password),
		Context:              hex.EncodeToString(inputs.config.Context),
		E:                    hex.EncodeToString(inputs.share[:]),
		CredentialIdentifier: hex.EncodeToString(inputs.credential),
		Pepper:               hex.EncodeToString(inputs.pepper),
		OPRFSeed:             hex.EncodeToString(inputs.serverKeys.OPRFSeed[:]),
		ServerKeySeed:        hex.EncodeToString(inputs.keySeed.MAC[:]),
		ServerPublicKey:      hex.EncodeToString(inputs.serverKeys.PublicKey[:]),
		BlindRegistration:    hex.EncodeToString(deterministicBlindBytes(label + "blind-registration")),
		EnvelopeNonce:        hex.EncodeToString(deterministic(label+"envelope-nonce", opaque.NonceLen)),
		RegistrationRequest:  core.Encode(run.request[:]),
		RegistrationResponse: core.Encode(run.response[:]),
		Registration:         core.Encode(run.registration.Record[:]),
		BlindLogin:           hex.EncodeToString(deterministicBlindBytes(label + "blind-login")),
		ClientNonce:          hex.EncodeToString(deterministic(label+"client-nonce", opaque.NonceLen)),
		ClientKeyshareSeed:   hex.EncodeToString(deterministic(label+"client-keyshare-seed", opaque.SeedLen)),
		MaskingNonce:         hex.EncodeToString(run.random.MaskingNonce[:]),
		ServerNonce:          hex.EncodeToString(run.random.ServerNonce[:]),
		ServerKeyshareSeed:   hex.EncodeToString(run.random.KeyshareSeed[:]),
		KE1:                  core.Encode(run.ke1[:]),
		KE2:                  core.Encode(run.ke2[:]),
		KE3:                  core.Encode(run.ke3[:]),
		P:                    hex.EncodeToString(run.registration.ExportKey[:]),
		Heavy:                vector.kdf == core.KDFScrypt,
	}
}

func (r *opaqueRun) register(inputs *opaqueInputs, label string) {
	request, state, err := opaque.CreateRegistrationRequestWith(
		inputs.password,
		deterministicBlind(label+"blind-registration"),
	)
	require(err == nil, fmt.Errorf("vectors: OPAQUE registration request: %w", err))
	r.request = request
	r.response = must(opaque.CreateRegistrationResponse(request[:], &inputs.serverKeys, inputs.credential))
	nonce := [opaque.NonceLen]byte(deterministic(label+"envelope-nonce", opaque.NonceLen))
	r.registration = must(opaque.FinalizeRegistrationRequestWith(&state, r.response[:], &inputs.config, &nonce))
}

func (r *opaqueRun) logIn(inputs *opaqueInputs, label, name string) {
	clientNonce := [opaque.NonceLen]byte(deterministic(label+"client-nonce", opaque.NonceLen))
	clientSeed := [opaque.SeedLen]byte(deterministic(label+"client-keyshare-seed", opaque.SeedLen))
	ke1, login, err := opaque.GenerateKE1With(
		inputs.password,
		deterministicBlind(label+"blind-login"),
		&clientNonce,
		&clientSeed,
	)
	require(err == nil, fmt.Errorf("vectors: OPAQUE run %q, KE1: %w", name, err))
	r.ke1 = ke1
	r.random = opaque.ServerRandomness{
		MaskingNonce: [opaque.NonceLen]byte(deterministic(label+"masking-nonce", opaque.NonceLen)),
		ServerNonce:  [opaque.NonceLen]byte(deterministic(label+"server-nonce", opaque.NonceLen)),
		KeyshareSeed: [opaque.SeedLen]byte(deterministic(label+"server-keyshare-seed", opaque.SeedLen)),
	}
	ke2, serverLogin, err := opaque.GenerateKE2With(
		&inputs.serverKeys,
		r.registration.Record[:],
		inputs.credential,
		ke1[:],
		&inputs.config,
		&r.random,
	)
	require(err == nil, fmt.Errorf("vectors: OPAQUE run %q, KE2: %w", name, err))
	r.ke2 = ke2
	result := must(login.GenerateKE3(ke2[:], &inputs.config))
	require(
		result.ExportKey == r.registration.ExportKey && serverLogin.ServerFinish(result.KE3[:]) == nil,
		fmt.Errorf("vectors: OPAQUE run %q %w", name, errOPAQUERun),
	)
	r.ke3 = result.KE3
}

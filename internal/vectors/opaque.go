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
	Name                 string  `json:"name"`
	ID                   string  `json:"id"`
	S                    string  `json:"s"`
	KDFID                int     `json:"kdf_id"`
	Passphrase           *string `json:"passphrase,omitempty"`
	PassphraseSalt       string  `json:"salt_pw_hex"`
	Password             string  `json:"password_hex"`
	Context              string  `json:"context_hex"`
	E                    string  `json:"e_hex"`
	CredentialIdentifier string  `json:"credential_identifier_hex"`
	Pepper               string  `json:"pepper_hex"`
	OPRFSeed             string  `json:"oprf_seed_hex"`
	ServerKeySeed        string  `json:"server_key_seed_hex"`
	ServerPublicKey      string  `json:"server_public_key_hex"`
	BlindRegistration    string  `json:"blind_registration_hex"`
	EnvelopeNonce        string  `json:"envelope_nonce_hex"`
	RegistrationRequest  string  `json:"registration_request"`
	RegistrationResponse string  `json:"registration_response"`
	Registration         string  `json:"registration"`
	BlindLogin           string  `json:"blind_login_hex"`
	ClientNonce          string  `json:"client_nonce_hex"`
	ClientKeyshareSeed   string  `json:"client_keyshare_seed_hex"`
	MaskingNonce         string  `json:"masking_nonce_hex"`
	ServerNonce          string  `json:"server_nonce_hex"`
	ServerKeyshareSeed   string  `json:"server_keyshare_seed_hex"`
	KE1                  string  `json:"ke1"`
	KE2                  string  `json:"ke2"`
	KE3                  string  `json:"ke3"`
	P                    string  `json:"p_hex"`
	Heavy                bool    `json:"heavy"`
}

type opaqueCase struct {
	name       string
	kdf        core.KDF
	passphrase *string
}

var opaqueCases = []opaqueCase{
	{"no-passphrase", core.KDFNone, nil},
	{"scrypt-ascii", core.KDFScrypt, new("correct horse battery staple")},
	{"scrypt-nfc-decomposed", core.KDFScrypt, new("héslo s češtinou")},
}

func (f *File) opaque() error {
	for _, vector := range opaqueCases {
		run, err := opaqueVector(vector)
		if err != nil {
			return err
		}
		f.OPAQUE = append(f.OPAQUE, run)
	}
	return nil
}

func deterministicBlind(label string) *[opaque.ScalarLen]byte {
	return (*[opaque.ScalarLen]byte)(deterministicBlindBytes(label))
}

func deterministicBlindBytes(label string) []byte {
	scalar, err := ristretto255.NewScalar().SetUniformBytes(deterministic(label, 64))
	if err != nil {
		panic(err)
	}
	return scalar.Bytes()
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

func opaqueSetup(vector opaqueCase) (*opaqueInputs, error) {
	inputs := &opaqueInputs{
		id:     deterministicID("opaque/" + vector.name),
		secret: deterministic32("opaque/s/" + vector.name),
		share:  deterministic32("opaque/e/" + vector.name),
		pepper: deterministic("pepper/current", pepper.SecretLen),
	}
	inputs.credential = keys.CredentialIdentifier(inputs.id, inputs.share)
	ring, err := pepper.NewRing(inputs.pepper, nil)
	if err != nil {
		return nil, err
	}
	if inputs.password, err = keys.Password(inputs.secret, vector.kdf, valueOr(vector.passphrase)); err != nil {
		return nil, err
	}
	if inputs.config, err = keys.OPAQUEConfig(inputs.id, vector.kdf); err != nil {
		return nil, err
	}
	if inputs.serverKeys, _, err = ring.OPAQUEKeys(inputs.id[:], ring.CurrentID()); err != nil {
		return nil, err
	}
	inputs.keySeed = ring.MAC(pepper.OPAQUEServer, inputs.id[:])
	oprfSeed, err := hkdf.Key(sha256.New, inputs.pepper, nil, infoOPRFSeed, opaque.OPRFSeedLen)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(oprfSeed, inputs.serverKeys.OPRFSeed[:]) {
		return nil, fmt.Errorf("vectors: OPRF seed of %q %w", vector.name, errOPAQUERun)
	}
	return inputs, nil
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

func opaqueVector(vector opaqueCase) (OPAQUE, error) {
	inputs, err := opaqueSetup(vector)
	if err != nil {
		return OPAQUE{}, err
	}
	label := "opaque/" + vector.name + "/"
	run := &opaqueRun{}
	if registerErr := run.register(inputs, label); registerErr != nil {
		return OPAQUE{}, registerErr
	}
	if loginErr := run.logIn(inputs, label); loginErr != nil {
		return OPAQUE{}, fmt.Errorf("vectors: OPAQUE run %q: %w", vector.name, loginErr)
	}
	salt, err := keys.PassphraseSalt(inputs.secret)
	if err != nil {
		return OPAQUE{}, err
	}
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
	}, nil
}

func (r *opaqueRun) register(inputs *opaqueInputs, label string) error {
	request, state, err := opaque.CreateRegistrationRequestWith(
		inputs.password,
		deterministicBlind(label+"blind-registration"),
	)
	if err != nil {
		return err
	}
	r.request = request
	if r.response, err = opaque.CreateRegistrationResponse(
		request[:],
		&inputs.serverKeys,
		inputs.credential,
	); err != nil {
		return err
	}
	nonce := [opaque.NonceLen]byte(deterministic(label+"envelope-nonce", opaque.NonceLen))
	r.registration, err = opaque.FinalizeRegistrationRequestWith(&state, r.response[:], &inputs.config, &nonce)
	return err
}

func (r *opaqueRun) logIn(inputs *opaqueInputs, label string) error {
	clientNonce := [opaque.NonceLen]byte(deterministic(label+"client-nonce", opaque.NonceLen))
	clientSeed := [opaque.SeedLen]byte(deterministic(label+"client-keyshare-seed", opaque.SeedLen))
	ke1, login, err := opaque.GenerateKE1With(
		inputs.password,
		deterministicBlind(label+"blind-login"),
		&clientNonce,
		&clientSeed,
	)
	if err != nil {
		return err
	}
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
	if err != nil {
		return err
	}
	r.ke2 = ke2
	result, err := login.GenerateKE3(ke2[:], &inputs.config)
	if err != nil {
		return err
	}
	if result.ExportKey != r.registration.ExportKey || serverLogin.ServerFinish(result.KE3[:]) != nil {
		return errOPAQUERun
	}
	r.ke3 = result.KE3
	return nil
}

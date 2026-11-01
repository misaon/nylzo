package vectors

import (
	"encoding/hex"
	"strings"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/keys"
	"github.com/misaon/nylzo/internal/core/signing"
)

type Derive struct {
	Name           string  `json:"name"`
	ID             string  `json:"id"`
	S              string  `json:"s"`
	KDFID          int     `json:"kdf_id"`
	Passphrase     *string `json:"passphrase,omitempty"`
	E              string  `json:"e_hex"`
	HSalt          string  `json:"hsalt_hex"`
	R              string  `json:"r_hex"`
	PassphraseSalt string  `json:"salt_pw_hex"`
	P              string  `json:"p_hex"`
	K              string  `json:"k_hex"`
	Commitment     string  `json:"kc_hex"`
	V              string  `json:"v_hex"`
	M              string  `json:"m_hex"`
	ReadKey        string  `json:"read_key"`
	VerifierKey    string  `json:"verifier_key"`
	ManageKey      string  `json:"manage_key"`
}

type deriveCase struct {
	name       string
	kdf        core.KDF
	passphrase *string
}

var deriveCases = []deriveCase{
	{"no-passphrase", core.KDFNone, nil},
	{"scrypt-ascii", core.KDFScrypt, new("correct horse battery staple")},
	{"scrypt-nfc-composed", core.KDFScrypt, new("héslo s češtinou")},
	{"scrypt-nfc-decomposed", core.KDFScrypt, new("héslo s češtinou")},
	{"scrypt-emoji", core.KDFScrypt, new("\U0001F510 tajné heslo")},
	{"scrypt-empty", core.KDFScrypt, new("")},
}

func (f *File) derive() error {
	for _, vector := range deriveCases {
		derived, err := deriveVector(vector)
		if err != nil {
			return err
		}
		f.Derive = append(f.Derive, derived)
	}
	return nil
}

func deriveVector(vector deriveCase) (Derive, error) {
	label := vector.name
	if strings.HasPrefix(label, "scrypt-nfc") {
		label = "scrypt-nfc"
	}
	id := deterministicID(label)
	secret := deterministic32("s/" + label)
	manageSeed := deterministic32("m/" + label)
	share := deterministic32("e/" + label)
	link, err := keys.DeriveLink(id, secret)
	if err != nil {
		return Derive{}, err
	}
	passphraseKey, err := keys.DerivePassphraseKey(vector.kdf, valueOr(vector.passphrase), &link.PassphraseSalt)
	if err != nil {
		return Derive{}, err
	}
	verifier, err := keys.DeriveVerifier(id, secret, passphraseKey)
	if err != nil {
		return Derive{}, err
	}
	content, err := keys.DeriveContent(id, secret, passphraseKey, share)
	if err != nil {
		return Derive{}, err
	}
	hsalt := keys.HSalt(id)
	readKey, manageKey := signing.PublicKey(&link.R), signing.PublicKey(manageSeed)
	verifierKey := signing.PublicKey(&verifier)
	derived := Derive{
		Name:           vector.name,
		ID:             id.String(),
		S:              hex.EncodeToString(secret[:]),
		KDFID:          int(vector.kdf),
		Passphrase:     vector.passphrase,
		HSalt:          hex.EncodeToString(hsalt[:]),
		R:              hex.EncodeToString(link.R[:]),
		PassphraseSalt: hex.EncodeToString(link.PassphraseSalt[:]),
		P:              hex.EncodeToString(passphraseKey),
		E:              hex.EncodeToString(share[:]),
		K:              hex.EncodeToString(content.K[:]),
		Commitment:     hex.EncodeToString(content.Commitment[:]),
		V:              hex.EncodeToString(verifier[:]),
		M:              hex.EncodeToString(manageSeed[:]),
		ReadKey:        core.Encode(readKey[:]),
		VerifierKey:    core.Encode(verifierKey[:]),
		ManageKey:      core.Encode(manageKey[:]),
	}
	return derived, nil
}

func valueOr(text *string) string {
	if text == nil {
		return ""
	}
	return *text
}

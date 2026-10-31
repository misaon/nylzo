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
	HSalt          string  `json:"hsalt_hex"`
	R              string  `json:"r_hex"`
	PassphraseSalt string  `json:"salt_pw_hex"`
	P              string  `json:"p_hex"`
	K              string  `json:"k_hex"`
	V              string  `json:"v_hex"`
	M              string  `json:"m_hex"`
	Fragment       string  `json:"fragment"`
	ReadKey        string  `json:"read_key"`
	VerifierKey    *string `json:"verifier_key,omitempty"`
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
	{"scrypt-nfc-decomposed", core.KDFScrypt, new("he\u0301slo s c\u030ces\u030ctinou")},
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
	if strings.HasPrefix(vector.name, "scrypt-nfc") {
		label = "scrypt-nfc"
	}
	id := deterministicID(label)
	secret := deterministic32("s/" + label)
	manageSeed := deterministic32("m/" + label)
	link, err := keys.DeriveLink(id, secret)
	if err != nil {
		return Derive{}, err
	}
	passphraseKey, err := keys.DerivePassphraseKey(vector.kdf, valueOr(vector.passphrase), &link.PassphraseSalt)
	if err != nil {
		return Derive{}, err
	}
	content, err := keys.DeriveContent(id, secret, passphraseKey)
	if err != nil {
		return Derive{}, err
	}
	hsalt := keys.HSalt(id)
	readKey, manageKey := signing.PublicKey(&link.R), signing.PublicKey(manageSeed)
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
		K:              hex.EncodeToString(content.K[:]),
		V:              hex.EncodeToString(content.V[:]),
		M:              hex.EncodeToString(manageSeed[:]),
		Fragment:       core.Fragment(vector.kdf, secret),
		ReadKey:        core.Encode(readKey[:]),
		ManageKey:      core.Encode(manageKey[:]),
	}
	if vector.kdf != core.KDFNone {
		verifierKey := signing.PublicKey(&content.V)
		derived.VerifierKey = new(core.Encode(verifierKey[:]))
	}
	return derived, nil
}

func valueOr(text *string) string {
	if text == nil {
		return ""
	}
	return *text
}

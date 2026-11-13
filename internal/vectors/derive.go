package vectors

import (
	"encoding/hex"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/keys"
	"github.com/misaon/nylzo/internal/core/signing"
)

// Derive pins the derivations of spec section 2 that need no server: R, salt_pw and, from P and E, K and K_c.
type Derive struct {
	Name           string `json:"name"`
	ID             string `json:"id"`
	S              string `json:"s"`
	P              string `json:"p_hex"`
	E              string `json:"e_hex"`
	HSalt          string `json:"hsalt_hex"`
	R              string `json:"r_hex"`
	PassphraseSalt string `json:"salt_pw_hex"`
	K              string `json:"k_hex"`
	Commitment     string `json:"kc_hex"`
	M              string `json:"m_hex"`
	ReadKey        string `json:"read_key"`
	ManageKey      string `json:"manage_key"`
}

var deriveCases = []string{"first", "second"}

func (f *File) derive() error {
	for _, name := range deriveCases {
		derived, err := deriveVector(name)
		if err != nil {
			return err
		}
		f.Derive = append(f.Derive, derived)
	}
	return nil
}

func deriveVector(name string) (Derive, error) {
	id := deterministicID(name)
	secret := deterministic32("s/" + name)
	manageSeed := deterministic32("m/" + name)
	share := deterministic32("e/" + name)
	exportKey := deterministicExportKey("p/" + name)
	read, err := keys.DeriveRead(id, secret)
	if err != nil {
		return Derive{}, err
	}
	salt, err := keys.PassphraseSalt(secret)
	if err != nil {
		return Derive{}, err
	}
	content, err := keys.DeriveContent(id, secret, exportKey, share)
	if err != nil {
		return Derive{}, err
	}
	hsalt := keys.HSalt(id)
	readKey, manageKey := signing.PublicKey(&read), signing.PublicKey(manageSeed)
	return Derive{
		Name:           name,
		ID:             id.String(),
		S:              hex.EncodeToString(secret[:]),
		P:              hex.EncodeToString(exportKey[:]),
		E:              hex.EncodeToString(share[:]),
		HSalt:          hex.EncodeToString(hsalt[:]),
		R:              hex.EncodeToString(read[:]),
		PassphraseSalt: hex.EncodeToString(salt[:]),
		K:              hex.EncodeToString(content.K[:]),
		Commitment:     hex.EncodeToString(content.Commitment[:]),
		M:              hex.EncodeToString(manageSeed[:]),
		ReadKey:        core.Encode(readKey[:]),
		ManageKey:      core.Encode(manageKey[:]),
	}, nil
}

func valueOr(text *string) string {
	if text == nil {
		return ""
	}
	return *text
}

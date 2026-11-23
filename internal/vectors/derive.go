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

func (f *File) derive() {
	for _, name := range deriveCases {
		f.Derive = append(f.Derive, deriveVector(name))
	}
}

func deriveVector(name string) Derive {
	id := deterministicID(name)
	secret := deterministic32("s/" + name)
	manageSeed := deterministic32("m/" + name)
	share := deterministic32("e/" + name)
	exportKey := deterministicExportKey("p/" + name)
	read := keys.DeriveRead(id, secret)
	salt := keys.PassphraseSalt(secret)
	content := must(keys.DeriveContent(id, secret, exportKey, share))
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
	}
}

package vectors

import (
	"encoding/hex"

	"golang.org/x/crypto/scrypt"
)

const rfc7914KeyLen = 64

type Scrypt struct {
	Name     string `json:"name"`
	Password string `json:"password"`
	Salt     string `json:"salt"`
	N        int    `json:"N"`
	R        int    `json:"r"`
	P        int    `json:"p"`
	DKLen    int    `json:"dkLen"`
	DK       string `json:"dk_hex"`
	Heavy    bool   `json:"heavy"`
}

var rfc7914Cases = []Scrypt{
	{Name: "rfc7914-1", Password: "", Salt: "", N: 16, R: 1, P: 1},
	{Name: "rfc7914-2", Password: "password", Salt: "NaCl", N: 1024, R: 8, P: 16},
	{Name: "rfc7914-3", Password: "pleaseletmein", Salt: "SodiumChloride", N: 16384, R: 8, P: 1},
	{Name: "rfc7914-4", Password: "pleaseletmein", Salt: "SodiumChloride", N: 1048576, R: 8, P: 1, Heavy: true},
}

func (f *File) scrypt() error {
	for _, vector := range rfc7914Cases {
		derived, err := scrypt.Key(
			[]byte(vector.Password),
			[]byte(vector.Salt),
			vector.N,
			vector.R,
			vector.P,
			rfc7914KeyLen,
		)
		if err != nil {
			return err
		}
		vector.DKLen = rfc7914KeyLen
		vector.DK = hex.EncodeToString(derived)
		f.Scrypt = append(f.Scrypt, vector)
	}
	return nil
}

package core

import (
	"encoding/base64"
	"errors"
)

var ErrEncoding = errors.New("core: value is not canonical base64url of the expected length")

var rawURL = base64.RawURLEncoding.Strict()

func Encode(b []byte) string {
	return rawURL.EncodeToString(b)
}

func DecodeExact(s string, n int) ([]byte, error) {
	if len(s) != rawURL.EncodedLen(n) {
		return nil, ErrEncoding
	}
	for i := 0; i < len(s); i++ {
		if !isB64URL(s[i]) {
			return nil, ErrEncoding
		}
	}
	out, err := rawURL.DecodeString(s)
	if err != nil || len(out) != n {
		return nil, ErrEncoding
	}
	return out, nil
}

func isB64URL(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_'
}

func Decode32(s string) (*[32]byte, error) {
	b, err := DecodeExact(s, 32)
	if err != nil {
		return nil, err
	}
	var out [32]byte
	copy(out[:], b)
	return &out, nil
}

package core

import (
	"encoding/base64"
	"errors"
)

var ErrEncoding = errors.New("core: value is not canonical base64url of the expected length")

var canonicalBase64URL = base64.RawURLEncoding.Strict()

func Encode(data []byte) string {
	return canonicalBase64URL.EncodeToString(data)
}

func DecodeExact(text string, length int) ([]byte, error) {
	if len(text) != canonicalBase64URL.EncodedLen(length) {
		return nil, ErrEncoding
	}
	decoded, err := canonicalBase64URL.DecodeString(text)
	if err != nil || len(decoded) != length {
		return nil, ErrEncoding
	}
	return decoded, nil
}

func Decode32(text string) (*[32]byte, error) {
	decoded, err := DecodeExact(text, 32)
	if err != nil {
		return nil, err
	}
	return (*[32]byte)(decoded), nil
}

package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

var ErrJSON = errors.New("core: malformed JSON")

func UnmarshalStrict(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(v); err != nil {
		return ErrJSON
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return ErrJSON
	}
	return nil
}

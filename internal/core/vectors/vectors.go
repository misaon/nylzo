package vectors

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"

	"github.com/misaon/nylzo/internal/core"
)

const (
	Spec      = "nylzo/v1"
	Generator = "go run ./cmd/nylzo-vectors -out spec/vectors.json"
	seed      = "nylzo/vectors/2026-10-22"
	inlineMax = 70000
	base      = "https://nylzo.example"
)

type File struct {
	Spec      string     `json:"spec"`
	Generator string     `json:"generator"`
	Scrypt    []Scrypt   `json:"scrypt_rfc7914"`
	Derive    []Derive   `json:"derive"`
	Envelope  []Envelope `json:"envelope"`
	Link      []Link     `json:"link"`
	Meta      []MetaCase `json:"meta"`
	Strength  []Strength `json:"passphrase_strength"`
	Request   []Request  `json:"request"`
	PoW       []PoW      `json:"pow"`
	Redact    []Redact   `json:"redact"`
	Server    Server     `json:"server"`
}

func Generate() (*File, error) {
	file := &File{Spec: Spec, Generator: Generator}
	steps := []func() error{
		file.scrypt,
		file.derive,
		file.envelope,
		file.link,
		file.meta,
		file.strength,
		file.request,
		file.pow,
		file.redact,
		file.server,
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return nil, err
		}
	}
	return file, nil
}

func Marshal(file *File) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(file); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func deterministic(label string, length int) []byte {
	output := make([]byte, 0, length+sha256.Size)
	for counter := uint32(0); len(output) < length; counter++ {
		sum := sha256.Sum256(binary.BigEndian.AppendUint32([]byte(seed+label), counter))
		output = append(output, sum[:]...)
	}
	return output[:length]
}

func deterministicID(label string) core.ID {
	return core.ID(deterministic("id/"+label, core.IDLen))
}

func deterministic32(label string) *[32]byte {
	return (*[32]byte)(deterministic(label, 32))
}

func counterBytes(length int) []byte {
	output := make([]byte, length)
	for i := range output {
		output[i] = byte(i)
	}
	return output
}

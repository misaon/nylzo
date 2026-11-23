package vectors

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"sync"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/keys"
)

const (
	Spec      = "nylzo/v3"
	Generator = "go run ./cmd/nylzo-vectors -out spec/vectors.json"
	seed      = "nylzo/vectors/2026-10-22"
	base      = "https://nylzo.example"
)

type File struct {
	Spec             string                `json:"spec"`
	Generator        string                `json:"generator"`
	Scrypt           []Scrypt              `json:"scrypt_rfc7914"`
	Derive           []Derive              `json:"derive"`
	OPAQUE           []OPAQUE              `json:"opaque"`
	Envelope         []Envelope            `json:"envelope"`
	Rejected         []Rejected            `json:"envelope_rejected"`
	CompressedLength []CompressedLength    `json:"compressed_length"`
	Share            []ShareCase           `json:"share"`
	ShareKeyRejected []ShareKeyRejected    `json:"share_key_rejected"`
	Link             []Link                `json:"link"`
	Meta             []MetaCase            `json:"meta"`
	Normalization    []Normalization       `json:"passphrase_normalization"`
	Strength         []Strength            `json:"passphrase_strength"`
	ListHash         string                `json:"passphrase_list_sha256"`
	AssignedHash     string                `json:"unicode_assigned_sha256"`
	AssignedVersion  string                `json:"unicode_assigned_version"`
	WordLists        []WordList            `json:"passphrase_words"`
	Generated        []GeneratedPassphrase `json:"generated_passphrase"`
	Request          []Request             `json:"request"`
	PoW              []PoW                 `json:"pow"`
	Server           Server                `json:"server"`
}

var errNotEncoded = errors.New("vectors: the vectors do not encode")

func Generate() *File {
	file := &File{Spec: Spec, Generator: Generator}
	steps := []func(){
		file.scrypt,
		file.derive,
		file.opaque,
		file.envelope,
		file.rejected,
		file.compressedLength,
		file.share,
		file.shareKeyRejected,
		file.link,
		file.meta,
		file.normalization,
		file.strength,
		file.passphraseWords,
		file.request,
		file.pow,
		file.server,
	}
	var wait sync.WaitGroup
	for _, step := range steps {
		wait.Go(step)
	}
	wait.Wait()
	return file
}

func Marshal(file *File) []byte {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	require(encoder.Encode(file) == nil, errNotEncoded)
	return buffer.Bytes()
}

//nolint:ireturn // T is the type of the value the caller hands in, returned unchanged.
func must[T any](value T, err error) T {
	require(err == nil, err)
	return value
}

func require(holds bool, disagreement error) {
	if !holds {
		panic(disagreement)
	}
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

func deterministicExportKey(label string) *keys.ExportKey {
	return (*keys.ExportKey)(deterministic(label, len(keys.ExportKey{})))
}

func counterBytes(length int) []byte {
	output := make([]byte, length)
	for i := range output {
		output[i] = byte(i)
	}
	return output
}

package envelope

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"strings"
	"unicode/utf8"
)

const (
	MaxFileName  = 255
	MaxMime      = 255
	MaxJSONDepth = 32
)

var ErrMeta = errors.New("core: invalid meta")

type FileMeta struct {
	Name string
	Mime string
	Size int64
}

type Meta struct {
	Text *string
	File *FileMeta
}

// The field order and the minimal escaping of json/v2 are the encoding that spec/vectors.json pins.
type metaObject struct {
	Version int         `json:"v"`
	Text    *string     `json:"text,omitzero"`
	File    *fileObject `json:"file,omitzero"`
}

type fileObject struct {
	Name        string `json:"name"`
	Mime        string `json:"mime"`
	Size        int64  `json:"size"`
	Encoding    string `json:"encoding,omitzero"`
	EncodedSize int64  `json:"encoded_size,omitzero"`
}

func (m Meta) Validate() error {
	if m.Text == nil && m.File == nil {
		return ErrMeta
	}
	if m.Text != nil && (len(*m.Text) > MaxTextBytes || !utf8.ValidString(*m.Text)) {
		return ErrMeta
	}
	if m.File != nil && !m.File.valid() {
		return ErrMeta
	}
	return nil
}

func (f *FileMeta) valid() bool {
	return validFileName(f.Name) && len(f.Mime) <= MaxMime && utf8.ValidString(f.Mime) &&
		f.Size >= 0 && f.Size <= MaxFileBytes
}

func validFileName(name string) bool {
	if name == "" || len(name) > MaxFileName || !utf8.ValidString(name) {
		return false
	}
	return !strings.ContainsFunc(name, func(character rune) bool {
		return character < 0x20 || character == 0x7f || character == '/' || character == '\\'
	})
}

func EncodeMeta(meta Meta) ([]byte, error) {
	return encodeStoredMeta(meta, 0)
}

func encodeStoredMeta(meta Meta, compressedSize int64) ([]byte, error) {
	if err := meta.Validate(); err != nil {
		return nil, err
	}
	object := metaObject{Version: 1, Text: meta.Text}
	if compressedSize > 0 {
		object.Version = 2
	}
	if meta.File != nil {
		object.File = &fileObject{Name: meta.File.Name, Mime: meta.File.Mime, Size: meta.File.Size}
		if compressedSize > 0 {
			object.File.Encoding, object.File.EncodedSize = "gzip", compressedSize
		}
	}
	encoded, err := jsonv2.Marshal(object)
	if err != nil {
		return nil, err
	}
	if len(encoded) > MaxMetaBytes {
		return nil, ErrMeta
	}
	return encoded, nil
}

func DecodeMeta(data []byte) (Meta, error) {
	stored, err := decodeStoredMeta(data)
	if err != nil || stored.compressedSize != 0 {
		return Meta{}, ErrMeta
	}
	return stored.meta, nil
}

type storedMeta struct {
	meta           Meta
	compressedSize int64
}

func decodeStoredMeta(data []byte) (storedMeta, error) {
	if len(data) > MaxMetaBytes || !utf8.Valid(data) || jsonDepth(data) > MaxJSONDepth {
		return storedMeta{}, ErrMeta
	}
	object, err := decodeObject(data)
	if err != nil {
		return storedMeta{}, err
	}
	var version int
	if err = decodeRequired(object, "v", &version); err != nil || (version != 1 && version != 2) {
		return storedMeta{}, ErrMeta
	}
	var meta Meta
	if raw := lookup(object, "text"); raw != nil {
		meta.Text = new(string)
		if json.Unmarshal(raw, meta.Text) != nil {
			return storedMeta{}, ErrMeta
		}
	}
	if raw := lookup(object, "file"); raw != nil {
		if meta.File, err = decodeFileMeta(raw); err != nil {
			return storedMeta{}, err
		}
	}
	if validationErr := meta.Validate(); validationErr != nil {
		return storedMeta{}, validationErr
	}
	compressedSize, err := decodeCompression(object, meta, version)
	return storedMeta{meta: meta, compressedSize: compressedSize}, err
}

func decodeCompression(object map[string]json.RawMessage, meta Meta, version int) (int64, error) {
	if version == 1 {
		return 0, nil
	}
	if meta.File == nil {
		return 0, ErrMeta
	}
	file, err := decodeObject(lookup(object, "file"))
	if err != nil {
		return 0, err
	}
	var encoding string
	var size int64
	if decodeRequired(file, "encoding", &encoding) != nil || encoding != "gzip" ||
		decodeRequired(file, "encoded_size", &size) != nil || size <= 0 || size >= meta.File.Size {
		return 0, ErrMeta
	}
	return size, nil
}

func jsonDepth(data []byte) int {
	depth, deepest := 0, 0
	inString, escaped := false, false
	for _, character := range data {
		switch {
		case escaped:
			escaped = false
		case inString:
			escaped = character == '\\'
			inString = character != '"'
		case character == '"':
			inString = true
		case character == '{' || character == '[':
			depth++
			deepest = max(deepest, depth)
		case character == '}' || character == ']':
			depth--
		default:
		}
	}
	return deepest
}

func decodeObject(data []byte) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if json.Unmarshal(data, &object) != nil || object == nil {
		return nil, ErrMeta
	}
	return object, nil
}

func lookup(object map[string]json.RawMessage, key string) json.RawMessage {
	if raw := object[key]; string(raw) != "null" {
		return raw
	}
	return nil
}

func decodeRequired(object map[string]json.RawMessage, key string, target any) error {
	raw := lookup(object, key)
	if raw == nil || json.Unmarshal(raw, target) != nil {
		return ErrMeta
	}
	return nil
}

func decodeFileMeta(raw json.RawMessage) (*FileMeta, error) {
	object, err := decodeObject(raw)
	if err != nil {
		return nil, err
	}
	file := &FileMeta{}
	if decodeRequired(object, "name", &file.Name) != nil || decodeRequired(object, "size", &file.Size) != nil {
		return nil, ErrMeta
	}
	if mime := lookup(object, "mime"); mime != nil && json.Unmarshal(mime, &file.Mime) != nil {
		return nil, ErrMeta
	}
	return file, nil
}

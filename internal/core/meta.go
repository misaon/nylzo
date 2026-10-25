package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
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
	if err := meta.Validate(); err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	buffer.WriteString(`{"v":1`)
	if meta.Text != nil {
		buffer.WriteString(`,"text":`)
		writeJSONString(&buffer, *meta.Text)
	}
	if meta.File != nil {
		buffer.WriteString(`,"file":{"name":`)
		writeJSONString(&buffer, meta.File.Name)
		buffer.WriteString(`,"mime":`)
		writeJSONString(&buffer, meta.File.Mime)
		buffer.WriteString(`,"size":`)
		buffer.WriteString(strconv.FormatInt(meta.File.Size, 10))
		buffer.WriteByte('}')
	}
	buffer.WriteByte('}')
	if buffer.Len() > MaxMetaBytes {
		return nil, ErrMeta
	}
	return buffer.Bytes(), nil
}

func DecodeMeta(data []byte) (Meta, error) {
	if len(data) > MaxMetaBytes || !utf8.Valid(data) || jsonDepth(data) > MaxJSONDepth {
		return Meta{}, ErrMeta
	}
	object, err := decodeObject(data)
	if err != nil {
		return Meta{}, err
	}
	var version int
	if err = decodeRequired(object, "v", &version); err != nil || version != 1 {
		return Meta{}, ErrMeta
	}
	var meta Meta
	if raw := lookup(object, "text"); raw != nil {
		meta.Text = new(string)
		if json.Unmarshal(raw, meta.Text) != nil {
			return Meta{}, ErrMeta
		}
	}
	if raw := lookup(object, "file"); raw != nil {
		if meta.File, err = decodeFileMeta(raw); err != nil {
			return Meta{}, err
		}
	}
	err = meta.Validate()
	if err != nil {
		return Meta{}, err
	}
	return meta, nil
}

const hexDigits = "0123456789abcdef"

func writeJSONString(buffer *bytes.Buffer, text string) {
	buffer.WriteByte('"')
	for _, character := range []byte(text) {
		switch {
		case character == '"':
			buffer.WriteString(`\"`)
		case character == '\\':
			buffer.WriteString(`\\`)
		case character == '\b':
			buffer.WriteString(`\b`)
		case character == '\t':
			buffer.WriteString(`\t`)
		case character == '\n':
			buffer.WriteString(`\n`)
		case character == '\f':
			buffer.WriteString(`\f`)
		case character == '\r':
			buffer.WriteString(`\r`)
		case character < 0x20:
			buffer.WriteString(`\u00`)
			buffer.WriteByte(hexDigits[character>>4])
			buffer.WriteByte(hexDigits[character&0xf])
		default:
			buffer.WriteByte(character)
		}
	}
	buffer.WriteByte('"')
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

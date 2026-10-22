package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"unicode/utf8"
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
	if m.Text != nil {
		if len(*m.Text) > MaxTextBytes || !utf8.ValidString(*m.Text) {
			return ErrMeta
		}
	}
	if m.File != nil {
		if err := validateFileName(m.File.Name); err != nil {
			return err
		}
		if len(m.File.Mime) > MaxMime || !utf8.ValidString(m.File.Mime) {
			return ErrMeta
		}
		if m.File.Size < 0 || m.File.Size > MaxFileBytes {
			return ErrMeta
		}
	}
	return nil
}

func validateFileName(name string) error {
	if len(name) == 0 || len(name) > MaxFileName || !utf8.ValidString(name) {
		return ErrMeta
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c < 0x20 || c == 0x7f || c == '/' || c == '\\' {
			return ErrMeta
		}
	}
	return nil
}

func EncodeMeta(m Meta) ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.WriteString(`{"v":1`)
	if m.Text != nil {
		b.WriteString(`,"text":`)
		writeJSONString(&b, *m.Text)
	}
	if m.File != nil {
		b.WriteString(`,"file":{"name":`)
		writeJSONString(&b, m.File.Name)
		b.WriteString(`,"mime":`)
		writeJSONString(&b, m.File.Mime)
		b.WriteString(`,"size":`)
		b.WriteString(strconv.FormatInt(m.File.Size, 10))
		b.WriteByte('}')
	}
	b.WriteByte('}')
	if b.Len() > MaxMetaBytes {
		return nil, ErrMeta
	}
	return b.Bytes(), nil
}

const hexDigits = "0123456789abcdef"

func writeJSONString(b *bytes.Buffer, s string) {
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			b.WriteString(`\"`)
		case c == '\\':
			b.WriteString(`\\`)
		case c == '\b':
			b.WriteString(`\b`)
		case c == '\t':
			b.WriteString(`\t`)
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\f':
			b.WriteString(`\f`)
		case c == '\r':
			b.WriteString(`\r`)
		case c < 0x20:
			b.WriteString(`\u00`)
			b.WriteByte(hexDigits[c>>4])
			b.WriteByte(hexDigits[c&0xf])
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
}

type fileWire struct {
	Name *string `json:"name"`
	Mime *string `json:"mime"`
	Size *int64  `json:"size"`
}

func decodeField(raw json.RawMessage, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(v); err != nil {
		return ErrMeta
	}
	return nil
}

func isNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func DecodeMeta(b []byte) (Meta, error) {
	if len(b) > MaxMetaBytes || !utf8.Valid(b) {
		return Meta{}, ErrMeta
	}
	var fields map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(&fields); err != nil || dec.More() || fields == nil {
		return Meta{}, ErrMeta
	}
	var v *int
	if raw, ok := fields["v"]; !ok || isNull(raw) || decodeField(raw, &v) != nil || v == nil || *v != 1 {
		return Meta{}, ErrMeta
	}
	var m Meta
	if raw, ok := fields["text"]; ok && !isNull(raw) {
		if err := decodeField(raw, &m.Text); err != nil {
			return Meta{}, err
		}
	}
	if raw, ok := fields["file"]; ok && !isNull(raw) {
		var f fileWire
		if err := decodeField(raw, &f); err != nil {
			return Meta{}, err
		}
		if f.Name == nil || f.Size == nil {
			return Meta{}, ErrMeta
		}
		mime := ""
		if f.Mime != nil {
			mime = *f.Mime
		}
		m.File = &FileMeta{Name: *f.Name, Mime: mime, Size: *f.Size}
	}
	if err := m.Validate(); err != nil {
		return Meta{}, err
	}
	return m, nil
}

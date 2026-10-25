package webclient

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"math"
	"strings"
)

const (
	publicURLPlaceholder = "__NYLZO_PUBLIC_URL__"
	publicStart          = "<!--nylzo:public-->"
	publicEnd            = "<!--/nylzo:public-->"
	startupNonce         = "nylzo-startup-check-nonce"
	englishHome          = "/"
	czechHome            = "/cs/"
)

var homeTemplates = map[string]string{englishHome: "home.en.html", czechHome: "home.cs.html"}

var gzipHeader = []byte{0x1f, 0x8b, 8, 0, 0, 0, 0, 0, 2, 255}

func IsHomePath(path string) bool {
	_, ok := homeTemplates[path]
	return ok
}

func templateFor(path string) string {
	if name, ok := homeTemplates[path]; ok {
		return name
	}
	return indexTemplate
}

func (b Build) Page(path, publicURL string) (string, error) {
	return render(b.Templates[templateFor(path)], publicURL)
}

type Pages map[string]Page

func (b Build) Render(publicURL string) (Pages, error) {
	pages := Pages{}
	for _, name := range templateNames {
		html, err := render(b.Templates[name], publicURL)
		if err != nil {
			return nil, fmt.Errorf("server: web client %s: %w", name, err)
		}
		if pages[name], err = newPage(html); err != nil {
			return nil, err
		}
	}
	return pages, nil
}

func (p Pages) For(path string) Page {
	return p[templateFor(path)]
}

func render(template, publicURL string) (string, error) {
	var out strings.Builder
	rest := template
	for {
		start, end := strings.Index(rest, publicStart), strings.Index(rest, publicEnd)
		if start < 0 && end < 0 {
			break
		}
		if start < 0 || end < start || strings.Contains(rest[start+len(publicStart):end], publicStart) {
			return "", errors.New("the public markers do not pair up")
		}
		outside, block := rest[:start], rest[start+len(publicStart):end]
		if strings.Contains(outside, publicURLPlaceholder) {
			return "", errors.New("the public URL placeholder appears outside a public block")
		}
		out.WriteString(outside)
		if publicURL != "" {
			out.WriteString(strings.ReplaceAll(block, publicURLPlaceholder, publicURL))
		}
		rest = rest[end+len(publicEnd):]
	}
	if strings.Contains(rest, publicURLPlaceholder) {
		return "", errors.New("the public URL placeholder appears outside a public block")
	}
	out.WriteString(rest)
	return out.String(), nil
}

// Page keeps the pieces around each nonce deflated once, so a compressed response costs no compression work.
type Page struct {
	pieces   [][]byte
	deflated [][]byte
	size     int
}

func newPage(html string) (Page, error) {
	page := Page{pieces: bytes.Split([]byte(html), []byte(NoncePlaceholder))}
	for _, piece := range page.pieces {
		deflated, err := deflate(piece)
		if err != nil {
			return Page{}, err
		}
		page.deflated = append(page.deflated, deflated)
		page.size += len(deflated)
	}
	if err := sameAfterGunzip(page.Gzip(startupNonce), page.Plain(startupNonce)); err != nil {
		return Page{}, fmt.Errorf("server: page compression: %w", err)
	}
	return page, nil
}

func deflate(piece []byte) ([]byte, error) {
	var buffer bytes.Buffer
	writer, err := flate.NewWriter(&buffer, flate.BestCompression)
	if err != nil {
		return nil, err
	}
	_, writeErr := writer.Write(piece)
	if flushErr := errors.Join(writeErr, writer.Flush()); flushErr != nil {
		return nil, flushErr
	}
	return buffer.Bytes(), nil
}

func (p Page) Plain(nonce string) []byte {
	return bytes.Join(p.pieces, []byte(nonce))
}

func (p Page) Gzip(nonce string) []byte {
	out := bytes.NewBuffer(make([]byte, 0, p.size+len(p.pieces)*(len(nonce)+5)+32))
	out.Write(gzipHeader)
	checksum := crc32.NewIEEE()
	nonceBlock := storedBlock(false, nonce)
	size := 0
	for i, piece := range p.pieces {
		if i > 0 {
			out.Write(nonceBlock)
			checksum.Write([]byte(nonce))
			size += len(nonce)
		}
		out.Write(p.deflated[i])
		checksum.Write(piece)
		size += len(piece)
	}
	out.Write(storedBlock(true, ""))
	out.Write(binary.LittleEndian.AppendUint32(nil, checksum.Sum32()))
	out.Write(binary.LittleEndian.AppendUint32(nil, uint32(size&math.MaxUint32)))
	return out.Bytes()
}

// storedBlock writes data uncompressed; a nonce always fits the 64 KiB limit of one block.
func storedBlock(final bool, data string) []byte {
	block := []byte{0}
	if final {
		block[0] = 1
	}
	length := uint16(len(data) & math.MaxUint16)
	block = binary.LittleEndian.AppendUint16(block, length)
	block = binary.LittleEndian.AppendUint16(block, ^length)
	return append(block, data...)
}

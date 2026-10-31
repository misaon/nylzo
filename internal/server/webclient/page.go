package webclient

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

const (
	publicURLPlaceholder = "__NYLZO_PUBLIC_URL__"
	publicStart          = "<!--nylzo:public-->"
	publicEnd            = "<!--/nylzo:public-->"
	englishHome          = "/"
	czechHome            = "/cs/"
	htmlContentType      = "text/html; charset=utf-8"
)

var homeTemplates = map[string]string{englishHome: "home.en.html", czechHome: "home.cs.html"}

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

type Page struct {
	body   []byte
	gzip   []byte
	policy string
}

func newPage(html string) (Page, error) {
	page := Page{body: []byte(html), policy: PagePolicy(html)}
	var buffer bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buffer, gzip.BestCompression)
	if err != nil {
		return Page{}, err
	}
	_, writeErr := writer.Write(page.body)
	if closeErr := errors.Join(writeErr, writer.Close()); closeErr != nil {
		return Page{}, fmt.Errorf("server: page compression: %w", closeErr)
	}
	page.gzip = buffer.Bytes()
	return page, nil
}

func (p Page) Body() []byte {
	return p.body
}

func (p Page) Policy() string {
	return p.policy
}

func (p Page) SHA256() string {
	return sha256Hex(p.body)
}

// Serve writes the page; the caller has set the headers shared with every other response.
func (p Page) Serve(w http.ResponseWriter, r *http.Request) {
	header := w.Header()
	header.Set("Content-Security-Policy", p.policy)
	header.Set("Content-Type", htmlContentType)
	header.Set("Vary", "Accept-Encoding")
	if AcceptsGzip(r.Header.Get("Accept-Encoding")) {
		header.Set("Content-Encoding", EncodingGzip)
		w.Write(p.gzip)
		return
	}
	w.Write(p.body)
}

func (a Asset) Serve(w http.ResponseWriter, r *http.Request) {
	header := w.Header()
	header.Set("Content-Type", a.ContentType)
	header.Set("Vary", "Accept-Encoding")
	if len(a.Gzip) > 0 && AcceptsGzip(r.Header.Get("Accept-Encoding")) {
		header.Set("Content-Encoding", EncodingGzip)
		w.Write(a.Gzip)
		return
	}
	w.Write(a.Body)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

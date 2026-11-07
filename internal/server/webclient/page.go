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
	sentPath             = "/sent"
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
	for name, template := range b.Templates {
		html, err := render(template, publicURL)
		if err != nil {
			return nil, fmt.Errorf("server: web client %s: %w", name, err)
		}
		policy := PagePolicy(html)
		if isStatusTemplate(name) {
			policy = StatusPolicy(html)
		}
		if pages[name], err = newPage(html, policy); err != nil {
			return nil, err
		}
	}
	return pages, nil
}

func (p Pages) For(path string) Page {
	return p[templateFor(path)]
}

func render(template, publicURL string) (string, error) {
	if publicURL != "" {
		if err := CheckPublicURL(publicURL); err != nil {
			return "", fmt.Errorf("public URL: %w", err)
		}
	}
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

func newPage(html, policy string) (Page, error) {
	page := Page{body: []byte(html), policy: policy}
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
	p.ServeStatus(w, r, http.StatusOK)
}

func (p Page) ServeStatus(w http.ResponseWriter, r *http.Request, status int) {
	header := w.Header()
	header.Set("Content-Security-Policy", p.policy)
	if r.URL.Path == transparencyPage {
		header.Del(connectionAllowlistHeader)
	}
	header.Set("Content-Type", htmlContentType)
	header.Add("Vary", "Accept-Encoding")
	body := p.body
	if AcceptsGzip(r.Header.Get("Accept-Encoding")) {
		header.Set("Content-Encoding", EncodingGzip)
		body = p.gzip
	}
	w.WriteHeader(status)
	w.Write(body)
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

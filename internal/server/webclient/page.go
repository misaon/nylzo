package webclient

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/misaon/nylzo/internal/server/operatoraddress"
)

const (
	publicURLPlaceholder = "__NYLZO_PUBLIC_URL__"
	publicStart          = "<!--nylzo:public-->"
	publicEnd            = "<!--/nylzo:public-->"
	sentPath             = "/sent"
	transparencyPath     = "/transparency"
	termsPath            = "/terms"
	agentsPath           = "/agents"
	robotsPath           = "/robots.txt"
	sitemapPath          = "/sitemap.xml"
	messagePagePath      = "/m/{id}"
	htmlContentType      = "text/html; charset=utf-8"
)

var homeTemplates = homeTemplateNames()

func homeTemplateNames() map[string]string {
	names := map[string]string{}
	for _, language := range languages {
		names[homePath(language.tag)] = homeTemplate(language.tag)
	}
	return names
}

var homeRedirects = homeRedirectPaths()

func homeRedirectPaths() map[string]string {
	redirects := map[string]string{}
	for _, path := range homePaths() {
		if path != "/" {
			redirects[strings.TrimSuffix(path, "/")] = path
		}
	}
	return redirects
}

var (
	errPublicMarkersUnpaired    = errors.New("the public markers do not pair up")
	errPlaceholderOutsidePublic = errors.New("the public URL placeholder appears outside a public block")
)

// publicDocuments carry no message and no message metadata, so a crawler may load them over classical TLS.
var publicDocuments = slices.Concat(homePaths(), slices.Sorted(maps.Keys(homeRedirects)), []string{
	termsPath,
	transparencyPath,
	agentsPath,
	"/" + agentsGuideFile,
	robotsPath,
	sitemapPath,
	SecurityTextPath,
	ManifestPath,
})

func IsPublicDocument(path string) bool {
	if name, ok := strings.CutPrefix(path, "/assets/"); ok {
		return name != "" && !strings.Contains(name, "/")
	}
	return slices.Contains(publicDocuments, path)
}

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

type Pages map[string]Page

// Render fills the templates for an instance; every page may reach only the addresses ConnectionAllowlist names,
// and the home pages of nylzo.com also Cloudflare Web Analytics.
func (b Build) Render(publicURL, contact string) (Pages, error) {
	return b.renderPages(publicURL, contact, true)
}

// RenderLocal fills the templates for the local client, whose pages load nothing from another origin.
func (b Build) RenderLocal(instance, contact string) (Pages, error) {
	return b.renderPages(instance, contact, false)
}

func (b Build) renderPages(publicURL, contact string, analyticsAllowed bool) (Pages, error) {
	if err := operatoraddress.CheckContact(contact); err != nil {
		return nil, fmt.Errorf("server: operator contact: %w", err)
	}
	pages := Pages{}
	for name, template := range b.Templates {
		html, err := render(template, publicURL)
		if err != nil {
			return nil, fmt.Errorf("server: web client %s: %w", name, err)
		}
		analytics := analyticsAllowed && countsVisits(publicURL, name)
		policy := pagePolicy(html, analytics)
		if isStatusTemplate(name) {
			policy = StatusPolicy(html)
		}
		headers := &pageHeaders{policy: policy, allowlist: pageAllowlist(contact, analytics), analytics: analytics}
		if pages[name], err = newPage(html, headers); err != nil {
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
		if err := operatoraddress.CheckPublicURL(publicURL); err != nil {
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
			return "", errPublicMarkersUnpaired
		}
		outside, block := rest[:start], rest[start+len(publicStart):end]
		if strings.Contains(outside, publicURLPlaceholder) {
			return "", errPlaceholderOutsidePublic
		}
		out.WriteString(outside)
		if publicURL != "" {
			out.WriteString(strings.ReplaceAll(block, publicURLPlaceholder, publicURL))
		}
		rest = rest[end+len(publicEnd):]
	}
	if strings.Contains(rest, publicURLPlaceholder) {
		return "", errPlaceholderOutsidePublic
	}
	out.WriteString(rest)
	return out.String(), nil
}

type Page struct {
	body    []byte
	gzip    []byte
	headers *pageHeaders
}

type pageHeaders struct {
	policy    string
	allowlist string
	analytics bool
}

func newPage(html string, headers *pageHeaders) (Page, error) {
	page := Page{body: []byte(html), headers: headers}
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
	return p.ownHeaders().policy
}

func (p Page) Allowlist() string {
	return p.ownHeaders().allowlist
}

// SecurityHeaders lists the headers the page carries at requestPath besides its policy and allowlist.
func (p Page) SecurityHeaders(requestPath string) map[string]string {
	headers := SecurityHeaders(requestPath)
	if p.ownHeaders().analytics {
		headers["Permissions-Policy"] = analyticsPermissionsPolicy
		delete(headers, integrityPolicyHeader)
	}
	return headers
}

// ContactlessAllowlist is the page's Connection-Allowlist from a front that was not given the operator's contact.
func (p Page) ContactlessAllowlist() string {
	return pageAllowlist("", p.ownHeaders().analytics)
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
	headers := p.ownHeaders()
	header.Set("Content-Security-Policy", headers.policy)
	header.Set(connectionAllowlistHeader, headers.allowlist)
	if headers.analytics {
		header.Set("Permissions-Policy", analyticsPermissionsPolicy)
		// Cloudflare ships the beacon without a version a hash could pin, and Integrity-Policy refuses a script without one.
		header.Del(integrityPolicyHeader)
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

// ownHeaders keeps a page the build lacks from going out without a policy.
func (p Page) ownHeaders() pageHeaders {
	if p.headers == nil {
		return pageHeaders{policy: NonHTMLCSP, allowlist: "(response-origin)"}
	}
	return *p.headers
}

func (a Asset) Serve(w http.ResponseWriter, r *http.Request) {
	header := w.Header()
	header.Set("Content-Type", a.ContentType)
	header.Set("Vary", "Accept-Encoding")
	if cachedForever(a.ContentType) {
		header.Set("Cache-Control", immutableAsset)
	}
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

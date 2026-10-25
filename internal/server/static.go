package server

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/misaon/nylzo/internal/core"
)

//go:embed web
var webFiles embed.FS

const noncePlaceholder = "__NYLZO_CSP_NONCE__"

const previewHTML = `<!doctype html><html lang="cs"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta name="robots" content="noindex, nofollow"><title>Nylzo</title></head><body><p>Odkaz vede na jednorázovou zprávu. Otevřete ho v prohlížeči zařízení, ne v náhledu aplikace.</p></body></html>`

var previewAgents = []string{
	"slackbot", "twitterbot", "facebookexternalhit", "facebot", "whatsapp", "telegrambot", "discordbot",
	"linkedinbot", "skypeuripreview", "applebot", "googlebot", "bingbot", "embedly", "iframely",
	"pinterestbot", "redditbot", "mastodon", "signal", "viber", "yahoo! slurp", "duckduckbot", "petalbot",
}

var assetTypes = map[string]string{
	".js":    "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".woff2": "font/woff2",
	".svg":   "image/svg+xml",
	".png":   "image/png",
}

const encodingGzip = "gzip"

var precompressed = map[string]string{".gz": encodingGzip}

var assetReference = regexp.MustCompile(
	`(?:(?:src|href|content)="(?:` + publicURLPlaceholder + `)?|url\("?)/assets/([^"')]*)`,
)

type webClient struct {
	built     bool
	templates map[string]string
	assets    map[string]webAsset
}

type webAsset struct {
	contentType string
	bodies      map[string][]byte
}

func loadWebClient(files fs.FS) (webClient, error) {
	if _, err := fs.Stat(files, "web/dist/"+indexTemplate); errors.Is(err, fs.ErrNotExist) {
		placeholder, err := fs.ReadFile(files, "web/placeholder.html")
		if err != nil {
			return webClient{}, fmt.Errorf("server: web placeholder: %w", err)
		}
		client := webClient{templates: map[string]string{}}
		for _, name := range templateNames {
			client.templates[name] = string(placeholder)
		}
		return client, nil
	}
	client := webClient{built: true, templates: map[string]string{}, assets: map[string]webAsset{}}
	if err := client.loadAssets(files); err != nil {
		return webClient{}, err
	}
	for _, name := range templateNames {
		if err := client.loadTemplate(files, name); err != nil {
			return webClient{}, err
		}
	}
	return client, nil
}

func (c *webClient) loadTemplate(files fs.FS, name string) error {
	template, err := fs.ReadFile(files, "web/dist/"+name)
	if err != nil {
		return fmt.Errorf("server: web client %s: %w", name, err)
	}
	if !bytes.Contains(template, []byte(noncePlaceholder)) {
		return fmt.Errorf("server: web client %s has no nonce placeholder", name)
	}
	if _, err := RenderPage(string(template), "https://nylzo.invalid"); err != nil {
		return fmt.Errorf("server: web client %s: %w", name, err)
	}
	for _, match := range assetReference.FindAllSubmatch(template, -1) {
		if _, ok := c.assets[string(match[1])]; !ok {
			return fmt.Errorf("server: web client %s references the missing asset %q", name, match[1])
		}
	}
	c.templates[name] = string(template)
	return nil
}

func (c *webClient) loadAssets(files fs.FS) error {
	entries, err := fs.ReadDir(files, "web/dist/assets")
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("server: web assets: %w", err)
	}
	bodies := map[string]map[string][]byte{}
	for _, entry := range entries {
		name, encoding := entry.Name(), ""
		if e, ok := precompressed[path.Ext(name)]; ok {
			name, encoding = strings.TrimSuffix(name, path.Ext(name)), e
		}
		if _, ok := assetTypes[path.Ext(name)]; !ok {
			return fmt.Errorf("server: web asset %s has no known type", entry.Name())
		}
		body, err := fs.ReadFile(files, "web/dist/assets/"+entry.Name())
		if err != nil {
			return fmt.Errorf("server: web asset %s: %w", entry.Name(), err)
		}
		if bodies[name] == nil {
			bodies[name] = map[string][]byte{}
		}
		bodies[name][encoding] = body
	}
	for name, variants := range bodies {
		plain, ok := variants[""]
		if !ok {
			return fmt.Errorf("server: web asset %s has a compressed variant without the file", name)
		}
		if gz, ok := variants[encodingGzip]; ok {
			if err := sameAfterGunzip(gz, plain); err != nil {
				return fmt.Errorf("server: web asset %s.gz: %w", name, err)
			}
		}
		c.assets[name] = webAsset{contentType: assetTypes[path.Ext(name)], bodies: variants}
	}
	return nil
}

func sameAfterGunzip(compressed, plain []byte) error {
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return err
	}
	unpacked, err := io.ReadAll(io.LimitReader(reader, int64(len(plain))+1))
	if err != nil {
		return err
	}
	if !bytes.Equal(unpacked, plain) {
		return errors.New("does not unpack to the file")
	}
	return nil
}

func acceptedEncodings(header string) map[string]bool {
	accepted := map[string]bool{}
	wildcard := false
	explicit := map[string]bool{}
	for part := range strings.SplitSeq(header, ",") {
		token, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		token = strings.ToLower(strings.TrimSpace(token))
		quality := 1.0
		if name, value, ok := strings.Cut(strings.TrimSpace(params), "="); ok && strings.TrimSpace(name) == "q" {
			if q, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
				quality = q
			}
		}
		if token == "*" {
			wildcard = quality > 0
			continue
		}
		explicit[token] = true
		accepted[token] = quality > 0
	}
	if !explicit[encodingGzip] {
		accepted[encodingGzip] = wildcard
	}
	return accepted
}

func isPreviewAgent(ua string) bool {
	ua = strings.ToLower(ua)
	return slices.ContainsFunc(previewAgents, func(a string) bool { return strings.Contains(ua, a) })
}

func (s *Server) handlePage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id != "" {
		if _, err := core.ParseID(id); err != nil {
			http.NotFound(w, r)
			return
		}
	}
	nonce := rand.Text()
	h := w.Header()
	h.Set("Content-Security-Policy", fmt.Sprintf(cspTemplate, nonce, nonce))
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Vary", "Accept-Encoding")
	if id != "" && isPreviewAgent(r.UserAgent()) {
		w.Write([]byte(previewHTML))
		return
	}
	page := s.pages[PageTemplate(r.URL.Path)]
	if acceptedEncodings(r.Header.Get("Accept-Encoding"))[encodingGzip] {
		h.Set("Content-Encoding", encodingGzip)
		w.Write(page.gzip(nonce))
		return
	}
	w.Write(page.plain(nonce))
}

func (s *Server) handleAsset(w http.ResponseWriter, r *http.Request) {
	asset, ok := s.web.assets[r.PathValue("name")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", asset.contentType)
	h.Set("Vary", "Accept-Encoding")
	if body, ok := asset.bodies[encodingGzip]; ok && acceptedEncodings(r.Header.Get("Accept-Encoding"))[encodingGzip] {
		h.Set("Content-Encoding", encodingGzip)
		w.Write(body)
		return
	}
	w.Write(asset.bodies[""])
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("ok"))
}

func (s *Server) handleAPINotFound(w http.ResponseWriter, r *http.Request) {
	writeNotFound(w)
}

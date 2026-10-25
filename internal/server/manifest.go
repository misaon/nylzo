package server

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"maps"
	"net/http"
	"slices"
)

const (
	ClientManifestPath = "/.well-known/nylzo/client.json"
	TransparencyPath   = "/.well-known/nylzo/transparency.json"
	NoncePlaceholder   = noncePlaceholder
	AssetCSP           = apiCSP
	EncodingGzip       = encodingGzip
)

var ClientPages = []string{"/", "/cs/", "/sent", "/report", "/transparency", "/m/{id}"}

var manifestPages = []string{"/", "/cs/", "/sent"}

func PageCSP(nonce string) string {
	return fmt.Sprintf(cspTemplate, nonce, nonce)
}

func SecurityHeaders(path string) map[string]string {
	headers := maps.Clone(securityHeaderValues)
	if !isHomePath(path) {
		headers["X-Robots-Tag"] = robotsTag
	}
	return headers
}

type ClientFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type ClientManifest struct {
	Server           string       `json:"server"`
	Built            bool         `json:"built"`
	NoncePlaceholder string       `json:"nonce_placeholder"`
	PublicURL        string       `json:"public_url"`
	Pages            []string     `json:"pages"`
	Files            []ClientFile `json:"files"`
}

type EmbeddedAsset struct {
	ContentType string
	Body        []byte
	Gzip        []byte
}

type EmbeddedClient struct {
	Built     bool
	Templates map[string]string
	Assets    map[string]EmbeddedAsset
}

func Embedded() (EmbeddedClient, error) {
	return LoadEmbedded(webFiles)
}

func LoadEmbedded(files fs.FS) (EmbeddedClient, error) {
	web, err := loadWebClient(files)
	if err != nil {
		return EmbeddedClient{}, err
	}
	return web.embedded(), nil
}

func WithWebFiles(files fs.FS) Option {
	return func(s *Server) {
		if files != nil {
			s.webFiles = files
		}
	}
}

func (c webClient) embedded() EmbeddedClient {
	out := EmbeddedClient{Built: c.built, Templates: maps.Clone(c.templates), Assets: map[string]EmbeddedAsset{}}
	for name, asset := range c.assets {
		out.Assets[name] = EmbeddedAsset{
			ContentType: asset.contentType,
			Body:        asset.bodies[""],
			Gzip:        asset.bodies[encodingGzip],
		}
	}
	return out
}

func (c EmbeddedClient) Page(path, publicURL string) (string, error) {
	return RenderPage(c.Templates[PageTemplate(path)], publicURL)
}

func (c EmbeddedClient) Manifest(version, publicURL string) ClientManifest {
	m := ClientManifest{
		Server:           version,
		Built:            c.Built,
		NoncePlaceholder: noncePlaceholder,
		PublicURL:        publicURL,
		Pages:            slices.Clone(ClientPages),
		Files:            []ClientFile{},
	}
	if !c.Built {
		return m
	}
	for _, path := range manifestPages {
		m.Files = append(m.Files, ClientFile{Path: path, SHA256: sha256Hex([]byte(c.Templates[PageTemplate(path)]))})
	}
	for _, name := range slices.Sorted(maps.Keys(c.Assets)) {
		m.Files = append(m.Files, ClientFile{Path: "/assets/" + name, SHA256: sha256Hex(c.Assets[name].Body)})
	}
	return m
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (s *Server) handleClientManifest(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.manifest)
}

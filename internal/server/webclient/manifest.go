package webclient

import (
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"slices"

	"github.com/misaon/nylzo/internal/buildinfo"
)

const ManifestPath = "/.well-known/nylzo/client.json"

var PagePaths = []string{englishHome, czechHome, "/sent", "/report", "/transparency", "/m/{id}"}

var manifestPages = []string{englishHome, czechHome, "/sent"}

type ManifestFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type Manifest struct {
	Build            buildinfo.Info `json:"build"`
	Server           string         `json:"server"`
	Built            bool           `json:"built"`
	NoncePlaceholder string         `json:"nonce_placeholder"`
	PublicURL        string         `json:"public_url"`
	Pages            []string       `json:"pages"`
	Files            []ManifestFile `json:"files"`
}

func (b Build) Manifest(version, publicURL string, information ...buildinfo.Info) Manifest {
	build := buildinfo.Info{Version: version}
	if len(information) > 0 {
		build = information[0]
		build.Version = version
	}
	manifest := Manifest{
		Build:            build,
		Server:           version,
		Built:            b.Built,
		NoncePlaceholder: NoncePlaceholder,
		PublicURL:        publicURL,
		Pages:            slices.Clone(PagePaths),
		Files:            []ManifestFile{},
	}
	if !b.Built {
		return manifest
	}
	for _, path := range manifestPages {
		template := []byte(b.Templates[templateFor(path)])
		manifest.Files = append(manifest.Files, ManifestFile{Path: path, SHA256: sha256Hex(template)})
	}
	for _, name := range slices.Sorted(maps.Keys(b.Assets)) {
		file := ManifestFile{Path: "/assets/" + name, SHA256: sha256Hex(b.Assets[name].Body)}
		manifest.Files = append(manifest.Files, file)
	}
	return manifest
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

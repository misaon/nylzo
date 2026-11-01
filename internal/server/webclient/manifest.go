package webclient

import (
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

type ManifestTemplate struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

type Manifest struct {
	Build     buildinfo.Info     `json:"build"`
	Server    string             `json:"server"`
	Built     bool               `json:"built"`
	PublicURL string             `json:"public_url"`
	Pages     []string           `json:"pages"`
	Files     []ManifestFile     `json:"files"`
	Templates []ManifestTemplate `json:"templates"`
}

func (b Build) Manifest(version, publicURL string, pages Pages, build buildinfo.Info) Manifest {
	build.Version = version
	manifest := Manifest{
		Build:     build,
		Server:    version,
		Built:     b.Built,
		PublicURL: publicURL,
		Pages:     slices.Clone(PagePaths),
		Files:     []ManifestFile{},
		Templates: []ManifestTemplate{},
	}
	if !b.Built {
		return manifest
	}
	for _, path := range manifestPages {
		manifest.Files = append(manifest.Files, ManifestFile{Path: path, SHA256: pages.For(path).SHA256()})
	}
	for _, name := range slices.Sorted(maps.Keys(b.Assets)) {
		file := ManifestFile{Path: "/assets/" + name, SHA256: sha256Hex(b.Assets[name].Body)}
		manifest.Files = append(manifest.Files, file)
	}
	for _, name := range slices.Sorted(maps.Keys(b.Templates)) {
		template := ManifestTemplate{Name: name, SHA256: sha256Hex([]byte(b.Templates[name]))}
		manifest.Templates = append(manifest.Templates, template)
	}
	return manifest
}

package webclient

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"
)

const (
	EncodingGzip    = "gzip"
	indexTemplate   = "index.html"
	placeholderFile = "placeholder.html"
	distDirectory   = "dist"
	assetsDirectory = "dist/assets"
	agentsGuideFile = "agents.md"
	faviconFile     = "favicon.ico"
)

var (
	errMissingAsset          = errors.New("references the missing asset")
	errUnknownAssetType      = errors.New("has no known type")
	errCompressedWithoutFile = errors.New("has a compressed variant without the file")
	errGunzipDiffers         = errors.New("does not unpack to the file")
)

var templateNames = append([]string{indexTemplate}, HomeTemplates()...)

var statusTemplateNames = statusTemplateList()

// HomeTemplates names the prerendered home page of every language, which a build must contain.
func HomeTemplates() []string {
	names := make([]string, 0, len(languages))
	for _, language := range languages {
		names = append(names, homeTemplate(language.tag))
	}
	return names
}

func statusTemplateList() []string {
	pages := []string{serverErrorPage, maintenancePage}
	names := make([]string, 0, len(pages)*len(languages))
	for _, page := range pages {
		for _, language := range languages {
			names = append(names, page+"."+language.tag+".html")
		}
	}
	return names
}

func isStatusTemplate(name string) bool {
	return slices.Contains(statusTemplateNames, name)
}

var rootFiles = map[string]string{
	agentsGuideFile: "text/markdown; charset=utf-8",
	faviconFile:     "image/vnd.microsoft.icon",
}

var assetTypes = map[string]string{
	".json":  "application/json; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".woff2": "font/woff2",
	".svg":   "image/svg+xml",
	".png":   "image/png",
}

var assetReference = regexp.MustCompile(
	`(?:(?:src|href|content)="(?:` + publicURLPlaceholder + `)?|url\("?)/assets/([^"')]*)`,
)

type Asset struct {
	ContentType string
	Body        []byte
	Gzip        []byte
}

type Build struct {
	Built     bool
	Templates map[string]string
	Assets    map[string]Asset
	RootFiles map[string]Asset
}

func Load(files fs.FS) (Build, error) {
	_, err := fs.Stat(files, path.Join(distDirectory, indexTemplate))
	if errors.Is(err, fs.ErrNotExist) {
		return loadPlaceholder(files)
	}
	build := Build{Built: true, Templates: map[string]string{}, RootFiles: map[string]Asset{}}
	if build.Assets, err = loadAssets(files); err != nil {
		return Build{}, err
	}
	for _, name := range templateNames {
		if build.Templates[name], err = loadTemplate(files, name, build.Assets); err != nil {
			return Build{}, err
		}
	}
	for _, name := range append([]string{adminTemplate}, statusTemplateNames...) {
		if _, statErr := fs.Stat(files, path.Join(distDirectory, name)); errors.Is(statErr, fs.ErrNotExist) {
			continue
		}
		if build.Templates[name], err = loadTemplate(files, name, build.Assets); err != nil {
			return Build{}, err
		}
	}
	for name, contentType := range rootFiles {
		body, readErr := fs.ReadFile(files, path.Join(distDirectory, name))
		if errors.Is(readErr, fs.ErrNotExist) {
			continue
		}
		if readErr != nil {
			return Build{}, fmt.Errorf("server: web client %s: %w", name, readErr)
		}
		build.RootFiles[name] = Asset{ContentType: contentType, Body: body}
	}
	return build, nil
}

func loadPlaceholder(files fs.FS) (Build, error) {
	placeholder, err := fs.ReadFile(files, placeholderFile)
	if err != nil {
		return Build{}, fmt.Errorf("server: web placeholder: %w", err)
	}
	build := Build{Templates: map[string]string{}}
	for _, name := range templateNames {
		build.Templates[name] = string(placeholder)
	}
	return build, nil
}

func loadTemplate(files fs.FS, name string, assets map[string]Asset) (string, error) {
	template, err := fs.ReadFile(files, path.Join(distDirectory, name))
	if err != nil {
		return "", fmt.Errorf("server: web client %s: %w", name, err)
	}
	if _, err = render(string(template), "https://nylzo.invalid"); err != nil {
		return "", fmt.Errorf("server: web client %s: %w", name, err)
	}
	for _, match := range assetReference.FindAllSubmatch(template, -1) {
		if _, ok := assets[string(match[1])]; !ok {
			return "", fmt.Errorf("server: web client %s %w %q", name, errMissingAsset, match[1])
		}
	}
	return string(template), nil
}

func loadAssets(files fs.FS) (map[string]Asset, error) {
	assets := map[string]Asset{}
	entries, err := fs.ReadDir(files, assetsDirectory)
	if errors.Is(err, fs.ErrNotExist) {
		return assets, nil
	}
	if err != nil {
		return nil, fmt.Errorf("server: web assets: %w", err)
	}
	gzipped := map[string][]byte{}
	for _, entry := range entries {
		name, compressed := strings.CutSuffix(entry.Name(), ".gz")
		contentType, ok := assetTypes[path.Ext(name)]
		if !ok {
			return nil, fmt.Errorf("server: web asset %s %w", entry.Name(), errUnknownAssetType)
		}
		body, readErr := fs.ReadFile(files, path.Join(assetsDirectory, entry.Name()))
		if readErr != nil {
			return nil, fmt.Errorf("server: web asset %s: %w", entry.Name(), readErr)
		}
		if compressed {
			gzipped[name] = body
		} else {
			assets[name] = Asset{ContentType: contentType, Body: body}
		}
	}
	for name, compressed := range gzipped {
		asset, ok := assets[name]
		if !ok {
			return nil, fmt.Errorf("server: web asset %s %w", name, errCompressedWithoutFile)
		}
		if gunzipErr := sameAfterGunzip(compressed, asset.Body); gunzipErr != nil {
			return nil, fmt.Errorf("server: web asset %s.gz: %w", name, gunzipErr)
		}
		asset.Gzip = compressed
		assets[name] = asset
	}
	return assets, nil
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
		return errGunzipDiffers
	}
	return nil
}

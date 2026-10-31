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
	"strings"
)

const (
	EncodingGzip    = "gzip"
	indexTemplate   = "index.html"
	placeholderFile = "placeholder.html"
	distDirectory   = "dist"
	assetsDirectory = "dist/assets"
)

var templateNames = []string{indexTemplate, "home.en.html", "home.cs.html"}

var assetTypes = map[string]string{
	".json":  "application/json; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".woff2": "font/woff2",
	".svg":   "image/svg+xml",
	".png":   "image/png",
}

var compressedVariants = map[string]string{".gz": EncodingGzip}

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
}

func Load(files fs.FS) (Build, error) {
	_, err := fs.Stat(files, path.Join(distDirectory, indexTemplate))
	if errors.Is(err, fs.ErrNotExist) {
		return loadPlaceholder(files)
	}
	build := Build{Built: true, Templates: map[string]string{}}
	if build.Assets, err = loadAssets(files); err != nil {
		return Build{}, err
	}
	for _, name := range templateNames {
		if build.Templates[name], err = loadTemplate(files, name, build.Assets); err != nil {
			return Build{}, err
		}
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
			return "", fmt.Errorf("server: web client %s references the missing asset %q", name, match[1])
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
	variants := map[string]map[string][]byte{}
	for _, entry := range entries {
		name, encoding := entry.Name(), ""
		if compressed, ok := compressedVariants[path.Ext(name)]; ok {
			name, encoding = strings.TrimSuffix(name, path.Ext(name)), compressed
		}
		if _, ok := assetTypes[path.Ext(name)]; !ok {
			return nil, fmt.Errorf("server: web asset %s has no known type", entry.Name())
		}
		if variants[name] == nil {
			variants[name] = map[string][]byte{}
		}
		if variants[name][encoding], err = fs.ReadFile(files, path.Join(assetsDirectory, entry.Name())); err != nil {
			return nil, fmt.Errorf("server: web asset %s: %w", entry.Name(), err)
		}
	}
	for name, bodies := range variants {
		if assets[name], err = newAsset(name, bodies); err != nil {
			return nil, err
		}
	}
	return assets, nil
}

func newAsset(name string, bodies map[string][]byte) (Asset, error) {
	plain, ok := bodies[""]
	if !ok {
		return Asset{}, fmt.Errorf("server: web asset %s has a compressed variant without the file", name)
	}
	compressed, ok := bodies[EncodingGzip]
	if ok {
		if err := sameAfterGunzip(compressed, plain); err != nil {
			return Asset{}, fmt.Errorf("server: web asset %s.gz: %w", name, err)
		}
	}
	return Asset{ContentType: assetTypes[path.Ext(name)], Body: plain, Gzip: compressed}, nil
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

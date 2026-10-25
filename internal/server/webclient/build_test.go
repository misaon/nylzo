package webclient_test

import (
	"bytes"
	"compress/gzip"
	"maps"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/misaon/nylzo/internal/server/webclient"
)

const (
	builtIndex = `<!doctype html><html><head><link rel="icon" href="/assets/favicon-d4.svg" type="image/svg+xml"><script type="module" src="/assets/index-a1.js" nonce="__NYLZO_CSP_NONCE__"></script><link rel="stylesheet" href="/assets/style-b2.css" nonce="__NYLZO_CSP_NONCE__"></head><body></body></html>`
	appJS      = "console.log('nylzo')"
	faviconSVG = `<svg xmlns="http://www.w3.org/2000/svg"/>`
	faviconPNG = "\x89PNG\r\n\x1a\n"
)

func builtHome(language string) string {
	return `<!doctype html><html lang="` + language + `"><head><title>home ` + language + `</title>
<!--nylzo:public-->
<link rel="canonical" href="__NYLZO_PUBLIC_URL__/"><meta property="og:image" content="__NYLZO_PUBLIC_URL__/assets/favicon-e5.png">
<!--/nylzo:public-->
<script type="module" src="/assets/index-a1.js" nonce="__NYLZO_CSP_NONCE__"></script><link rel="stylesheet" href="/assets/style-b2.css" nonce="__NYLZO_CSP_NONCE__"></head><body><h1>home ` + language + `</h1></body></html>`
}

func templates(index, english, czech string) fstest.MapFS {
	return fstest.MapFS{
		"dist/index.html":   {Data: []byte(index)},
		"dist/home.en.html": {Data: []byte(english)},
		"dist/home.cs.html": {Data: []byte(czech)},
	}
}

func gzipped(t *testing.T, data string) []byte {
	t.Helper()
	var out bytes.Buffer
	writer := gzip.NewWriter(&out)
	if _, err := writer.Write([]byte(data)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func builtClient(t *testing.T) fstest.MapFS {
	t.Helper()
	files := templates(builtIndex, builtHome("en"), builtHome("cs"))
	maps.Copy(files, fstest.MapFS{
		"placeholder.html":             {Data: []byte("placeholder")},
		"dist/assets/index-a1.js":      {Data: []byte(appJS)},
		"dist/assets/index-a1.js.gz":   {Data: gzipped(t, appJS)},
		"dist/assets/style-b2.css":     {Data: []byte("body{}")},
		"dist/assets/kdf-worker-c3.js": {Data: []byte("self.onmessage=null")},
		"dist/assets/favicon-d4.svg":   {Data: []byte(faviconSVG)},
		"dist/assets/favicon-e5.png":   {Data: []byte(faviconPNG)},
	})
	return files
}

func load(t *testing.T, files fstest.MapFS) webclient.Build {
	t.Helper()
	build, err := webclient.Load(files)
	if err != nil {
		t.Fatal(err)
	}
	return build
}

func TestLoadTypesEveryAsset(t *testing.T) {
	t.Parallel()
	build := load(t, builtClient(t))
	for name, contentType := range map[string]string{
		"index-a1.js":      "text/javascript; charset=utf-8",
		"style-b2.css":     "text/css; charset=utf-8",
		"kdf-worker-c3.js": "text/javascript; charset=utf-8",
		"favicon-d4.svg":   "image/svg+xml",
		"favicon-e5.png":   "image/png",
	} {
		if asset := build.Assets[name]; asset.ContentType != contentType || len(asset.Body) == 0 {
			t.Fatalf("%s: %+v", name, asset)
		}
	}
	if len(build.Assets) != 5 || len(build.Assets["index-a1.js"].Gzip) == 0 ||
		build.Assets["style-b2.css"].Gzip != nil {
		t.Fatalf("assets and their gzip variants: %+v", build.Assets)
	}
	placeholder := load(t, fstest.MapFS{"placeholder.html": {Data: []byte("placeholder")}})
	if placeholder.Built || len(placeholder.Assets) != 0 || placeholder.Templates["home.cs.html"] != "placeholder" {
		t.Fatalf("a missing build must give the placeholder: %+v", placeholder)
	}
	const page = `<p nonce="__NYLZO_CSP_NONCE__">built</p>`
	if withoutAssets := load(t, templates(page, page, page)); !withoutAssets.Built {
		t.Fatal("a build without assets must load")
	}
}

func TestLoadRefusesABrokenBuild(t *testing.T) {
	t.Parallel()
	const page = `<p nonce="__NYLZO_CSP_NONCE__"></p>`
	index := func(extra fstest.MapFS) fstest.MapFS {
		files := templates(page, page, page)
		maps.Copy(files, extra)
		return files
	}
	home := func(czech string) fstest.MapFS {
		return index(fstest.MapFS{"dist/home.cs.html": {Data: []byte(czech)}})
	}
	withoutCzech := index(nil)
	delete(withoutCzech, "dist/home.cs.html")
	for name, files := range map[string]fstest.MapFS{
		"unknown extension":                 index(fstest.MapFS{"dist/assets/index-a1.js.map": {Data: []byte("{}")}}),
		"nested directory":                  index(fstest.MapFS{"dist/assets/sub/a.js": {}}),
		"directory named like a script":     index(fstest.MapFS{"dist/assets/x.js/a.js": {}}),
		"compressed variant without a file": index(fstest.MapFS{"dist/assets/a.js.gz": {Data: gzipped(t, "x")}}),
		"brotli variant": index(fstest.MapFS{
			"dist/assets/a.js":    {Data: []byte("a")},
			"dist/assets/a.js.br": {Data: []byte("a")},
		}),
		"gzip that is not the file": index(fstest.MapFS{
			"dist/assets/a.js":    {Data: []byte("a")},
			"dist/assets/a.js.gz": {Data: gzipped(t, "b")},
		}),
		"gzip that is not gzip": index(fstest.MapFS{
			"dist/assets/a.js":    {Data: []byte("a")},
			"dist/assets/a.js.gz": {Data: []byte("a")},
		}),
		"compressed unknown type": index(fstest.MapFS{"dist/assets/a.map": {}, "dist/assets/a.map.gz": {}}),
		"index without the nonce": index(fstest.MapFS{
			"dist/index.html":  {Data: []byte(`<script src="/assets/a.js"></script>`)},
			"dist/assets/a.js": {},
		}),
		"index with a missing asset": index(fstest.MapFS{
			"dist/index.html": {
				Data: []byte(`<script nonce="__NYLZO_CSP_NONCE__" src="/assets/a.js"></script><link href="/assets/gone.css">`),
			},
			"dist/assets/a.js": {},
		}),
		"missing Czech home page":     withoutCzech,
		"home page without the nonce": home(`<p></p>`),
		"public block that does not close": home(
			page + `<!--nylzo:public--><link rel="canonical" href="__NYLZO_PUBLIC_URL__/">`,
		),
		"public block that closes first": home(page + `<!--/nylzo:public--><!--nylzo:public-->`),
		"nested public blocks": home(
			page + `<!--nylzo:public--><!--nylzo:public--><!--/nylzo:public--><!--/nylzo:public-->`,
		),
		"public URL outside a public block": home(page + `<link rel="canonical" href="__NYLZO_PUBLIC_URL__/">`),
		"social image that is not embedded": home(
			page + `<!--nylzo:public--><meta property="og:image" content="__NYLZO_PUBLIC_URL__/assets/og-gone.png"><!--/nylzo:public-->`,
		),
		"inline style with a missing font": home(
			page + `<style nonce="__NYLZO_CSP_NONCE__">@font-face{src:url(/assets/gone.woff2)}</style>`,
		),
		"quoted font that is not embedded": home(
			page + `<style nonce="__NYLZO_CSP_NONCE__">@font-face{src:url("/assets/gone.woff2")}</style>`,
		),
	} {
		if _, err := webclient.Load(files); err == nil || !strings.HasPrefix(err.Error(), "server: ") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestAcceptsGzip(t *testing.T) {
	t.Parallel()
	for header, want := range map[string]bool{
		"":                  false,
		"identity":          false,
		"br":                false,
		"gzip":              true,
		"gzip, deflate, br": true,
		"br;q=0, gzip":      true,
		"GZIP;Q=0.5":        true,
		"*":                 true,
		"*;q=0":             false,
		"gzip;q=0, br":      false,
		"*, gzip;q=0":       false,
		"*, br;q=0":         true,
		"*, gzip;q=-1":      false,
	} {
		if got := webclient.AcceptsGzip(header); got != want {
			t.Errorf("Accept-Encoding %q: %v", header, got)
		}
	}
}

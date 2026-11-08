package webclient

import (
	"html"
	"maps"
	"slices"
	"strings"
)

type previewText struct {
	title       string
	description string
}

var previewTexts = map[string]previewText{
	"en": {"One-time message · Nylzo", "Open the link in your browser. The message appears only after a click."},
	"cs": {"Jednorázová zpráva · Nylzo", "Otevřete odkaz v prohlížeči, zpráva se zobrazí až po kliknutí."},
}

// previewHTML is the page link-preview agents get for a message link. It is the same for every message,
// so a chat preview tells nothing about the message, its sender or its expiry.
func previewHTML(language, publicURL, image string) string {
	text := previewTexts[language]
	var page strings.Builder
	page.WriteString(`<!doctype html><html lang="` + language + `"><head><meta charset="utf-8">`)
	page.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1">`)
	page.WriteString(`<meta name="robots" content="noindex, nofollow"><title>` + text.title + `</title>`)
	page.WriteString(`<meta property="og:type" content="website"><meta property="og:site_name" content="Nylzo">`)
	page.WriteString(`<meta property="og:title" content="` + text.title + `">`)
	page.WriteString(`<meta property="og:description" content="` + text.description + `">`)
	card := "summary"
	if publicURL != "" && image != "" {
		page.WriteString(`<meta property="og:image" content="` + html.EscapeString(publicURL+"/assets/"+image) + `">`)
		card = "summary_large_image"
	}
	page.WriteString(`<meta name="twitter:card" content="` + card + `"></head><body>`)
	page.WriteString(`<p>` + text.description + `</p></body></html>`)
	return page.String()
}

func previewPages(publicURL string, assets map[string]Asset) map[string][]byte {
	image := ""
	for _, name := range slices.Sorted(maps.Keys(assets)) {
		if strings.HasPrefix(name, "og-image-") && strings.HasSuffix(name, ".png") {
			image = name
			break
		}
	}
	pages := map[string][]byte{}
	for _, language := range pageLanguages {
		pages[language] = []byte(previewHTML(language, publicURL, image))
	}
	return pages
}

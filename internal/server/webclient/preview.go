package webclient

import (
	"html"
	"maps"
	"slices"
	"strings"
)

const (
	previewTitle       = "Jednorázová zpráva / One-time message · Nylzo"
	previewDescription = "Otevřete odkaz v prohlížeči, zpráva se zobrazí až po kliknutí. " +
		"Open the link in your browser; the message appears only after a click."
)

// PreviewHTML is the page link-preview agents get for a message link. It is the same for every message,
// so a chat preview tells nothing about the message, its sender or its expiry.
func PreviewHTML(publicURL, image string) string {
	var page strings.Builder
	page.WriteString(`<!doctype html><html lang="cs"><head><meta charset="utf-8">`)
	page.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1">`)
	page.WriteString(`<meta name="robots" content="noindex, nofollow"><title>` + previewTitle + `</title>`)
	page.WriteString(`<meta property="og:type" content="website"><meta property="og:site_name" content="Nylzo">`)
	page.WriteString(`<meta property="og:title" content="` + previewTitle + `">`)
	page.WriteString(`<meta property="og:description" content="` + previewDescription + `">`)
	card := "summary"
	if publicURL != "" && image != "" {
		page.WriteString(`<meta property="og:image" content="` + html.EscapeString(publicURL+"/assets/"+image) + `">`)
		card = "summary_large_image"
	}
	page.WriteString(`<meta name="twitter:card" content="` + card + `"></head><body>`)
	page.WriteString(`<p>Otevřete odkaz v prohlížeči, zpráva se zobrazí až po kliknutí.</p>`)
	page.WriteString(`<p lang="en">Open the link in your browser; the message appears only after a click.</p>`)
	page.WriteString(`</body></html>`)
	return page.String()
}

func previewImage(assets map[string]Asset) string {
	for _, name := range slices.Sorted(maps.Keys(assets)) {
		if strings.HasPrefix(name, "og-image-") && strings.HasSuffix(name, ".png") {
			return name
		}
	}
	return ""
}

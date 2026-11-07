package webclient

import (
	"fmt"
	"strings"
)

func Robots(publicURL string) []byte {
	body := "User-agent: *\nDisallow: /api/\n"
	if publicURL != "" {
		body += "\nSitemap: " + publicURL + "/sitemap.xml\n"
	}
	return []byte(body)
}

func Sitemap(publicURL string) []byte {
	if publicURL == "" {
		return nil
	}
	var links strings.Builder
	for _, alternate := range []struct{ language, path string }{
		{"en", englishHome},
		{"cs", czechHome},
		{"x-default", englishHome},
	} {
		fmt.Fprintf(
			&links,
			`<xhtml:link rel="alternate" hreflang="%s" href="%s%s"/>`,
			alternate.language,
			publicURL,
			alternate.path,
		)
	}
	var out strings.Builder
	out.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	out.WriteString(
		`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">` + "\n",
	)
	for _, path := range []string{englishHome, czechHome} {
		fmt.Fprintf(&out, "<url><loc>%s%s</loc>%s</url>\n", publicURL, path, links.String())
	}
	out.WriteString("</urlset>\n")
	return []byte(out.String())
}

package webclient

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	SecurityTextPath      = "/.well-known/security.txt"
	projectAdvisories     = "https://github.com/misaon/nylzo/security/advisories/new"
	projectSecurityPolicy = "https://github.com/misaon/nylzo/blob/main/SECURITY.md"
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

// SecurityText expires a year after the build's source commit, so an instance on an old build shows a stale file.
func SecurityText(publicURL, contact, buildDate string) []byte {
	built, err := time.Parse(time.RFC3339, buildDate)
	if err != nil {
		return nil
	}
	var out strings.Builder
	if contact != "" {
		if !strings.HasPrefix(contact, "https://") {
			contact = "mailto:" + url.PathEscape(contact)
		}
		fmt.Fprintf(&out, "Contact: %s\n", contact)
	}
	fmt.Fprintf(&out, "Contact: %s\n", projectAdvisories)
	fmt.Fprintf(&out, "Expires: %s\n", built.UTC().AddDate(1, 0, 0).Format(time.RFC3339))
	fmt.Fprintf(&out, "Policy: %s\n", projectSecurityPolicy)
	out.WriteString("Preferred-Languages: en, cs\n")
	if publicURL != "" {
		fmt.Fprintf(&out, "Canonical: %s%s\n", publicURL, SecurityTextPath)
	}
	return []byte(out.String())
}

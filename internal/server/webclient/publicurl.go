package webclient

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/misaon/nylzo/internal/loopback"
)

var dnsName = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)*$`)

func CheckPublicURL(publicURL string) error {
	parsed, err := url.Parse(publicURL)
	if err != nil || parsed.Scheme+"://"+parsed.Host != publicURL || strings.HasSuffix(parsed.Host, ":") {
		return errors.New("want https://host or https://host:port with nothing after it")
	}
	host := parsed.Hostname()
	if portErr := checkPort(parsed.Port()); portErr != nil {
		return portErr
	}
	if hostErr := checkHost(host); hostErr != nil {
		return hostErr
	}
	if parsed.Scheme == "https" || parsed.Scheme == "http" && loopback.Host(host) {
		return nil
	}
	return errors.New("want https://; http:// is only for localhost or a loopback address")
}

func checkPort(port string) error {
	if port == "" {
		return nil
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 || strconv.Itoa(number) != port {
		return fmt.Errorf("%q is not a port number", port)
	}
	return nil
}

func checkHost(host string) error {
	if address, err := netip.ParseAddr(host); err == nil {
		if address.Zone() != "" {
			return errors.New("the address must not carry a zone")
		}
		return nil
	}
	if len(host) > 253 || !dnsName.MatchString(host) {
		return fmt.Errorf("the host %q must be a lowercase ASCII name or an IP address", host)
	}
	return nil
}

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

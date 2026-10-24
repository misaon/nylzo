package server

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	publicURLPlaceholder = "__NYLZO_PUBLIC_URL__"
	publicStart          = "<!--nylzo:public-->"
	publicEnd            = "<!--/nylzo:public-->"
	indexTemplate        = "index.html"
)

var templateNames = []string{indexTemplate, "home.en.html", "home.cs.html"}

var homeTemplates = map[string]string{"/": "home.en.html", "/cs/": "home.cs.html"}

var dnsName = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)*$`)

var loopbackHosts = []string{"localhost", "127.0.0.1", "::1"}

func PageTemplate(path string) string {
	if name, ok := homeTemplates[path]; ok {
		return name
	}
	return indexTemplate
}

func isHomePath(path string) bool {
	_, ok := homeTemplates[path]
	return ok
}

func CheckPublicURL(v string) error {
	u, err := url.Parse(v)
	if err != nil || u.Scheme+"://"+u.Host != v || strings.HasSuffix(u.Host, ":") {
		return errors.New("want https://host or https://host:port with nothing after it")
	}
	host, port := u.Hostname(), u.Port()
	if port != "" {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 || strconv.Itoa(n) != port {
			return fmt.Errorf("%q is not a port number", port)
		}
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		if addr.Zone() != "" {
			return errors.New("the address must not carry a zone")
		}
	} else if len(host) > 253 || !dnsName.MatchString(host) {
		return fmt.Errorf("the host %q must be a lowercase ASCII name or an IP address", host)
	}
	if u.Scheme == "https" || u.Scheme == "http" && slices.Contains(loopbackHosts, host) {
		return nil
	}
	return errors.New("want https://; http:// is only for localhost, 127.0.0.1 or [::1]")
}

func RenderPage(template, publicURL string) (string, error) {
	var out strings.Builder
	rest := template
	for {
		start, end := strings.Index(rest, publicStart), strings.Index(rest, publicEnd)
		if start < 0 && end < 0 {
			break
		}
		if start < 0 || end < start || strings.Contains(rest[start+len(publicStart):end], publicStart) {
			return "", errors.New("the public markers do not pair up")
		}
		outside, block := rest[:start], rest[start+len(publicStart):end]
		if strings.Contains(outside, publicURLPlaceholder) {
			return "", errors.New("the public URL placeholder appears outside a public block")
		}
		out.WriteString(outside)
		if publicURL != "" {
			out.WriteString(strings.ReplaceAll(block, publicURLPlaceholder, publicURL))
		}
		rest = rest[end+len(publicEnd):]
	}
	if strings.Contains(rest, publicURLPlaceholder) {
		return "", errors.New("the public URL placeholder appears outside a public block")
	}
	out.WriteString(rest)
	return out.String(), nil
}

type page struct {
	pieces   [][]byte
	deflated [][]byte
	size     int
}

var gzipHeader = []byte{0x1f, 0x8b, 8, 0, 0, 0, 0, 0, 2, 255}

func newPage(html string) (page, error) {
	p := page{pieces: bytes.Split([]byte(html), []byte(noncePlaceholder))}
	for _, piece := range p.pieces {
		var buf bytes.Buffer
		w, err := flate.NewWriter(&buf, flate.BestCompression)
		if err != nil {
			return page{}, err
		}
		if _, err := w.Write(piece); err != nil {
			return page{}, err
		}
		if err := w.Flush(); err != nil {
			return page{}, err
		}
		p.deflated = append(p.deflated, buf.Bytes())
		p.size += len(buf.Bytes())
	}
	const probe = "nylzo-startup-check-nonce"
	if err := sameAfterGunzip(p.gzip(probe), p.plain(probe)); err != nil {
		return page{}, fmt.Errorf("server: page compression: %w", err)
	}
	return p, nil
}

func (p page) plain(nonce string) []byte {
	return bytes.Join(p.pieces, []byte(nonce))
}

func (p page) gzip(nonce string) []byte {
	out := bytes.NewBuffer(make([]byte, 0, p.size+len(p.pieces)*(len(nonce)+5)+32))
	out.Write(gzipHeader)
	crc := crc32.NewIEEE()
	var size uint32
	for i, piece := range p.pieces {
		out.Write(p.deflated[i])
		crc.Write(piece)
		size += uint32(len(piece))
		if i == len(p.pieces)-1 {
			break
		}
		out.Write(storedBlock(0, nonce))
		crc.Write([]byte(nonce))
		size += uint32(len(nonce))
	}
	out.Write(storedBlock(1, ""))
	out.Write(binary.LittleEndian.AppendUint32(nil, crc.Sum32()))
	out.Write(binary.LittleEndian.AppendUint32(nil, size))
	return out.Bytes()
}

func storedBlock(final byte, data string) []byte {
	block := []byte{final}
	block = binary.LittleEndian.AppendUint16(block, uint16(len(data)))
	block = binary.LittleEndian.AppendUint16(block, ^uint16(len(data)))
	return append(block, data...)
}

func renderPages(c webClient, publicURL string) (map[string]page, error) {
	pages := map[string]page{}
	for _, name := range templateNames {
		html, err := RenderPage(c.templates[name], publicURL)
		if err != nil {
			return nil, fmt.Errorf("server: web client %s: %w", name, err)
		}
		if pages[name], err = newPage(html); err != nil {
			return nil, err
		}
	}
	return pages, nil
}

func robotsTxt(publicURL string) []byte {
	body := "User-agent: *\nDisallow: /api/\n"
	if publicURL != "" {
		body += "\nSitemap: " + publicURL + "/sitemap.xml\n"
	}
	return []byte(body)
}

func sitemapXML(publicURL string) []byte {
	var links strings.Builder
	for _, alt := range []struct{ lang, path string }{{"en", "/"}, {"cs", "/cs/"}, {"x-default", "/"}} {
		fmt.Fprintf(&links, `<xhtml:link rel="alternate" hreflang="%s" href="%s%s"/>`, alt.lang, publicURL, alt.path)
	}
	var out strings.Builder
	out.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	out.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">` + "\n")
	for _, path := range []string{"/", "/cs/"} {
		fmt.Fprintf(&out, "<url><loc>%s%s</loc>%s</url>\n", publicURL, path, links.String())
	}
	out.WriteString("</urlset>\n")
	return []byte(out.String())
}

func (s *Server) handleRobots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(s.robots)
}

func (s *Server) handleSitemap(w http.ResponseWriter, r *http.Request) {
	if s.sitemap == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Write(s.sitemap)
}

func (s *Server) handleCzechHome(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/cs/", http.StatusPermanentRedirect)
}

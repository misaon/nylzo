package webclient

import (
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

var (
	inlineScript    = regexp.MustCompile(`(?is)<script\b([^>]*)>(.*?)</script\s*>`)
	inlineStyle     = regexp.MustCompile(`(?is)<style\b[^>]*>(.*?)</style\s*>`)
	sourceAttribute = regexp.MustCompile(`(?i)\ssrc\s*=`)
	dataBlockType   = regexp.MustCompile(`(?i)\stype\s*=\s*"application/ld\+json"`)
)

// PagePolicy allows the inline scripts and styles of html by hash, so every response carries the same header.
func PagePolicy(html string) string {
	hashes := inlineHashes(html)
	return fmt.Sprintf(pagePolicyFormat, sourceList(hashes.scripts), sourceList(hashes.styles))
}

// StatusPolicy admits only the inline code of a status page, which loads no module, opens no connection and starts no worker.
func StatusPolicy(html string) string {
	hashes := inlineHashes(html)
	allowed := "'none'"
	if len(hashes.scripts) > 0 {
		allowed = strings.Join(hashes.scripts, " ")
	}
	return fmt.Sprintf(statusPolicyFormat, allowed, sourceList(hashes.styles))
}

type inlineCode struct {
	scripts []string
	styles  []string
}

func inlineHashes(html string) inlineCode {
	var hashes inlineCode
	for _, match := range inlineScript.FindAllStringSubmatch(html, -1) {
		if !sourceAttribute.MatchString(match[1]) && !dataBlockType.MatchString(match[1]) {
			hashes.scripts = appendHash(hashes.scripts, match[2])
		}
	}
	for _, match := range inlineStyle.FindAllStringSubmatch(html, -1) {
		hashes.styles = appendHash(hashes.styles, match[1])
	}
	return hashes
}

func appendHash(sources []string, content string) []string {
	sum := sha512.Sum384([]byte(content))
	source := "'sha384-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
	if slices.Contains(sources, source) {
		return sources
	}
	return append(sources, source)
}

func sourceList(sources []string) string {
	if len(sources) == 0 {
		return ""
	}
	return " " + strings.Join(sources, " ")
}

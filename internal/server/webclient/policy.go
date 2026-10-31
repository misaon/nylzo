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
	var scripts []string
	for _, match := range inlineScript.FindAllStringSubmatch(html, -1) {
		if !sourceAttribute.MatchString(match[1]) && !dataBlockType.MatchString(match[1]) {
			scripts = appendHash(scripts, match[2])
		}
	}
	var styles []string
	for _, match := range inlineStyle.FindAllStringSubmatch(html, -1) {
		styles = appendHash(styles, match[1])
	}
	return fmt.Sprintf(pagePolicyFormat, sourceList(scripts), sourceList(styles))
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

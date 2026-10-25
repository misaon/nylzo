package webclient

import (
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

const NonHTMLCSP = "default-src 'none'; frame-ancestors 'none'"

const pageCSPFormat = "default-src 'none'; script-src 'nonce-%s'; style-src 'nonce-%s'; connect-src 'self'; img-src 'self'; font-src 'self'; worker-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'; require-trusted-types-for 'script'; trusted-types svelte-trusted-html nylzo-kdf"

const robotsTag = "noindex, nofollow"

const PreviewHTML = `<!doctype html><html lang="cs"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta name="robots" content="noindex, nofollow"><title>Nylzo</title></head><body><p>Odkaz vede na jednorázovou zprávu. Otevřete ho v prohlížeči zařízení, ne v náhledu aplikace.</p></body></html>`

var securityHeaders = map[string]string{
	"Referrer-Policy":              "no-referrer",
	"Cache-Control":                "no-store",
	"X-Content-Type-Options":       "nosniff",
	"Cross-Origin-Opener-Policy":   "same-origin",
	"Cross-Origin-Resource-Policy": "same-origin",
	"Strict-Transport-Security":    "max-age=31536000; includeSubDomains",
	"Permissions-Policy":           "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()",
}

var previewAgents = []string{
	"slackbot", "twitterbot", "facebookexternalhit", "facebot", "whatsapp", "telegrambot", "discordbot",
	"linkedinbot", "skypeuripreview", "applebot", "googlebot", "bingbot", "embedly", "iframely",
	"pinterestbot", "redditbot", "mastodon", "signal", "viber", "yahoo! slurp", "duckduckbot", "petalbot",
}

func PageCSP(nonce string) string {
	return fmt.Sprintf(pageCSPFormat, nonce, nonce)
}

// SecurityHeaders lists the headers every response for path carries besides its Content-Security-Policy.
func SecurityHeaders(path string) map[string]string {
	headers := maps.Clone(securityHeaders)
	if !IsHomePath(path) {
		headers["X-Robots-Tag"] = robotsTag
	}
	return headers
}

func SetSecurityHeaders(header http.Header, indexable bool) {
	for name, value := range securityHeaders {
		header.Set(name, value)
	}
	if !indexable {
		header.Set("X-Robots-Tag", robotsTag)
	}
	header.Set("Content-Security-Policy", NonHTMLCSP)
}

func IsPreviewAgent(userAgent string) bool {
	userAgent = strings.ToLower(userAgent)
	return slices.ContainsFunc(previewAgents, func(agent string) bool { return strings.Contains(userAgent, agent) })
}

func AcceptsGzip(acceptEncoding string) bool {
	wildcard, named, gzipAccepted := false, false, false
	for part := range strings.SplitSeq(acceptEncoding, ",") {
		preference := parsePreference(part)
		switch preference.token {
		case "*":
			wildcard = preference.quality > 0
		case EncodingGzip:
			named, gzipAccepted = true, preference.quality > 0
		default:
		}
	}
	if !named {
		return wildcard
	}
	return gzipAccepted
}

type encodingPreference struct {
	token   string
	quality float64
}

func parsePreference(part string) encodingPreference {
	token, parameters, _ := strings.Cut(strings.TrimSpace(part), ";")
	preference := encodingPreference{token: strings.ToLower(strings.TrimSpace(token)), quality: 1}
	name, value, ok := strings.Cut(strings.TrimSpace(parameters), "=")
	if ok && strings.TrimSpace(name) == "q" {
		if quality, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
			preference.quality = quality
		}
	}
	return preference
}

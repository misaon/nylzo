package visits

import (
	"slices"
	"strings"
)

// Family groups the robots the admin area tells apart.
type Family string

const (
	FamilySearch   Family = "search"
	FamilyAI       Family = "ai"
	FamilyPreview  Family = "preview"
	FamilyMonitor  Family = "monitor"
	FamilyAudit    Family = "audit"
	FamilyScript   Family = "script"
	FamilyHeadless Family = "headless"
	FamilyFlood    Family = "flood"
	FamilyOther    Family = "other"
)

type agentFamily struct {
	family  Family
	needles []string
}

// The first family with a needle in the lowercased User-Agent wins, so the named robots come before the words
// that any robot may carry.
var agentFamilies = []agentFamily{
	{FamilyAI, []string{
		"gptbot", "chatgpt-user", "oai-searchbot", "claudebot", "claude-web", "claude-user", "claude-searchbot",
		"anthropic-ai", "perplexitybot", "perplexity-user", "ccbot", "bytespider", "amazonbot", "google-extended",
		"googleother", "meta-externalagent", "meta-externalfetcher", "cohere-ai", "mistralai-user", "diffbot",
		"youbot", "ai2bot", "applebot-extended", "duckassistbot", "timpibot", "imagesiftbot", "omgili",
	}},
	{FamilyPreview, []string{
		"slackbot", "slack-imgproxy", "twitterbot", "facebookexternalhit", "facebot", "whatsapp/", "telegrambot",
		"discordbot", "linkedinbot", "skypeuripreview", "embedly", "iframely", "pinterestbot", "redditbot",
		"mastodon", "bluesky", "cardyb", "snapchat url preview", "flipboardproxy", "tumblr/", "vkshare", "zoombot",
		"google-pagerenderer",
	}},
	{FamilySearch, []string{
		"googlebot", "google-inspectiontool", "storebot-google", "adsbot-google", "mediapartners-google",
		"feedfetcher-google", "bingbot", "bingpreview", "msnbot", "adidxbot", "seznambot", "duckduckbot",
		"duckduckgo-favicons-bot", "yandexbot", "yandeximages", "yandex.com/bots", "baiduspider", "applebot",
		"petalbot", "sogou", "exabot", "qwantbot", "qwantify", "mojeekbot", "yahoo! slurp", "naverbot", "yeti/",
		"coccocbot", "bravebot", "kagibot", "yacybot", "ia_archiver", "archive.org_bot",
	}},
	{FamilyMonitor, []string{
		"uptimerobot", "pingdom", "statuscake", "better uptime", "betterstack", "site24x7", "datadog", "newrelic",
		"zabbix", "nagios", "checkly", "gatus", "uptime-kuma", "hetrixtools", "updown.io", "freshping", "nodeping",
		"monitis", "catchpoint", "appdynamics", "dynatrace", "blackbox", "prometheus", "healthcheck", "nylzo-monitor",
	}},
	{FamilyAudit, []string{
		"ahrefsbot", "ahrefssiteaudit", "semrushbot", "siteauditbot", "mj12bot", "dotbot", "dataforseobot", "blexbot",
		"serpstatbot", "screaming frog", "rogerbot", "seokicks", "barkrowler", "megaindex", "seobility", "sistrix",
		"chrome-lighthouse", "pagespeed", "gtmetrix", "ptst/", "censys", "shodan", "zgrab", "nmap", "masscan",
		"nuclei", "nikto", "wpscan", "sqlmap", "internetmeasurement", "expanse", "netcraft", "securitytrails",
		"qualys", "ssllabs", "hardenize", "detectify", "w3c_validator", "validator.nu", "nessus", "httrack",
		"siteimprove", "google-site-verification", "google favicon", "google-ads",
	}},
	{FamilyHeadless, []string{
		"headlesschrome", "headless", "phantomjs", "puppeteer", "playwright", "selenium", "webdriver", "cypress",
		"slimerjs", "htmlunit", "jsdom",
	}},
	{FamilyOther, []string{
		"crawl", "spider", "slurp", "scrape", "+http", "archiver", "monitor", "preview", "validator", "scanner",
	}},
}

const (
	botWord = "bot"
	// A phone maker's name, which is the one place where bot follows these letters.
	botWordOfPhone = "cubot"
)

// RobotFamily tells which family of robots a User-Agent names itself as, if any. A client that does not claim to
// be a browser at all is a script.
func RobotFamily(userAgent string) (Family, bool) {
	lowered := strings.ToLower(userAgent)
	for _, known := range agentFamilies {
		named := func(needle string) bool { return strings.Contains(lowered, needle) }
		if slices.ContainsFunc(known.needles, named) {
			return known.family, true
		}
	}
	if strings.Contains(strings.ReplaceAll(lowered, botWordOfPhone, ""), botWord) {
		return FamilyOther, true
	}
	if !strings.HasPrefix(lowered, "mozilla/") && !strings.HasPrefix(lowered, "opera/") {
		return FamilyScript, true
	}
	return "", false
}

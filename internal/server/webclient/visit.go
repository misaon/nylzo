package webclient

import (
	"net/http"

	"github.com/misaon/nylzo/internal/core/api"
)

const (
	AdminPath           = "/admin"
	destinationDocument = "document"
)

var countedPages = map[string]string{
	sentPath:         api.PageSent,
	transparencyPath: api.PageTransparency,
	termsPath:        api.PageTerms,
	agentsPath:       api.PageAgents,
}

// DocumentVisit tells which counted page a request loads as a document, and whether a browser navigated to it.
// Only the pages the site answers with their content count: never a page that shows a message, the operator's
// own, a redirect, an address the site does not have, a request another site makes its visitor's browser send, or
// a page a browser loads ahead of a visit that may never come.
func DocumentVisit(r *http.Request) (api.Visit, bool) {
	if r.Method != http.MethodGet || fromAnotherSite(r) || r.Header.Get("Sec-Purpose") != "" {
		return api.Visit{}, false
	}
	page, counted := countedPages[r.URL.Path]
	if isHomePath(r.URL.Path) {
		page, counted = api.PageHome, true
	}
	if !counted {
		return api.Visit{}, false
	}
	navigation := r.Header.Get("Sec-Fetch-Mode") == "navigate" && r.Header.Get("Sec-Fetch-Dest") == destinationDocument
	return api.Visit{Signal: api.VisitDocument, Page: page, Navigation: navigation}, true
}

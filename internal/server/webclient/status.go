package webclient

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	serverErrorPage = "error"
	maintenancePage = "maintenance"
	unavailableBody = `{"error":"unavailable"}`

	MaintenanceRetry = 30 * time.Second
)

// WantsPage reports whether a browser navigates to the address, so a status page answers instead of a bare status.
func WantsPage(r *http.Request) bool {
	if destination := r.Header.Get("Sec-Fetch-Dest"); destination != "" {
		return destination == "document"
	}
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

// PreferredLanguage picks the language of a status page, which has no script to read the reader's saved choice.
func PreferredLanguage(acceptLanguage string) string {
	chosen, best := defaultLanguage, 0.0
	for part := range strings.SplitSeq(acceptLanguage, ",") {
		preference := parsePreference(part)
		if language, ok := matchLanguage(preference.token); ok && preference.quality > best {
			chosen, best = language, preference.quality
		}
	}
	return chosen
}

func (p Pages) ServeServerError(w http.ResponseWriter, r *http.Request, status int) {
	p.serveStatus(w, r, serverErrorPage, status)
}

func (p Pages) ServeMaintenance(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Retry-After", strconv.Itoa(int(MaintenanceRetry.Seconds())))
	p.serveStatus(w, r, maintenancePage, http.StatusServiceUnavailable)
}

func (p Pages) serveStatus(w http.ResponseWriter, r *http.Request, name string, status int) {
	SetSecurityHeaders(w.Header(), false)
	page, ok := p[name+"."+PreferredLanguage(r.Header.Get("Accept-Language"))+".html"]
	if !ok || !WantsPage(r) {
		w.WriteHeader(status)
		return
	}
	w.Header().Set("Vary", "Accept-Language")
	page.ServeStatus(w, r, status)
}

func WriteUnavailable(w http.ResponseWriter, retry time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(unavailableBody))
}

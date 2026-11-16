package webclient

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/misaon/nylzo/internal/core/api"
)

// Only a page the site answers with its content is reported, as the page it is; never a page that shows a
// message, the operator's page, a redirect, an address the site does not have, a request another site makes a
// browser send or a page loaded ahead of a visit.
func TestDocumentVisitReportsOnlyCountedPages(t *testing.T) {
	navigation := map[string]string{"Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "document"}
	with := func(extra map[string]string) map[string]string {
		headers := maps.Clone(navigation)
		maps.Copy(headers, extra)
		return headers
	}
	cases := []struct {
		name, method, path string
		headers            map[string]string
		page               string
		navigation         bool
	}{
		{"home", http.MethodGet, "/", navigation, api.PageHome, true},
		{"home in Czech", http.MethodGet, "/cs/", navigation, api.PageHome, true},
		{"terms without navigation", http.MethodGet, "/terms", nil, api.PageTerms, false},
		{
			"frame",
			http.MethodGet,
			"/sent",
			map[string]string{"Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "iframe"},
			api.PageSent,
			false,
		},
		{
			"cross-site navigation",
			http.MethodGet,
			"/agents",
			with(map[string]string{"Sec-Fetch-Site": "cross-site"}),
			api.PageAgents,
			true,
		},
		{"message", http.MethodGet, "/r", navigation, "", false},
		{"message by id", http.MethodGet, "/m/AAAAAAAAAAAAAAAAAAAAAA", navigation, "", false},
		{"admin", http.MethodGet, AdminPath, navigation, "", false},
		{"redirect", http.MethodGet, "/cs", navigation, "", false},
		{"unknown address", http.MethodGet, "/wp-login.php", navigation, "", false},
		{"file", http.MethodGet, "/robots.txt", navigation, "", false},
		{"post", http.MethodPost, "/", navigation, "", false},
		{
			"image on another site",
			http.MethodGet,
			"/",
			map[string]string{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Dest": "image"},
			"",
			false,
		},
		{"prefetch", http.MethodGet, "/", with(map[string]string{"Sec-Purpose": "prefetch"}), "", false},
	}
	for _, test := range cases {
		request := httptest.NewRequest(test.method, test.path, http.NoBody)
		for name, value := range test.headers {
			request.Header.Set(name, value)
		}
		visit, counted := DocumentVisit(request)
		if counted != (test.page != "") || visit.Page != test.page || visit.Navigation != test.navigation {
			t.Errorf("%s: %+v, counted %v", test.name, visit, counted)
		}
		if counted && visit.Signal != api.VisitDocument {
			t.Errorf("%s: signal %q", test.name, visit.Signal)
		}
	}
}

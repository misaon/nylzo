package webclient_test

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/misaon/nylzo/internal/server/webclient"
)

func TestPreferredLanguageFollowsTheReadersRanking(t *testing.T) {
	t.Parallel()
	for header, want := range map[string]string{
		"":                               "en",
		"de-DE,de;q=0.9":                 "de",
		"ru,sk;q=0.5":                    "sk",
		"pt-PT":                          "pt-BR",
		"zh-CN":                          "zh-Hans",
		"zh-TW,zh;q=0.5":                 "zh-Hans",
		"zh-Hant-HK":                     "en",
		"in-ID":                          "id",
		"cs":                             "cs",
		"cs-CZ,cs;q=0.9,en;q=0.8":        "cs",
		"en-US,en;q=0.9,cs;q=0.8":        "en",
		"nl,cs;q=0.5,en;q=0.4":           "cs",
		"en, cs":                         "en",
		"cs;q=0, en;q=0":                 "en",
		"CS-cz;q=0.7, EN;q=0.3, *;q=0.9": "cs",
	} {
		if got := webclient.PreferredLanguage(header); got != want {
			t.Errorf("PreferredLanguage(%q) = %q, want %q", header, got, want)
		}
	}
}

func TestStatusPagesAnswerOnlyNavigations(t *testing.T) {
	t.Parallel()
	files := builtClient(t)
	page := func(text string) *fstest.MapFile {
		return &fstest.MapFile{
			Data: []byte(`<!doctype html><html><head><style>body{}</style></head><body>` + text + `</body></html>`),
		}
	}
	maps.Copy(files, fstest.MapFS{
		"dist/error.en.html":       page("error en"),
		"dist/error.cs.html":       page("error cs"),
		"dist/maintenance.en.html": page("maintenance en"),
		"dist/maintenance.cs.html": page("maintenance cs"),
	})
	pages, err := load(t, files).Render("", "")
	if err != nil {
		t.Fatal(err)
	}
	bare, err := load(t, builtClient(t)).Render("", "")
	if err != nil {
		t.Fatal(err)
	}
	type answer struct {
		status int
		body   string
	}
	for name, test := range map[string]struct {
		serve   func(webclient.Pages, http.ResponseWriter, *http.Request)
		pages   webclient.Pages
		headers map[string]string
		want    answer
	}{
		"maintenance in Czech": {
			serve:   webclient.Pages.ServeMaintenance,
			pages:   pages,
			headers: map[string]string{"Sec-Fetch-Dest": "document", "Accept-Language": "cs,en;q=0.8"},
			want:    answer{http.StatusServiceUnavailable, "maintenance cs"},
		},
		"maintenance for a script": {
			serve:   webclient.Pages.ServeMaintenance,
			pages:   pages,
			headers: map[string]string{"Sec-Fetch-Dest": "empty", "Accept": "text/html"},
			want:    answer{http.StatusServiceUnavailable, ""},
		},
		"maintenance without the page in the build": {
			serve:   webclient.Pages.ServeMaintenance,
			pages:   bare,
			headers: map[string]string{"Sec-Fetch-Dest": "document"},
			want:    answer{http.StatusServiceUnavailable, ""},
		},
		"server error without fetch metadata": {
			serve: func(pages webclient.Pages, w http.ResponseWriter, r *http.Request) {
				pages.ServeServerError(w, r, http.StatusBadGateway)
			},
			pages:   pages,
			headers: map[string]string{"Accept": "text/html,application/xhtml+xml"},
			want:    answer{http.StatusBadGateway, "error en"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest(http.MethodGet, "/sent", http.NoBody)
			for header, value := range test.headers {
				request.Header.Set(header, value)
			}
			response := httptest.NewRecorder()
			test.serve(test.pages, response, request)
			body := response.Body.String()
			if response.Code != test.want.status || (test.want.body == "") != (body == "") ||
				test.want.body != "" &&
					body != `<!doctype html><html><head><style>body{}</style></head><body>`+test.want.body+`</body></html>` {
				t.Fatalf("answered %d %q, want %d %q", response.Code, body, test.want.status, test.want.body)
			}
			policy := webclient.NonHTMLCSP
			if body != "" {
				policy = webclient.StatusPolicy(body)
			}
			if response.Header().Get("Content-Security-Policy") != policy ||
				response.Header().Get("X-Robots-Tag") == "" ||
				response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status page headers: %v", response.Header())
			}
			if test.want.status == http.StatusServiceUnavailable && response.Header().Get("Retry-After") != "30" {
				t.Fatal("maintenance lost its Retry-After")
			}
		})
	}
}

func TestStatusPagePolicyIsTheOneOfTheSpecAndAdmitsOnlyItsGuard(t *testing.T) {
	t.Parallel()
	spec, err := os.ReadFile("../../../docs/wiki/reference/Protocol.md")
	if err != nil {
		t.Fatal(err)
	}
	const marker = "their own `Content-Security-Policy: "
	_, rest, found := strings.Cut(string(spec), marker)
	format, _, closed := strings.Cut(rest, "`")
	if !found || !closed {
		t.Fatal("Protocol section 9 lost the status page policy")
	}
	const guard = "history.replaceState(null,'','/')"
	files := builtClient(t)
	files["dist/error.en.html"] = &fstest.MapFile{
		Data: []byte(
			`<!doctype html><html><head><script>` + guard + `</script><style>body{}</style></head><body>error</body></html>`,
		),
	}
	pages, err := load(t, files).Render("", "")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/r", http.NoBody)
	request.Header.Set("Sec-Fetch-Dest", "document")
	response := httptest.NewRecorder()
	pages.ServeServerError(response, request, http.StatusBadGateway)
	want := strings.NewReplacer("{script hashes}", hashSource(guard), "{style hashes}", hashSource("body{}")).
		Replace(format)
	if got := response.Header().Get("Content-Security-Policy"); got != want {
		t.Fatalf("status page policy\n got %s\nwant %s", got, want)
	}
	if response.Header().Get("Connection-Allowlist") != webclient.ConnectionAllowlist("") {
		t.Fatal("the status page lost its Connection-Allowlist")
	}
}

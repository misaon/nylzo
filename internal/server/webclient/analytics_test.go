package webclient_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/misaon/nylzo/internal/server/webclient"
)

const analyticsPatterns = ` "https://static.cloudflareinsights.com/beacon.min.js?#" "https://cloudflareinsights.com/cdn-cgi/rum?#")`

func withAnalytics(allowlist string) string {
	return strings.TrimSuffix(allowlist, ")") + analyticsPatterns
}

func directives(policy string) map[string]string {
	parsed := map[string]string{}
	for directive := range strings.SplitSeq(policy, "; ") {
		name, value, _ := strings.Cut(directive, " ")
		parsed[name] = value
	}
	return parsed
}

func TestOnlyTheHomePagesOfNylzoComLoadWebAnalytics(t *testing.T) {
	t.Parallel()
	const contact = "security@example.org"
	build := load(t, builtClient(t))
	official, err := build.Render("https://nylzo.com", contact)
	if err != nil {
		t.Fatal(err)
	}
	denied := webclient.SecurityHeaders("/")["Permissions-Policy"]
	homes := 0
	for name, page := range official {
		policy := directives(page.Policy())
		if !strings.HasPrefix(name, "home.") {
			if strings.Contains(page.Policy(), "cloudflare") ||
				page.Allowlist() != webclient.ConnectionAllowlist(contact) ||
				page.SecurityHeaders("/sent")["Permissions-Policy"] != denied {
				t.Errorf("%s of nylzo.com loads analytics: %q %q", name, page.Policy(), page.Allowlist())
			}
			continue
		}
		homes++
		if !strings.HasSuffix(policy["script-src"], " https://static.cloudflareinsights.com/beacon.min.js") ||
			policy["connect-src"] != "'self' https://cloudflareinsights.com/cdn-cgi/rum" ||
			policy["trusted-types"] != "nylzo-kdf nylzo-analytics" {
			t.Errorf("%s policy %q", name, page.Policy())
		}
		if want := withAnalytics(webclient.ConnectionAllowlist(contact)); page.Allowlist() != want {
			t.Errorf("%s allowlist %q, want %q", name, page.Allowlist(), want)
		}
		if want := withAnalytics(webclient.ConnectionAllowlist("")); page.ContactlessAllowlist() != want {
			t.Errorf("%s without a contact %q, want %q", name, page.ContactlessAllowlist(), want)
		}
		response := httptest.NewRecorder()
		page.Serve(response, httptest.NewRequest(http.MethodGet, "/", http.NoBody))
		clientHints := strings.Replace(denied, "ch-ua-high-entropy-values=()", "ch-ua-high-entropy-values=(self)", 1)
		if response.Header().Get("Content-Security-Policy") != page.Policy() ||
			response.Header().Get("Connection-Allowlist") != page.Allowlist() ||
			response.Header().
				Get("Permissions-Policy") !=
				clientHints || page.SecurityHeaders("/")["Permissions-Policy"] != clientHints {
			t.Errorf("%s serves other headers: %v", name, response.Header())
		}
	}
	if homes < 12 {
		t.Fatalf("checked %d home pages, want every language", homes)
	}
	local, err := build.RenderLocal("https://nylzo.com", contact)
	if err != nil {
		t.Fatal(err)
	}
	other, err := build.Render("https://nylzo.example", contact)
	if err != nil {
		t.Fatal(err)
	}
	for client, pages := range map[string]webclient.Pages{"the local client": local, "another instance": other} {
		for name, page := range pages {
			if strings.Contains(page.Policy(), "cloudflare") || strings.Contains(page.Policy(), "nylzo-analytics") ||
				page.Allowlist() != webclient.ConnectionAllowlist(contact) ||
				page.SecurityHeaders("/")["Permissions-Policy"] != denied {
				t.Errorf("%s: %s loads analytics: %q %q", client, name, page.Policy(), page.Allowlist())
			}
		}
	}
}

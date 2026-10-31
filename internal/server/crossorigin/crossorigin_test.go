package crossorigin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBrowserMutationProtection(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		headers http.Header
		allowed bool
	}{
		{"same origin", http.Header{"Origin": {"https://nylzo.example"}, "X-Requested-With": {requestedWith}}, true},
		{"default port", http.Header{"Origin": {"https://nylzo.example:443"}, "X-Requested-With": {requestedWith}}, true},
		{"same origin metadata", http.Header{"Sec-Fetch-Site": {"same-origin"}, "X-Requested-With": {requestedWith}}, true},
		{"referer fallback", http.Header{"Referer": {"https://nylzo.example/m/id?query=ignored"}, "X-Requested-With": {requestedWith}}, true},
		{"native client", http.Header{"X-Requested-With": {requestedWith}}, true},
		{"legacy signed client", http.Header{"Authorization": {"Nylzo-Sig signed-request"}}, true},
		{"legacy create", http.Header{"X-Nylzo-Create": {"create-metadata"}}, true},
		{"missing marker", http.Header{"Origin": {"https://nylzo.example"}}, false},
		{"metadata without marker", http.Header{"Sec-Fetch-Site": {"same-origin"}}, false},
		{"cross origin", http.Header{"Origin": {"https://attacker.example"}, "X-Requested-With": {requestedWith}}, false},
		{"same site sibling", http.Header{"Origin": {"https://evil.nylzo.example"}, "X-Requested-With": {requestedWith}}, false},
		{"suffix match", http.Header{"Origin": {"https://nylzo.example.attacker.example"}, "X-Requested-With": {requestedWith}}, false},
		{"wrong scheme", http.Header{"Origin": {"http://nylzo.example"}, "X-Requested-With": {requestedWith}}, false},
		{"wrong port", http.Header{"Origin": {"https://nylzo.example:8443"}, "X-Requested-With": {requestedWith}}, false},
		{"opaque origin", http.Header{"Origin": {"null"}, "X-Requested-With": {requestedWith}}, false},
		{"origin path", http.Header{"Origin": {"https://nylzo.example/path"}, "X-Requested-With": {requestedWith}}, false},
		{"userinfo", http.Header{"Origin": {"https://attacker@nylzo.example"}, "X-Requested-With": {requestedWith}}, false},
		{"conflicting referer", http.Header{"Origin": {"https://nylzo.example"}, "Referer": {"https://attacker.example/"}, "X-Requested-With": {requestedWith}}, false},
		{"cross site metadata", http.Header{"Origin": {"https://nylzo.example"}, "Sec-Fetch-Site": {"cross-site"}, "X-Requested-With": {requestedWith}}, false},
		{"same site metadata", http.Header{"Sec-Fetch-Site": {"same-site"}, "X-Requested-With": {requestedWith}}, false},
		{"duplicate origin", http.Header{"Origin": {"https://nylzo.example", "https://nylzo.example"}, "X-Requested-With": {requestedWith}}, false},
		{"duplicate marker", http.Header{"X-Requested-With": {requestedWith, requestedWith}}, false},
		{"wrong marker", http.Header{"X-Requested-With": {"anything"}}, false},
		{"empty origin", http.Header{"Origin": {""}, "X-Requested-With": {requestedWith}}, false},
		{"forwarded spoof", http.Header{"Origin": {"https://attacker.example"}, "X-Forwarded-Host": {"attacker.example"}, "X-Requested-With": {requestedWith}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			refuse := http.HandlerFunc(
				func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) },
			)
			handler := Guard(
				"https://nylzo.example",
				refuse,
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Host != "untrusted-proxy.example" || r.Header.Get("Origin") != test.headers.Get("Origin") {
						t.Fatal("the guard changed the original request")
					}
					w.WriteHeader(http.StatusNoContent)
				}),
			)
			request := httptest.NewRequest(
				http.MethodPost,
				"https://untrusted-proxy.example/api/v1/messages",
				http.NoBody,
			)
			request.Header = test.headers.Clone()
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			want := http.StatusForbidden
			if test.allowed {
				want = http.StatusNoContent
			}
			if response.Code != want {
				t.Fatalf("status %d, want %d: %s", response.Code, want, response.Body.String())
			}
		})
	}
}

func TestUnconfiguredBrowserMutationTarget(t *testing.T) {
	t.Parallel()
	for _, host := range []string{"localhost:8080", "127.0.0.1:8080", "[::1]:8080", "rebind.attacker.example:8080"} {
		request := httptest.NewRequest(http.MethodPost, "http://"+host+"/api/v1/messages", http.NoBody)
		request.Header.Set("Origin", "http://"+host)
		request.Header.Set("X-Requested-With", requestedWith)
		allowed := validBrowserMutation(request, "", http.NewCrossOriginProtection())
		if allowed == strings.Contains(host, "attacker") {
			t.Fatalf("unconfigured browser target %s: allowed=%v", host, allowed)
		}
	}
}

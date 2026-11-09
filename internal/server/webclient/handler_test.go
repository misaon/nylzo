package webclient_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/misaon/nylzo/internal/buildinfo"
	"github.com/misaon/nylzo/internal/server/webclient"
)

func TestIndependentFrontendKeepsClientIntegrityAndReaderGuards(t *testing.T) {
	t.Parallel()
	build := load(t, builtClient(t))
	information := buildinfo.Info{Version: "0.4.0", Commit: strings.Repeat("a", 40)}
	handler, err := webclient.NewHandler(build, "https://nylzo.example", "", information)
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, userAgent, purpose string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, path, http.NoBody)
		request.Header.Set("User-Agent", userAgent)
		request.Header.Set("Sec-Purpose", purpose)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Header().Get("X-Content-Type-Options") != "nosniff" ||
			response.Header().Get("Cache-Control") != expectedCacheControl(path, response.Code) {
			t.Fatalf("%s lost its security headers: %v", path, response.Header())
		}
		return response
	}
	manifestResponse := request(http.MethodGet, webclient.ManifestPath, "", "")
	var manifest webclient.Manifest
	if decodeErr := json.Unmarshal(manifestResponse.Body.Bytes(), &manifest); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if manifest.Build != information || !manifest.Built {
		t.Fatalf("the frontend manifest lost its own release identity: %+v", manifest)
	}
	for _, file := range manifest.Files {
		response := request(http.MethodGet, file.Path, "", "")
		if response.Code != http.StatusOK || fmt.Sprintf("%x", sha256.Sum256(response.Body.Bytes())) != file.SHA256 {
			t.Fatalf("%s disagrees with its own manifest", file.Path)
		}
	}
	if response := request(
		http.MethodGet,
		"/r",
		"Slackbot",
		"",
	); response.Code != http.StatusOK || strings.Contains(response.Body.String(), "<script") ||
		!strings.Contains(response.Body.String(), `property="og:title"`) {
		t.Fatalf("link preview received executable application content: %s", response.Body.String())
	}
	if response := request(
		http.MethodGet,
		"/r",
		"",
		"prefetch",
	); response.Code != http.StatusServiceUnavailable ||
		response.Header().Get("Retry-After") != "60" {
		t.Fatal("the reader page accepted a prefetch")
	}
	for _, path := range []string{"/m/invalid", "/files.go", "/assets/../files.go", "/api/v1/instance", "/unknown"} {
		response := request(http.MethodGet, path, "", "")
		if response.Code == http.StatusOK || strings.Contains(response.Body.String(), "package web") {
			t.Fatalf("unexpected frontend resource %s: %d", path, response.Code)
		}
	}
	for _, fetch := range []struct {
		site, destination, path string
		status                  int
	}{
		{"cross-site", "script", manifest.Files[len(manifest.Files)-1].Path, http.StatusForbidden},
		{"same-site", "image", "/", http.StatusForbidden},
		{"cross-site", "iframe", "/r", http.StatusForbidden},
		{"cross-site", "document", "/r", http.StatusOK},
		{"same-origin", "script", manifest.Files[len(manifest.Files)-1].Path, http.StatusOK},
		{"none", "document", "/", http.StatusOK},
	} {
		probe := httptest.NewRequest(http.MethodGet, fetch.path, http.NoBody)
		probe.Header.Set("Sec-Fetch-Site", fetch.site)
		probe.Header.Set("Sec-Fetch-Dest", fetch.destination)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, probe)
		if response.Code != fetch.status ||
			response.Header().Get("Cache-Control") != expectedCacheControl(fetch.path, response.Code) {
			t.Fatalf("a %s %s request for %s answered %d", fetch.site, fetch.destination, fetch.path, response.Code)
		}
	}
	index := request(http.MethodGet, "/sent", "", "")
	for _, path := range []string{"/m/invalid", "/unknown", "/cs/sent"} {
		response := request(http.MethodGet, path, "", "")
		if response.Code != http.StatusNotFound || response.Body.String() != index.Body.String() ||
			response.Header().Get("Content-Security-Policy") != index.Header().Get("Content-Security-Policy") {
			t.Fatalf("%s did not answer the client page with 404: %d", path, response.Code)
		}
	}
	if response := request(http.MethodGet, "/assets/missing.js", "", ""); response.Code != http.StatusNotFound ||
		strings.Contains(response.Header().Get("Content-Type"), "html") {
		t.Fatal("a missing asset answered a page")
	}
	if response := request(http.MethodPost, "/r", "", ""); response.Code != http.StatusMethodNotAllowed {
		t.Fatal("the frontend accepted a mutation")
	}
}

// expectedCacheControl caches only scripts and stylesheets, which every page loads with Subresource Integrity (D119).
func expectedCacheControl(path string, status int) string {
	if status == http.StatusOK && strings.HasPrefix(path, "/assets/") &&
		(strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".css")) {
		return "max-age=31536000, immutable"
	}
	return "no-store"
}

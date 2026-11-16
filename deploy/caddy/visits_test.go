package main

import (
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core/api"
)

// isVisitReport tells the frontend's own report about a served page from a request it passed on for a client.
func isVisitReport(r *http.Request) bool {
	return r.Method == http.MethodPost && r.URL.Path == api.DocumentVisitsPath
}

type reportedVisit struct {
	body      string
	forwarded string
	userAgent string
	header    http.Header
}

// The frontend tells the backend which counted page it served and to whom, in a request of its own that carries
// nothing else of the page request; a page that shows a message, the admin page and files are never reported, and
// a backend that does not answer delays no page.
func TestFrontendReportsCountedPagesWithoutWaitingForTheBackend(t *testing.T) {
	var (
		mutex    sync.Mutex
		reported []reportedVisit
	)
	release := make(chan struct{})
	backend := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mutex.Lock()
		reported = append(reported, reportedVisit{
			body:      r.Method + " " + r.URL.RequestURI() + " " + string(body),
			forwarded: r.Header.Get("X-Forwarded-For"),
			userAgent: r.UserAgent(),
			header:    r.Header.Clone(),
		})
		mutex.Unlock()
		<-release
	}))
	defer backend.Close()
	defer close(release)
	configuration := filepath.Join("..", "compose", "Caddyfile")
	address, roots := startCaddy(t, configuration, backend.Listener.Addr().String())
	client := &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}},
		Timeout:   10 * time.Second,
	}
	load := func(path string) {
		t.Helper()
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://"+address+path, http.NoBody)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("User-Agent", "Mozilla/5.0 (reader)")
		request.Header.Set("Sec-Fetch-Mode", "navigate")
		request.Header.Set("Sec-Fetch-Dest", "document")
		request.Header.Set("Cookie", "kept=private")
		request.Header.Set("Referer", "https://elsewhere.example/page")
		request.Header.Set("Accept-Language", "cs")
		started := time.Now()
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if waited := time.Since(started); waited > 1500*time.Millisecond {
			t.Fatalf("%s waited %s for a backend that does not answer", path, waited)
		}
	}
	for _, path := range []string{"/r", "/m/AAAAAAAAAAAAAAAAAAAAAA", "/admin", "/robots.txt", "/assets/none.js", "/"} {
		load(path)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		mutex.Lock()
		count := len(reported)
		mutex.Unlock()
		if count > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if len(reported) != 1 {
		t.Fatalf("the backend got %d reports, want the home page alone: %+v", len(reported), reported)
	}
	report := reported[0]
	expected := "POST " + api.DocumentVisitsPath + ` {"signal":"document","page":"home","navigation":true}`
	if report.body != expected || report.forwarded != "127.0.0.1" || report.userAgent != "Mozilla/5.0 (reader)" {
		t.Fatalf("the report is %q from %q as %q", report.body, report.forwarded, report.userAgent)
	}
	for _, name := range []string{"Cookie", "Referer", "Accept-Language", "Sec-Fetch-Mode", "Authorization"} {
		if report.header.Get(name) != "" {
			t.Fatalf("the report carries the page request's %s", name)
		}
	}
}

// A client that names the address of the frontend's own reports gets the answer of an address the API does not
// have, and the frontend stops reporting for a while once the backend says that it counts no visits.
func TestFrontendKeepsClientsOffItsReportsAndPausesWhenNothingIsCounted(t *testing.T) {
	var (
		mutex   sync.Mutex
		reports int
		others  int
	)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mutex.Lock()
		defer mutex.Unlock()
		if !isVisitReport(r) {
			others++
			return
		}
		reports++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"counted":false}`)
	}))
	defer backend.Close()
	address, roots := startCaddy(t, filepath.Join("..", "compose", "Caddyfile"), backend.Listener.Addr().String())
	client := &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}},
		Timeout:   10 * time.Second,
	}
	forged, err := client.Post("https://"+address+api.DocumentVisitsPath, "application/json", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(forged.Body)
	_ = forged.Body.Close()
	if forged.StatusCode != http.StatusNotFound || string(body) != `{"error":"not_found"}` {
		t.Fatalf("a client's report got %d %s", forged.StatusCode, body)
	}
	counted := func() int {
		mutex.Lock()
		defer mutex.Unlock()
		return reports
	}
	page := func() {
		t.Helper()
		response, getErr := client.Get("https://" + address + "/")
		if getErr != nil {
			t.Fatal(getErr)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}
	page()
	for deadline := time.Now().Add(5 * time.Second); counted() == 0 && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	for range 5 {
		page()
	}
	time.Sleep(300 * time.Millisecond)
	mutex.Lock()
	defer mutex.Unlock()
	if reports != 1 || others != 0 {
		t.Fatalf("the backend got %d reports and %d other requests, want one report and no client's", reports, others)
	}
}

// A backend that breaks off its answer to a report makes the proxy panic; the frontend keeps serving pages.
func TestFrontendSurvivesAReportTheBackendBreaksOff(t *testing.T) {
	aborted := make(chan struct{}, 1)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isVisitReport(r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "16")
		_, _ = io.WriteString(w, `{"co`)
		controller := http.NewResponseController(w)
		_ = controller.Flush()
		if connection, _, err := controller.Hijack(); err == nil {
			_ = connection.Close()
		}
		select {
		case aborted <- struct{}{}:
		default:
		}
	}))
	defer backend.Close()
	address, roots := startCaddy(t, filepath.Join("..", "compose", "Caddyfile"), backend.Listener.Addr().String())
	client := &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}},
		Timeout:   10 * time.Second,
	}
	page := func() {
		t.Helper()
		response, err := client.Get("https://" + address + "/")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("the home page answers %d", response.StatusCode)
		}
	}
	page()
	select {
	case <-aborted:
	case <-time.After(5 * time.Second):
		t.Fatal("the backend got no report")
	}
	for deadline := time.Now().Add(500 * time.Millisecond); time.Now().Before(deadline); {
		page()
		time.Sleep(20 * time.Millisecond)
	}
}

// Nothing a client sends with a page request stops the reports of other visitors: neither a Host the frontend could
// not turn into an address nor a User-Agent longer than the backend reads.
func TestAClientCannotStopTheReports(t *testing.T) {
	const after = "Mozilla/5.0 (after)"
	reported := make(chan struct{}, 1)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isVisitReport(r) {
			return
		}
		if len(r.UserAgent()) > 1024 {
			w.WriteHeader(http.StatusRequestHeaderFieldsTooLarge)
			return
		}
		if r.UserAgent() == after {
			select {
			case reported <- struct{}{}:
			default:
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"counted":true}`)
	}))
	defer backend.Close()
	address, roots := startCaddy(t, filepath.Join("..", "compose", "Caddyfile"), backend.Listener.Addr().String())
	client := &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}},
		Timeout:   10 * time.Second,
	}
	page := func(host, userAgent string) {
		t.Helper()
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://"+address+"/", http.NoBody)
		if err != nil {
			t.Fatal(err)
		}
		if host != "" {
			request.Host = host
		}
		request.Header.Set("User-Agent", userAgent)
		request.Header.Set("Sec-Fetch-Mode", "navigate")
		request.Header.Set("Sec-Fetch-Dest", "document")
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}
	page("localhost:abc", "Mozilla/5.0 (host)")
	page("", "Mozilla/5.0 "+strings.Repeat("a", 19_000))
	page("", after)
	select {
	case <-reported:
	case <-time.After(5 * time.Second):
		t.Fatal("a page request of one client stopped the report of the next visitor")
	}
}

package server_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/misaon/nylzo/internal/core"
)

func TestAbuseReportsRecordOnlyValidReports(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
	h.mustCreate(m)
	send := func(token, body string) int {
		resp, _ := h.do(request{method: "POST", path: "/api/v1/abuse", ip: "203.0.113.70", body: []byte(body), token: token, headers: map[string]string{"Content-Type": "application/json"}})
		return resp.StatusCode
	}
	long := strings.Repeat("r", 3000)
	if send(m.readToken, fmt.Sprintf(`{"id":%q,"reason":%q}`, m.id.String(), long)) != 202 ||
		send(m.readToken, fmt.Sprintf(`{"id":%q}`, core.NewID().String())) != 202 ||
		send(core.Encode(make([]byte, 32)), fmt.Sprintf(`{"id":%q}`, m.id.String())) != 202 {
		t.Fatal("reports must answer 202")
	}
	if send("", `{"id":`) != 400 {
		t.Fatal("malformed report body")
	}
	data, err := h.abuseReports()
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], m.id.String()) || strings.Contains(lines[0], strings.Repeat("r", 1001)) {
		t.Fatalf("recorded reports: %d lines", len(lines))
	}
	if resp, meta := h.metadata(m); resp.StatusCode != 200 || meta["state"] != "available" {
		t.Fatal("abuse report changed the message")
	}
}

func TestAbuseReportHardening(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
	h.mustCreate(m)
	link := core.FormatLink("https://nylzo.example", m.id, m.kdf, m.secret)
	_, fragment, _ := strings.Cut(link, "#")
	body := fmt.Sprintf(`{"id":%q,"reason":%q}`, m.id.String(), "malware at "+link+" and again "+fragment)
	for _, ct := range []string{"", "text/plain", "application/x-www-form-urlencoded"} {
		resp, _ := h.do(request{method: "POST", path: "/api/v1/abuse", ip: "203.0.113.80", body: []byte(body), token: m.readToken, headers: map[string]string{"Content-Type": ct}})
		if resp.StatusCode != 400 {
			t.Fatalf("content type %q: %d", ct, resp.StatusCode)
		}
	}
	resp, _ := h.do(request{method: "POST", path: "/api/v1/abuse", ip: "203.0.113.81", body: []byte(body), token: m.readToken, headers: map[string]string{"Content-Type": "application/json; charset=utf-8"}})
	if resp.StatusCode != 202 {
		t.Fatalf("json report: %d", resp.StatusCode)
	}
	path := h.abuseFile(h.clock.Now())
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	secret := core.Encode(m.secret[:])
	if strings.Contains(string(data), secret) || !strings.Contains(string(data), "[redacted]") {
		t.Fatalf("the report kept the link secret: %s", data)
	}
	if err := os.Truncate(path, 16<<20); err != nil {
		t.Fatal(err)
	}
	resp, _ = h.do(request{method: "POST", path: "/api/v1/abuse", ip: "203.0.113.82", body: []byte(body), token: m.readToken, headers: map[string]string{"Content-Type": "application/json"}})
	info, err := os.Stat(path)
	if resp.StatusCode != 503 || err != nil || info.Size() != 16<<20 || !strings.Contains(h.logBuf.String(), "abuse-file-full") {
		t.Fatalf("a full report file must answer 503, log an error and not grow: %d %v %d", resp.StatusCode, err, info.Size())
	}
}

func TestAbuseReasonRedactionEdges(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
	h.mustCreate(m)
	secret := core.Encode(m.secret[:])
	link := core.FormatLink("https://nylzo.example", m.id, m.kdf, m.secret)
	reasons := []string{
		"see https://x.example/p#token-abc def",
		strings.Repeat("a ", 499) + "aé tail",
		"https://safelinks.example/?url=" + strings.NewReplacer(":", "%3A", "/", "%2F", "#", "%23").Replace(link),
		"double %2523" + secret[:30] + " and glued key_1.0." + secret,
		"bare " + secret + " end",
		strings.Repeat("<", 600),
		"see https%3A%2F%2Fx.example%2Fp%23token-abc def",
		"see p%2523token-abc def",
		"see p%252523token-abc def",
		"see p%25252523token-abc def",
		"a\u009bb\u202ec\u007fd\u2066e%E2%80%AEf\nnext",
	}
	for i, reason := range reasons {
		reported := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
		h.mustCreate(reported)
		body, _ := json.Marshal(map[string]string{"id": reported.id.String(), "reason": reason})
		resp, _ := h.do(request{method: "POST", path: "/api/v1/abuse", ip: fmt.Sprintf("203.0.%d.1", 90+i), body: body, token: reported.readToken, headers: map[string]string{"Content-Type": "application/json"}})
		if resp.StatusCode != 202 {
			t.Fatalf("report %d: %d", i, resp.StatusCode)
		}
	}
	data, err := h.abuseReports()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret[:22]) {
		t.Fatalf("a link secret reached the report file: %s", data)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	reason := func(i int) string {
		var row struct{ Reason string }
		if err := json.Unmarshal([]byte(lines[i]), &row); err != nil {
			t.Fatal(err)
		}
		return row.Reason
	}
	if got := reason(0); got != "see https://x.example/p[redacted] def" {
		t.Fatalf("hash run: %q", got)
	}
	if got := reason(6); got != "see https://x.example/p[redacted] def" {
		t.Fatalf("percent-encoded hash run: %q", got)
	}
	for i, want := range map[int]string{7: "see p[redacted] def", 8: "see p[redacted] def", 9: "see p%23token-abc def", 10: "a bc def next"} {
		if got := reason(i); got != want {
			t.Fatalf("reason %d: %q, want %q", i, got, want)
		}
	}
	if got := reason(1); len(got) != 999 || strings.ContainsRune(got, utf8.RuneError) {
		t.Fatalf("cut is not UTF-8 safe: %d", len(got))
	}
	for i, line := range lines {
		if len(line) > 1400 {
			t.Fatalf("line %d is %d bytes after escaping", i, len(line))
		}
	}
}

func TestAbuseReportsCappedPerMessage(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 2*86400, "abuse")
	other := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
	third := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
	h.mustCreate(m)
	h.mustCreate(other)
	h.mustCreate(third)
	report := func(msg *message, ip string) int {
		body := fmt.Sprintf(`{"id":%q,"reason":"spam"}`, msg.id.String())
		resp, _ := h.do(request{method: "POST", path: "/api/v1/abuse", ip: ip, body: []byte(body), token: msg.readToken, headers: map[string]string{"Content-Type": "application/json"}})
		return resp.StatusCode
	}
	stored := func() int {
		data, _ := h.abuseReports()
		return strings.Count(string(data), "\n")
	}
	for i := range 4 {
		if code := report(m, fmt.Sprintf("198.51.%d.1", 7+i)); code != 202 {
			t.Fatalf("report %d: %d", i, code)
		}
	}
	if n := stored(); n != 3 {
		t.Fatalf("one message stored %d reports from different ranges, want 3", n)
	}
	report(other, "198.51.7.9")
	if n := stored(); n != 4 {
		t.Fatalf("another message must keep its own budget: %d", n)
	}
	path := h.abuseFile(h.clock.Now())
	if err := os.Truncate(path, 16<<20); err != nil {
		t.Fatal(err)
	}
	if code := report(third, "192.0.2.1"); code != 503 {
		t.Fatalf("full file: %d", code)
	}
	if err := os.Truncate(path, 0); err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		report(third, fmt.Sprintf("192.0.2.%d", i+2))
	}
	if n := stored(); n != 3 {
		t.Fatalf("a report refused for a full file must not use up the message budget: %d", n)
	}
	h.clock.Advance(24 * time.Hour)
	report(m, "198.51.7.20")
	if n := stored(); n != 4 {
		t.Fatalf("the budget must reset the next UTC day: %d", n)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(h.dir, "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var tied int
	if err := db.QueryRow(`SELECT COUNT(*) FROM rate_counters WHERE key LIKE 'abuse:id:%:%'`).Scan(&tied); err != nil || tied != 0 {
		t.Fatalf("a counter ties a message to a reporter range: %d %v", tied, err)
	}
}

func TestOverBudgetReportWithAFullFileIsAccepted(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
	fresh := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
	h.mustCreate(m)
	h.mustCreate(fresh)
	report := func(msg *message, ip string) int {
		body, _ := json.Marshal(map[string]string{"id": msg.id.String(), "reason": "spam"})
		resp, _ := h.do(request{method: "POST", path: "/api/v1/abuse", ip: ip, body: body, token: msg.readToken, headers: map[string]string{"Content-Type": "application/json"}})
		return resp.StatusCode
	}
	for i := range 3 {
		if code := report(m, fmt.Sprintf("198.51.7.%d", i+1)); code != 202 {
			t.Fatalf("report %d: %d", i+1, code)
		}
	}
	if err := os.Truncate(h.abuseFile(h.clock.Now()), 16<<20); err != nil {
		t.Fatal(err)
	}
	if code := report(m, "198.51.7.9"); code != 202 {
		t.Fatalf("an over-budget report with a full file: %d", code)
	}
	if code := report(fresh, "198.51.8.1"); code != 503 {
		t.Fatalf("a report that would be stored in a full file: %d", code)
	}
}

func TestFormatCharactersCannotSplitARedactedToken(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
	h.mustCreate(m)
	secret := core.Encode(m.secret[:])
	split := secret[:15] + "\u200b" + secret[15:30] + "\u00ad" + secret[30:]
	body, _ := json.Marshal(map[string]string{"id": m.id.String(), "reason": "pasted " + split + " end"})
	if resp, _ := h.do(request{method: "POST", path: "/api/v1/abuse", ip: "203.0.113.7", body: body, token: m.readToken, headers: map[string]string{"Content-Type": "application/json"}}); resp.StatusCode != 202 {
		t.Fatalf("report: %d", resp.StatusCode)
	}
	data, err := h.abuseReports()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret[:15]) {
		t.Fatalf("a token split by format characters reached the report file: %s", data)
	}
}

func TestAbuseRedactsRunsFromTwentyTwoCharacters(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFNone, "", 1, 3600, "abuse")
	h.mustCreate(m)
	run21, run22 := strings.Repeat("a", 21), strings.Repeat("b", 22)
	body, _ := json.Marshal(map[string]string{"id": m.id.String(), "reason": run21 + " " + run22})
	if resp, _ := h.do(request{method: "POST", path: "/api/v1/abuse", ip: "203.0.113.9", body: body, token: m.readToken, headers: map[string]string{"Content-Type": "application/json"}}); resp.StatusCode != 202 {
		t.Fatalf("report: %d", resp.StatusCode)
	}
	data, err := h.abuseReports()
	if err != nil || !strings.Contains(string(data), `"`+run21+` [redacted]"`) {
		t.Fatalf("runs of 22 or more base64url characters must be redacted and shorter ones kept: %s %v", data, err)
	}
}

package server_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
)

func issuedID(t *testing.T, h *harness) (core.ID, string, time.Time) {
	t.Helper()
	_, body := h.do(request{method: "GET", path: "/api/v1/instance"})
	c, ok := decodeJSON(t, body)["create"].(map[string]any)
	if !ok {
		t.Fatalf("instance carries no create block: %s", body)
	}
	id, err := core.ParseID(c["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	expires, err := time.Parse(time.RFC3339, c["expires_at"].(string))
	if err != nil {
		t.Fatal(err)
	}
	return id, c["id_token"].(string), expires
}

func createWithToken(t *testing.T, h *harness, m *message, token any) (*http.Response, []byte) {
	t.Helper()
	header := mutateHeader(t, m, func(f map[string]any) {
		if token != nil {
			f["id_token"] = token
		}
	})
	return h.do(request{method: "POST", path: "/api/v1/messages", body: m.object, ip: m.ip,
		rawCreate: true, headers: map[string]string{"Content-Type": "application/octet-stream", "X-Nylzo-Create": header}})
}

func TestCreateNeedsAnIssuedIDToken(t *testing.T) {
	t.Parallel()
	h := newHarness(t, baseConfig(pepperA, nil))
	id, token, expires := issuedID(t, h)
	if want := h.clock.Now().Add(10 * time.Minute).Truncate(time.Second); !expires.Equal(want) {
		t.Fatalf("id token expires %v, want %v", expires, want)
	}
	if other, _, _ := issuedID(t, h); other == id {
		t.Fatal("two instance responses issued the same id")
	}
	m, err := buildMessageWithID(id, core.KDFNone, "", 1, 3600, "issued")
	if err != nil {
		t.Fatal(err)
	}
	stranger := newMessage(t, core.KDFNone, "", 1, 3600, "chosen by the client")
	for name, c := range map[string]struct {
		m     *message
		token any
	}{
		"missing":        {m, nil},
		"empty":          {m, ""},
		"malformed":      {m, "not-a-token"},
		"for another id": {stranger, token},
	} {
		if resp, body := createWithToken(t, h, c.m, c.token); resp.StatusCode != http.StatusForbidden || decodeJSON(t, body)["error"] != "id_token" {
			t.Fatalf("%s token: %d %s", name, resp.StatusCode, body)
		}
	}
	if resp, body := createWithToken(t, h, m, token); resp.StatusCode != http.StatusCreated {
		t.Fatalf("issued id: %d %s", resp.StatusCode, body)
	}
	if resp, _ := createWithToken(t, h, m, token); resp.StatusCode != http.StatusConflict {
		t.Fatalf("a second create with the same token: %d", resp.StatusCode)
	}
	if resp, body := createWithToken(t, h, m, nil); resp.StatusCode != http.StatusForbidden || decodeJSON(t, body)["error"] != "id_token" {
		t.Fatalf("a create without a token must not learn that the id exists: %d %s", resp.StatusCode, body)
	}
	late, lateToken, _ := issuedID(t, h)
	h.clock.Advance(10*time.Minute + 6*time.Second)
	lm, err := buildMessageWithID(late, core.KDFNone, "", 1, 3600, "late")
	if err != nil {
		t.Fatal(err)
	}
	if resp, body := createWithToken(t, h, lm, lateToken); resp.StatusCode != http.StatusForbidden || decodeJSON(t, body)["error"] != "id_token" {
		t.Fatalf("expired token: %d %s", resp.StatusCode, body)
	}
}

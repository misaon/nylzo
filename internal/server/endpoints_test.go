package server_test

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server"
)

func TestBurnAndCancelEndpoints(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	burn := newMessage(t, core.KDFNone, "", 1, 3600, "b")
	cancel := newMessage(t, core.KDFNone, "", 2, 3600, "c")
	h.mustCreate(burn)
	h.mustCreate(cancel)
	unknown, unknownBody := h.do(request{method: "POST", path: "/api/v1/messages/" + core.NewID().String() + "/burn", token: burn.readToken})
	if unknown.StatusCode != 404 {
		t.Fatalf("unknown id: %d", unknown.StatusCode)
	}
	for _, c := range []struct {
		m             *message
		action, token string
	}{{burn, "burn", burn.manageTok}, {cancel, "cancel", cancel.readToken}} {
		resp, body := h.post(c.m, c.action, c.token)
		if resp.StatusCode != 404 || string(body) != string(unknownBody) {
			t.Fatalf("%s with the token of the other purpose: %d %s", c.action, resp.StatusCode, body)
		}
		if !h.blobExists(c.m.id) {
			t.Fatalf("%s with a wrong token removed the blob", c.action)
		}
	}
	if resp, _ := h.post(burn, "burn", burn.readToken); resp.StatusCode != 204 || h.blobExists(burn.id) {
		t.Fatalf("burn: %d", resp.StatusCode)
	}
	if _, meta := h.metadata(burn); meta["state"] != "deleted" {
		t.Fatalf("metadata after burn: %v", meta)
	}
	if resp, _ := h.claim(cancel, "", ""); resp.StatusCode != 200 {
		t.Fatal("claim before cancel")
	}
	if resp, _ := h.post(cancel, "cancel", cancel.manageTok); resp.StatusCode != 204 || h.blobExists(cancel.id) {
		t.Fatalf("cancel: %d", resp.StatusCode)
	}
	if _, st := h.status(cancel); st["state"] != "cancelled" || st["first_opened_at"] == nil {
		t.Fatalf("status after cancel: %v", st)
	}
	resp, body := h.post(burn, "burn", burn.readToken)
	if resp.StatusCode != 410 || decodeJSON(t, body)["state"] != "deleted" {
		t.Fatalf("second burn: %d %s", resp.StatusCode, body)
	}
	if resp, _ := h.post(cancel, "cancel", cancel.manageTok); resp.StatusCode != 410 {
		t.Fatalf("second cancel: %d", resp.StatusCode)
	}
}

func TestCreateHeaderRules(t *testing.T) {
	cfg := server.Defaults()
	none := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	pw := newMessage(t, core.KDFPBKDF2, "pw", 1, 3600, "x")
	if !server.ParseCreateHeaderForTest(cfg, none.createHeader(0)) || !server.ParseCreateHeaderForTest(cfg, pw.createHeader(0)) {
		t.Fatal("valid headers rejected")
	}
	raw, _ := base64.RawURLEncoding.DecodeString(none.createHeader(0))
	cases := map[string]string{
		"unknown-key":           mutateHeader(t, none, func(f map[string]any) { f["extra"] = 1 }),
		"case-variant-key":      mutateHeader(t, none, func(f map[string]any) { f["ID"] = f["id"]; delete(f, "id") }),
		"verifier-with-kdf0":    mutateHeader(t, none, func(f map[string]any) { f["verifier_key"] = publicKey(core.NewSecret()) }),
		"no-verifier-with-kdf2": mutateHeader(t, pw, func(f map[string]any) { delete(f, "verifier_key") }),
		"opens-0":               mutateHeader(t, none, func(f map[string]any) { f["opens_max"] = 0 }),
		"ttl-59":                mutateHeader(t, none, func(f map[string]any) { f["ttl_s"] = 59 }),
		"kdf-3":                 mutateHeader(t, none, func(f map[string]any) { f["kdf_id"] = 3 }),
		"trailing-brace":        core.Encode(append(append([]byte(nil), raw...), '}')),
		"trailing-object":       core.Encode(append(append([]byte(nil), raw...), []byte(" {}")...)),
		"not-an-object":         core.Encode([]byte(`[1,2]`)),
	}
	for name, value := range cases {
		if server.ParseCreateHeaderForTest(cfg, value) {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestClaimBodyVariants(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFPBKDF2, "pw", 1, 3600, "x")
	h.mustCreate(m)
	path := "/api/v1/messages/" + m.id.String() + "/claim"
	for name, req := range map[string]request{
		"malformed-json": {method: "POST", path: path, body: []byte(`{"verifier":`), token: m.readToken},
		"short-verifier": {method: "POST", path: path, token: m.readToken, verifier: m.verifier[:42]},
	} {
		resp, _ := h.do(req)
		if resp.StatusCode != 400 {
			t.Fatalf("%s: %d", name, resp.StatusCode)
		}
	}
	if _, st := h.status(m); st["failed_passphrase_attempts"].(float64) != 0 || st["opens_used"].(float64) != 0 {
		t.Fatalf("malformed claim bodies changed the message: %v", st)
	}
	r, _ := http.NewRequest("POST", h.ts.URL+path, io.MultiReader(strings.NewReader(`{"verifier":"`+m.verifier+`"}`)))
	r.Header.Set("Authorization", core.SignRequest(&m.link.R, core.PurposeRead, "POST", path, h.srv.ChallengeForTest(), nil).Authorization())
	r.Header.Set("X-Forwarded-For", "203.0.113.4")
	resp, err := h.client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 400 || r.ContentLength != 0 {
		t.Fatalf("claim with a chunked body: %d (content length %d)", resp.StatusCode, r.ContentLength)
	}
	if _, st := h.status(m); st["opens_used"].(float64) != 0 {
		t.Fatalf("a chunked claim body spent an open: %v", st)
	}
}

func TestPassphraseBackoffIsSharedByEveryReader(t *testing.T) {
	h := newHarness(t, baseConfig(pepperA, nil))
	m := newMessage(t, core.KDFPBKDF2, "pw", 1, 3600, "x")
	h.mustCreate(m)
	wrong := core.Encode(make([]byte, 32))
	for i := 1; i <= 6; i++ {
		h.claim(m, wrong, fmt.Sprintf("203.0.113.%d", i))
	}
	resp, body := h.claim(m, m.verifier, "198.51.100.1")
	if resp.StatusCode != 403 || decodeJSON(t, body)["retry_after"].(float64) != 60 {
		t.Fatalf("the backoff must apply to every address, rounded up to the minute: %d %s", resp.StatusCode, body)
	}
}

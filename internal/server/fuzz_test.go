package server_test

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server"
)

func FuzzCreateHeader(f *testing.F) {
	m, err := buildMessage(core.KDFNone, "", 1, 3600, "seed")
	if err != nil {
		f.Fatal(err)
	}
	f.Add(m.createHeader(0))
	f.Add(core.Encode([]byte(`{"id":"` + m.id.String() + `"}`)))
	f.Add("")
	f.Add("not base64!")
	cfg := server.Defaults()
	allowed := map[string]bool{"id": true, "id_token": true, "read_key": true, "verifier_key": true, "manage_key": true, "kdf_id": true, "opens_max": true, "ttl_s": true, "size": true}
	f.Fuzz(func(t *testing.T, value string) {
		if !server.ParseCreateHeaderForTest(cfg, value) {
			return
		}
		data, err := base64.RawURLEncoding.Strict().DecodeString(value)
		if err != nil {
			t.Fatal("accepted non-canonical base64url")
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			t.Fatalf("accepted invalid JSON: %v", err)
		}
		for k := range fields {
			if !allowed[k] {
				t.Fatalf("accepted key %q", k)
			}
		}
		var h struct {
			KDFID       int     `json:"kdf_id"`
			VerifierKey *string `json:"verifier_key"`
			OpensMax    int     `json:"opens_max"`
			TTL         int64   `json:"ttl_s"`
		}
		json.Unmarshal(data, &h)
		if (h.KDFID != 0) != (h.VerifierKey != nil) {
			t.Fatal("verifier_key does not match kdf_id")
		}
		if h.OpensMax < 1 || h.OpensMax > cfg.Limits.MaxOpens || h.TTL < core.MinTTLSeconds || h.TTL > int64(cfg.Limits.MaxTTL.Seconds()) {
			t.Fatal("accepted a policy outside the limits")
		}
	})
}

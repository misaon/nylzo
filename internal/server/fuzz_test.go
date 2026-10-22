package server_test

import (
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
	f.Fuzz(func(t *testing.T, value string) {
		server.ParseCreateHeaderForTest(cfg, value)
	})
}

package cli

import (
	"bytes"
	"testing"

	"github.com/misaon/nylzo/internal/core"
)

func TestSealingAttachmentRespectsServerCompressionCapability(t *testing.T) {
	t.Parallel()
	file := bytes.Repeat([]byte{'A'}, 256*1024)
	message := content{meta: core.Meta{File: &core.FileMeta{Name: "rows.csv", Size: int64(len(file))}}, file: file}
	for _, supported := range []bool{false, true} {
		request, err := sealMessage(core.NewID(), core.NewSecret(), core.NewSecret(), core.KDFNone, "",
			core.Policy{OpensMax: 1, TTLSeconds: 3600}, &message, supported)
		if err != nil {
			t.Fatal(err)
		}
		want := 327680
		if supported {
			want = core.PadFile
		}
		if len(request.Object) != want {
			t.Fatalf("support %t: want %d stored bytes, got %d", supported, want, len(request.Object))
		}
	}
}

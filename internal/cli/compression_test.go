package cli

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/misaon/nylzo/internal/core/envelope"
)

// W2-06, F6: only the sender's --compress compresses, whatever the server says.
func TestF6AttachmentIsCompressedOnlyOnRequest(t *testing.T) {
	t.Parallel()
	instance := newTestServer(t, nil)
	var mutex sync.Mutex
	var stored []int64
	instance.hold = func(r *http.Request) bool {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/messages" {
			mutex.Lock()
			stored = append(stored, r.ContentLength)
			mutex.Unlock()
		}
		return false
	}
	source := filepath.Join(t.TempDir(), "rows.csv")
	if err := os.WriteFile(source, bytes.Repeat([]byte{'A'}, 256*1024), 0o600); err != nil {
		t.Fatal(err)
	}
	createText(t, instance, "", "--from-file", source)
	createText(t, instance, "", "--from-file", source, "--compress")
	mutex.Lock()
	defer mutex.Unlock()
	if want := []int64{327680, envelope.Pad}; !slices.Equal(stored, want) {
		t.Fatalf("stored %v bytes, want %v", stored, want)
	}
}

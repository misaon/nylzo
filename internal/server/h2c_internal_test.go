package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/store"
	"github.com/misaon/nylzo/internal/store/disk"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

func TestWaitingUploadDoesNotStallItsH2CConnection(t *testing.T) {
	dir := t.TempDir()
	st, err := sqlite.Open(filepath.Join(dir, "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	blobs, err := disk.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Defaults()
	cfg.DataDir = dir
	cfg.Pepper = bytes.Repeat([]byte{1}, 32)
	cfg.PoWConcurrency = 1
	cfg.PoWDifficulty = 1
	cfg.Rates.GlobalDailyCreates = 2
	s, err := New(cfg, st, blobs)
	if err != nil {
		t.Fatal(err)
	}
	defer func(d time.Duration) { powSlotWait = d }(powSlotWait)
	powSlotWait = 3 * time.Second
	d := dayWindow(time.Now())
	if _, err := st.RateHit(t.Context(), []store.RateHit{{Key: globalCreatesKey, WindowStart: d.start, PurgeAt: d.purgeAt()}}); err != nil {
		t.Fatal(err)
	}
	s.powSlots <- struct{}{}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hs := s.HTTPServer()
	go hs.Serve(ln)
	t.Cleanup(func() { hs.Close() })
	var protocols http.Protocols
	protocols.SetUnencryptedHTTP2(true)
	client := &http.Client{Transport: &http.Transport{Protocols: &protocols}}
	base := "http://" + ln.Addr().String()

	id := core.NewID()
	h := core.Hash([]byte("x"))
	size := int64(256 << 10)
	hdr, _ := json.Marshal(map[string]any{"id": id.String(), "read_hash": core.Encode(h[:]), "manage_hash": core.Encode(h[:]), "kdf_id": 0, "opens_max": 1, "ttl_s": 60, "size": size})
	done := make(chan struct{})
	go func() {
		defer close(done)
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, base+"/api/v1/messages", bytes.NewReader(make([]byte, size)))
		req.ContentLength = size
		req.Header.Set("X-Nylzo-Create", core.Encode(hdr))
		req.Header.Set("X-Nylzo-Pow", s.powChallenge(time.Now())+".0000000000000001")
		if resp, err := client.Do(req); err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	}()
	time.Sleep(300 * time.Millisecond)

	start := time.Now()
	req, _ := http.NewRequest(http.MethodPost, base+"/api/v1/abuse", strings.NewReader(`{"id":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if elapsed := time.Since(start); resp.ProtoMajor != 2 || elapsed > time.Second {
		t.Fatalf("a small request waited %v (%s) behind an upload that has not been read yet", elapsed, resp.Proto)
	}
	<-done
}

type deadlineWriter struct {
	http.ResponseWriter
	read, write time.Time
}

func (w *deadlineWriter) SetReadDeadline(t time.Time) error {
	w.read = t
	return nil
}

func (w *deadlineWriter) SetWriteDeadline(t time.Time) error {
	w.write = t
	return nil
}

func TestTransfersGetTimeForTheirSize(t *testing.T) {
	const size = 5373983
	want := time.Duration(size) * time.Second / store.MinTransferRate
	if want <= writeTimeout || store.LeaseFor(size) != want || store.LeaseFor(1000) != store.LeaseDuration {
		t.Fatalf("lease for %d bytes %v, small %v", size, store.LeaseFor(size), store.LeaseFor(1000))
	}
	s := powServer(t, 1)
	w := &deadlineWriter{ResponseWriter: httptest.NewRecorder()}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	start := time.Now()
	s.serveBlob(w, r, &store.Message{ID: "x"}, "", strings.NewReader("x"), size)
	if got := w.write.Sub(start); got < want || got > want+time.Second {
		t.Fatalf("a %d byte response may take %v, got a write deadline %v ahead", size, want, got)
	}

	cfg := Defaults()
	cfg.DataDir = t.TempDir()
	cfg.Pepper = bytes.Repeat([]byte{1}, 32)
	st, err := sqlite.Open(filepath.Join(cfg.DataDir, "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	blobs, err := disk.New(cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	cs, err := New(cfg, st, blobs)
	if err != nil {
		t.Fatal(err)
	}
	id := core.NewID()
	h := core.Hash([]byte("x"))
	hdr, _ := json.Marshal(map[string]any{"id": id.String(), "read_hash": core.Encode(h[:]), "manage_hash": core.Encode(h[:]), "kdf_id": 0, "opens_max": 1, "ttl_s": 60, "size": size})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/messages", strings.NewReader(""))
	req.ContentLength = size
	req.Header.Set("Content-Length", strconv.Itoa(size))
	req.Header.Set("X-Nylzo-Create", core.Encode(hdr))
	cw := &deadlineWriter{ResponseWriter: httptest.NewRecorder()}
	start = time.Now()
	cs.handleCreate(cw, req)
	if got := cw.read.Sub(start); got < want || got > want+time.Second {
		t.Fatalf("a %d byte upload may take %v, got a read deadline %v ahead", size, want, got)
	}
}

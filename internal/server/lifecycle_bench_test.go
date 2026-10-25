//go:build unix

package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/server/logging"
	"github.com/misaon/nylzo/internal/store"
	"github.com/misaon/nylzo/internal/store/disk"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

const (
	benchDatabase   = "nylzo.db"
	walHeaderLen    = 32
	walFrameHeader  = 24
	copyBufferBytes = 32 << 10
)

var errBenchStatus = errors.New("bench: unexpected status")

type benchContent struct {
	text int
	file int
}

func (c benchContent) String() string {
	if c.file > 0 {
		return fmt.Sprintf("file-%dKiB", c.file>>10)
	}
	return fmt.Sprintf("text-%dB", c.text)
}

func benchMix() []benchContent {
	textSizes := []int{
		120, 200, 300, 400, 500, 600, 800, 1000, 1200,
		1500, 1800, 2000, 2500, 3000, 3500, 4000, 6000, 10000,
	}
	fileSizes := []int{100 << 10, 500 << 10, 1 << 20, 5 << 20}
	mix := make([]benchContent, 0, 2*len(textSizes)+len(fileSizes))
	for range 2 {
		for _, size := range textSizes {
			mix = append(mix, benchContent{text: size})
		}
	}
	for _, size := range fileSizes {
		mix = append(mix, benchContent{file: size})
	}
	return mix
}

func sealBenchObjects(tb testing.TB, contents []benchContent) [][]byte {
	tb.Helper()
	objects := make([][]byte, len(contents))
	for i, content := range contents {
		payload := core.Payload{}
		if content.text > 0 {
			payload.Meta.Text = new(strings.Repeat("a", content.text))
		}
		if content.file > 0 {
			payload.File = make([]byte, content.file)
			payload.Meta.File = &core.FileMeta{Name: "bench.bin", Mime: octetStream, Size: int64(content.file)}
		}
		object, err := core.Seal(
			new([core.KeyLen]byte),
			core.NewHeader(core.KDFNone),
			core.NewID(),
			core.Policy{OpensMax: 1, TTLSeconds: 86400},
			payload,
		)
		if err != nil {
			tb.Fatal(err)
		}
		objects[i] = object
	}
	return objects
}

type benchServer struct {
	server    *Server
	handler   http.Handler
	clock     *fakeClock
	directory string
}

func newBenchServer(tb testing.TB, wrap func(store.Store) store.Store) *benchServer {
	tb.Helper()
	directory := tb.TempDir()
	database, err := sqlite.Open(tb.Context(), filepath.Join(directory, benchDatabase))
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { database.Close() })
	blobs, err := disk.New(directory)
	if err != nil {
		tb.Fatal(err)
	}
	var storage store.Store = database
	if wrap != nil {
		storage = wrap(database)
	}
	settings := baseSettings(pepperA, nil)
	settings.DataDir = directory
	settings.MinFreeBytes = 0
	settings.MaxStreams = 1024
	rates := &settings.Rates
	rates.CreatePerHour, rates.CreatePerDay, rates.CreateBytesPerDay = 1<<40, 1<<40, 1<<50
	rates.ReadPerHour, rates.GlobalDailyCreates = 1<<40, 1<<40
	clock := &fakeClock{now: time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)}
	server, err := New(settings, storage, blobs,
		WithClock(clock.Now),
		WithLogger(logging.New(io.Discard)),
		WithAfterFunc(func(time.Duration, func()) {}),
	)
	if err != nil {
		tb.Fatal(err)
	}
	return &benchServer{server: server, handler: server.Handler(), clock: clock, directory: directory}
}

type benchMessage struct {
	create *http.Request
	claim  *http.Request
	ack    *http.Request
}

func (s *benchServer) prepare(tb testing.TB, objects [][]byte, count int) []benchMessage {
	tb.Helper()
	messages := make([]benchMessage, count)
	for i := range messages {
		object := objects[i%len(objects)]
		id, readSeed := core.NewID(), core.NewSecret()
		token, _ := s.server.idTokens.Issue(id, s.clock.Now())
		header, err := json.Marshal(map[string]any{
			"id":         id.String(),
			"id_token":   token,
			"read_key":   publicKey(readSeed),
			"manage_key": publicKey(core.NewSecret()),
			"kdf_id":     0,
			"opens_max":  1,
			"ttl_s":      86400,
			"size":       len(object),
		})
		if err != nil {
			tb.Fatal(err)
		}
		address := fmt.Sprintf("10.%d.%d.1:1234", (i>>8)&0xff, i&0xff)
		create := httptest.NewRequest(http.MethodPost, "/api/v1/messages", bytes.NewReader(object))
		create.RemoteAddr = address
		create.Header.Set("Content-Type", octetStream)
		create.Header.Set("Content-Length", strconv.Itoa(len(object)))
		create.Header.Set(createHeaderName, core.Encode(header))
		path := "/api/v1/messages/" + id.String()
		messages[i] = benchMessage{
			create: create,
			claim:  s.signed(readSeed, path+"/claim", address),
			ack:    s.signed(readSeed, path+"/ack", address),
		}
	}
	return messages
}

func (s *benchServer) signed(seed *[core.SeedLen]byte, path, address string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, path, http.NoBody)
	r.RemoteAddr = address
	challenge, _ := s.server.challenges.Issue(s.clock.Now())
	signature := core.SignRequest(seed, core.PurposeRead, http.MethodPost, path, challenge, nil)
	r.Header.Set("Authorization", signature.Authorization())
	return r
}

func (s *benchServer) serve(r *http.Request, want int, buffer []byte) (http.Header, error) {
	response := &discardResponse{header: http.Header{}, buffer: buffer}
	s.handler.ServeHTTP(response, r)
	if response.status != want {
		return nil, fmt.Errorf("%w: %s %s answered %d", errBenchStatus, r.Method, r.URL.Path, response.status)
	}
	return response.header, nil
}

func (s *benchServer) create(message *benchMessage, buffer []byte) error {
	_, err := s.serve(message.create, http.StatusCreated, buffer)
	return err
}

func (s *benchServer) claimAndAck(message *benchMessage, buffer []byte) error {
	claimed, err := s.serve(message.claim, http.StatusOK, buffer)
	if err != nil {
		return err
	}
	message.ack.Header.Set(leaseHeaderName, claimed.Get(leaseHeaderName))
	_, err = s.serve(message.ack, http.StatusNoContent, buffer)
	return err
}

func (s *benchServer) lifecycle(message *benchMessage, buffer []byte) error {
	if err := s.create(message, buffer); err != nil {
		return err
	}
	return s.claimAndAck(message, buffer)
}

type discardResponse struct {
	header http.Header
	status int
	buffer []byte
}

type discardWriter struct{}

func (discardWriter) Write(data []byte) (int, error) {
	return len(data), nil
}

func (d *discardResponse) Header() http.Header {
	return d.header
}

func (d *discardResponse) WriteHeader(status int) {
	if d.status == 0 {
		d.status = status
	}
}

func (d *discardResponse) Write(data []byte) (int, error) {
	d.WriteHeader(http.StatusOK)
	return len(data), nil
}

func (d *discardResponse) ReadFrom(source io.Reader) (int64, error) {
	d.WriteHeader(http.StatusOK)
	return io.CopyBuffer(discardWriter{}, source, d.buffer)
}

func (*discardResponse) SetReadDeadline(time.Time) error {
	return nil
}

func (*discardResponse) SetWriteDeadline(time.Time) error {
	return nil
}

func processCPU() time.Duration {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return 0
	}
	return time.Duration(usage.Utime.Nano() + usage.Stime.Nano())
}

func maxResidentBytes() float64 {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return 0
	}
	if runtime.GOOS == "darwin" {
		return float64(usage.Maxrss)
	}
	return float64(usage.Maxrss) * 1024
}

func BenchmarkLifecycle(b *testing.B) {
	cases := []struct {
		name     string
		contents []benchContent
	}{
		{"text-300B", []benchContent{{text: 300}}},
		{"text-2000B", []benchContent{{text: 2000}}},
		{"text-10000B", []benchContent{{text: 10000}}},
		{"file-100KiB", []benchContent{{file: 100 << 10}}},
		{"file-1MiB", []benchContent{{file: 1 << 20}}},
		{"file-5MiB", []benchContent{{file: 5 << 20}}},
		{"mix", benchMix()},
	}
	for _, benchCase := range cases {
		objects := sealBenchObjects(b, benchCase.contents)
		b.Run(benchCase.name, func(b *testing.B) {
			server := newBenchServer(b, nil)
			messages := server.prepare(b, objects, b.N)
			buffer := make([]byte, copyBufferBytes)
			b.ReportAllocs()
			b.ResetTimer()
			start := processCPU()
			for i := range b.N {
				if err := server.lifecycle(&messages[i], buffer); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(processCPU()-start)/float64(b.N), "cpu-ns/op")
		})
	}
}

func BenchmarkLifecycleParallel(b *testing.B) {
	objects := sealBenchObjects(b, benchMix())
	server := newBenchServer(b, nil)
	messages := server.prepare(b, objects, b.N)
	var next atomic.Int64
	runtime.GC()
	samples := []metrics.Sample{
		{Name: "/memory/classes/heap/objects:bytes"},
		{Name: "/memory/classes/total:bytes"},
		{Name: "/gc/heap/live:bytes"},
	}
	metrics.Read(samples)
	startHeap, startTotal, startLive := samples[0].Value.Uint64(), samples[1].Value.Uint64(), samples[2].Value.Uint64()
	stop := make(chan struct{})
	peaks := samplePeaks(samples, stop)
	b.ReportAllocs()
	b.ResetTimer()
	start := processCPU()
	b.RunParallel(func(pb *testing.PB) {
		buffer := make([]byte, copyBufferBytes)
		for pb.Next() {
			if err := server.lifecycle(&messages[next.Add(1)-1], buffer); err != nil {
				b.Error(err)
				return
			}
		}
	})
	b.StopTimer()
	close(stop)
	peak := <-peaks
	b.ReportMetric(float64(processCPU()-start)/float64(b.N), "cpu-ns/op")
	b.ReportMetric(float64(peak[0]-min(peak[0], startHeap)), "peak-heap-B")
	b.ReportMetric(float64(peak[1]-min(peak[1], startTotal)), "peak-runtime-B")
	b.ReportMetric(float64(peak[2]-min(peak[2], startLive)), "peak-live-heap-B")
	b.ReportMetric(maxResidentBytes(), "maxrss-B")
}

func samplePeaks(samples []metrics.Sample, stop <-chan struct{}) <-chan []uint64 {
	result := make(chan []uint64, 1)
	go func() {
		peaks := make([]uint64, len(samples))
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			metrics.Read(samples)
			for i := range samples {
				peaks[i] = max(peaks[i], samples[i].Value.Uint64())
			}
			select {
			case <-stop:
				result <- peaks
				return
			case <-ticker.C:
			}
		}
	}()
	return result
}

type walWrites struct {
	store.Store

	path         string
	frames       atomic.Int64
	transactions atomic.Int64
}

func (w *walWrites) Create(ctx context.Context, row *store.Row, counters ...store.RateHit) error {
	return w.count(w.Store.Create(ctx, row, counters...))
}

func (w *walWrites) Update(ctx context.Context, locator seal.Locator, change func(row *store.Row) error) error {
	return w.count(w.Store.Update(ctx, locator, change))
}

func (w *walWrites) ShredBlobs(ctx context.Context, due []store.DueBlob) error {
	return w.count(w.Store.ShredBlobs(ctx, due))
}

func (w *walWrites) ForgetBlobs(ctx context.Context, due []store.DueBlob) error {
	return w.count(w.Store.ForgetBlobs(ctx, due))
}

func (w *walWrites) RateHit(ctx context.Context, hits []store.RateHit) ([]store.RateCount, error) {
	counts, err := w.Store.RateHit(ctx, hits)
	return counts, w.count(err)
}

func (w *walWrites) RateReserve(ctx context.Context, hit store.RateHit) (int64, error) {
	count, err := w.Store.RateReserve(ctx, hit)
	return count, w.count(err)
}

func (w *walWrites) RateReserveBatch(ctx context.Context, hits []store.RateHit) ([]bool, error) {
	reserved, err := w.Store.RateReserveBatch(ctx, hits)
	return reserved, w.count(err)
}

func (w *walWrites) RateRelease(ctx context.Context, key string, windowStart time.Time) error {
	return w.count(w.Store.RateRelease(ctx, key, windowStart))
}

func (w *walWrites) PurgeRows(ctx context.Context, now time.Time, limit int) (int, error) {
	purged, err := w.Store.PurgeRows(ctx, now, limit)
	return purged, w.count(err)
}

func (w *walWrites) RatePurge(ctx context.Context, now time.Time, limit int) (int, error) {
	purged, err := w.Store.RatePurge(ctx, now, limit)
	return purged, w.count(err)
}

func (w *walWrites) count(err error) error {
	info, statErr := os.Stat(w.path)
	if statErr == nil && info.Size() > walHeaderLen {
		w.frames.Add((info.Size() - walHeaderLen) / (walFrameHeader + 4096))
		w.transactions.Add(1)
	}
	return err
}

type writeCount struct {
	frames       float64
	transactions float64
}

func (w *walWrites) during(tb testing.TB, step func() error) writeCount {
	tb.Helper()
	frames, transactions := w.frames.Load(), w.transactions.Load()
	if err := step(); err != nil {
		tb.Fatal(err)
	}
	return writeCount{
		frames:       float64(w.frames.Load() - frames),
		transactions: float64(w.transactions.Load() - transactions),
	}
}

type footprint struct {
	databaseBytes int64
	walBytes      int64
	pages         int64
	freePages     int64
	blobBlocks    int64
	blobBytes     int64
	blobs         int64
}

func (s *benchServer) footprint(tb testing.TB) footprint {
	tb.Helper()
	var measured footprint
	database := filepath.Join(s.directory, benchDatabase)
	if info, err := os.Stat(database + "-wal"); err == nil {
		measured.walBytes = info.Size()
	}
	header := make([]byte, 100)
	file, err := os.Open(database)
	if err == nil {
		_, err = io.ReadFull(file, header)
		file.Close()
	}
	if err != nil {
		tb.Fatal(err)
	}
	measured.pages = int64(binary.BigEndian.Uint32(header[28:]))
	measured.databaseBytes = measured.pages * pageSize(header)
	measured.freePages = int64(binary.BigEndian.Uint32(header[36:]))
	entries, err := os.ReadDir(filepath.Join(s.directory, disk.Directory))
	if err != nil {
		tb.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(s.directory, disk.Directory, entry.Name())
		measured.blobBlocks += allocated(tb, path)
		info, infoErr := os.Stat(path)
		if infoErr != nil {
			tb.Fatal(infoErr)
		}
		measured.blobBytes += info.Size()
		measured.blobs++
	}
	return measured
}

func pageSize(header []byte) int64 {
	if size := int64(binary.BigEndian.Uint16(header[16:])); size != 1 {
		return size
	}
	return 1 << 16
}

func allocated(tb testing.TB, path string) int64 {
	tb.Helper()
	info, err := os.Stat(path)
	if err != nil {
		tb.Fatal(err)
	}
	status, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		tb.Fatal("no file system block count")
	}
	return status.Blocks * 512
}

func BenchmarkStorageFootprint(b *testing.B) {
	objects := sealBenchObjects(b, benchMix())
	writes := &walWrites{}
	server := newBenchServer(b, func(storage store.Store) store.Store {
		writes.Store = storage
		return writes
	})
	writes.path = filepath.Join(server.directory, benchDatabase+"-wal")
	buffer := make([]byte, copyBufferBytes)
	empty := server.footprint(b)
	count := float64(b.N)
	var objectBytes int64
	for i := range b.N {
		objectBytes += int64(len(objects[i%len(objects)]))
	}
	b.ResetTimer()
	var live, swept footprint
	for round := range 2 {
		messages := server.prepare(b, objects, b.N)
		created := writes.during(b, func() error {
			for i := range messages {
				if err := server.create(&messages[i], buffer); err != nil {
					return err
				}
			}
			return nil
		})
		createdFootprint := server.footprint(b)
		read := writes.during(b, func() error {
			for i := range messages {
				if err := server.claimAndAck(&messages[i], buffer); err != nil {
					return err
				}
			}
			return nil
		})
		server.clock.Advance(store.TombstoneTTL + 2*time.Hour)
		sweep := writes.during(b, func() error {
			_, err := server.server.SweepOnce(b.Context())
			return err
		})
		if round == 1 {
			b.ReportMetric(float64(createdFootprint.databaseBytes-live.databaseBytes), "db-growth-round2-B")
			continue
		}
		live, swept = createdFootprint, server.footprint(b)
		b.ReportMetric(created.frames/count, "create-pages/msg")
		b.ReportMetric(created.transactions/count, "create-txns/msg")
		b.ReportMetric(read.frames/count, "claim+ack-pages/msg")
		b.ReportMetric(read.transactions/count, "claim+ack-txns/msg")
		b.ReportMetric(sweep.frames/count, "sweep-pages/msg")
	}
	b.StopTimer()
	b.ReportMetric(float64(live.databaseBytes-empty.databaseBytes)/count, "db-B/msg")
	b.ReportMetric(float64(live.blobBlocks)/count, "blob-disk-B/msg")
	b.ReportMetric(float64(live.blobBytes)/count, "blob-len-B/msg")
	b.ReportMetric(float64(objectBytes)/count, "object-B/msg")
	b.ReportMetric(float64(live.walBytes), "wal-live-B")
	b.ReportMetric(float64(swept.freePages), "free-pages-swept")
	b.ReportMetric(float64(swept.pages), "pages-swept")
	b.ReportMetric(float64(swept.walBytes), "wal-swept-B")
}

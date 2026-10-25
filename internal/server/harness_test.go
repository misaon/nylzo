package server

import (
	"bytes"
	"cmp"
	"context"
	"crypto/hpke"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/server/abuse"
	"github.com/misaon/nylzo/internal/server/config"
	"github.com/misaon/nylzo/internal/server/diskspace"
	"github.com/misaon/nylzo/internal/store"
	"github.com/misaon/nylzo/internal/store/disk"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

var (
	pepperA     = bytes.Repeat([]byte{0x11}, 32)
	pepperB     = bytes.Repeat([]byte{0x22}, 32)
	errInjected = errors.New("test: injected storage failure")
)

var testReportKey = func() hpke.PrivateKey {
	key, err := abuse.NewKeyPair()
	if err != nil {
		panic(err)
	}
	return key
}()

type fakeClock struct {
	mutex sync.Mutex
	now   time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.now
}

func (c *fakeClock) Advance(step time.Duration) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.now = c.now.Add(step)
}

func (c *fakeClock) Set(now time.Time) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.now = now
}

type syncBuffer struct {
	mutex  sync.Mutex
	buffer bytes.Buffer
}

func (b *syncBuffer) Write(data []byte) (int, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.buffer.Write(data)
}

func (b *syncBuffer) String() string {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.buffer.String()
}

type pendingTimer struct {
	due time.Time
	run func()
}

// testHarness runs the whole handler with a fake clock and lease timers that fire only when asked.
type testHarness struct {
	t          *testing.T
	settings   config.Server
	store      store.Store
	blobs      store.Blobs
	clock      *fakeClock
	logs       *syncBuffer
	server     *Server
	httpServer *httptest.Server
	client     *http.Client
	directory  string
	timersMu   sync.Mutex
	timers     []pendingTimer
	captureMu  sync.Mutex
	lastCreate []byte
}

type responseBody []byte

type jsonFields map[string]any

type testResponse struct {
	StatusCode int
	Header     http.Header
	Close      bool
}

type testRequest struct {
	method    string
	path      string
	body      []byte
	headers   map[string]string
	address   string
	token     string
	verifier  string
	rawCreate bool
}

func baseSettings(pepper, previous []byte) *config.Server {
	settings := config.Defaults()
	settings.ReportKey = testReportKey.PublicKey()
	settings.Pepper = pepper
	settings.PepperPrevious = previous
	settings.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")}
	settings.Rates.BucketPerSecond = 1 << 20
	settings.Rates.BucketBurst = 1 << 20
	settings.Rates.SourcePerSecond = 1 << 20
	settings.Rates.SourceBurst = 1 << 20
	return &settings
}

func newHarness(t *testing.T, settings *config.Server) *testHarness {
	t.Helper()
	directory := t.TempDir()
	settings.DataDir = directory
	database, err := sqlite.Open(t.Context(), filepath.Join(directory, "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	blobs, err := disk.New(directory)
	if err != nil {
		t.Fatal(err)
	}
	harness := &testHarness{
		t:         t,
		settings:  *settings,
		store:     database,
		blobs:     blobs,
		directory: directory,
		logs:      &syncBuffer{},
	}
	harness.clock = &fakeClock{now: time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)}
	harness.open()
	return harness
}

func (h *testHarness) open() {
	h.t.Helper()
	server, err := New(&h.settings, h.store, h.blobs,
		WithClock(h.clock.Now),
		WithLogger(slog.New(slog.NewJSONHandler(h.logs, nil))),
		WithAfterFunc(func(delay time.Duration, run func()) {
			h.timersMu.Lock()
			defer h.timersMu.Unlock()
			h.timers = append(h.timers, pendingTimer{due: h.clock.Now().Add(delay), run: run})
		}),
	)
	if err != nil {
		h.t.Fatal(err)
	}
	h.server = server
	handler := server.Handler()
	h.httpServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/messages" {
			h.capture(r)
		}
		handler.ServeHTTP(w, r)
	}))
	h.t.Cleanup(h.httpServer.Close)
	h.client = h.httpServer.Client()
}

func (h *testHarness) capture(r *http.Request) {
	var dump bytes.Buffer
	fmt.Fprintf(&dump, "%s %s\n", r.Method, r.URL.RequestURI())
	r.Header.Write(&dump)
	body, _ := io.ReadAll(r.Body)
	dump.Write(body)
	r.Body = io.NopCloser(bytes.NewReader(body))
	h.captureMu.Lock()
	defer h.captureMu.Unlock()
	h.lastCreate = dump.Bytes()
}

func (h *testHarness) reopen(settings *config.Server) {
	h.httpServer.Close()
	settings.DataDir = h.directory
	h.settings = *settings
	h.open()
}

func (h *testHarness) listen() string {
	h.t.Helper()
	httpServer := h.server.HTTPServer()
	listener, err := new(net.ListenConfig).Listen(h.t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		h.t.Fatal(err)
	}
	go httpServer.Serve(listener)
	h.t.Cleanup(func() { httpServer.Close() })
	return listener.Addr().String()
}

func (h *testHarness) challenge() [core.ChallengeLen]byte {
	return h.challengeAt(h.clock.Now())
}

func (h *testHarness) challengeAt(now time.Time) [core.ChallengeLen]byte {
	challenge, _ := h.server.challenges.Issue(now)
	return challenge
}

func (h *testHarness) idToken(id core.ID) string {
	token, _ := h.server.idTokens.Issue(id, h.clock.Now())
	return token
}

func (h *testHarness) setFreeSpace(free diskspace.FreeFunc) {
	h.server.disk = diskspace.NewReserver(h.settings.DataDir, h.settings.MinFreeBytes, free, h.server.logger)
}

func (h *testHarness) fireTimers() int {
	h.timersMu.Lock()
	now := h.clock.Now()
	var due []func()
	var waiting []pendingTimer
	for _, timer := range h.timers {
		if timer.due.After(now) {
			waiting = append(waiting, timer)
		} else {
			due = append(due, timer.run)
		}
	}
	h.timers = waiting
	h.timersMu.Unlock()
	for _, run := range due {
		run()
	}
	return len(due)
}

func (h *testHarness) pendingTimers() int {
	h.timersMu.Lock()
	defer h.timersMu.Unlock()
	return len(h.timers)
}

func (h *testHarness) do(request *testRequest) (testResponse, responseBody) {
	h.t.Helper()
	response, body, err := h.tryDo(request)
	if err != nil {
		h.t.Fatal(err)
	}
	return response, body
}

func (h *testHarness) tryDo(request *testRequest) (testResponse, responseBody, error) {
	var body io.Reader = http.NoBody
	if request.body != nil {
		body = bytes.NewReader(request.body)
	}
	r, err := http.NewRequestWithContext(h.t.Context(), request.method, h.httpServer.URL+request.path, body)
	if err != nil {
		return testResponse{}, nil, err
	}
	for name, value := range request.headers {
		r.Header.Set(name, value)
	}
	if request.address != "" {
		r.Header.Set("X-Forwarded-For", request.address)
	}
	if request.method == http.MethodPost && request.path == "/api/v1/messages" && !request.rawCreate {
		if header := r.Header.Get("X-Nylzo-Create"); header != "" {
			r.Header.Set("X-Nylzo-Create", h.withIDToken(header))
		}
	}
	if request.token != "" {
		h.sign(r, request.token, request.verifier, request.body)
	}
	response, err := h.client.Do(r)
	if err != nil {
		return testResponse{}, nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return testResponse{}, nil, err
	}
	if response.StatusCode == http.StatusInternalServerError {
		logs := h.logs.String()
		h.t.Logf("server log tail after 500: %s", logs[max(0, len(logs)-2000):])
	}
	return testResponse{StatusCode: response.StatusCode, Header: response.Header, Close: response.Close}, data, nil
}

func (h *testHarness) statusFrom(request *testRequest) int {
	response, _, err := h.tryDo(request)
	if err != nil {
		h.t.Error(err)
		return 0
	}
	return response.StatusCode
}

func (h *testHarness) lastCreateRequest() []byte {
	h.captureMu.Lock()
	defer h.captureMu.Unlock()
	return slices.Clone(h.lastCreate)
}

func (h *testHarness) create(message *testMessage) (testResponse, responseBody) {
	h.t.Helper()
	response, body := h.do(createRequestFor(message))
	if response.StatusCode == http.StatusCreated {
		h.blobRef(message)
	}
	return response, body
}

func (h *testHarness) mustCreate(message *testMessage) {
	h.t.Helper()
	response, body := h.create(message)
	if response.StatusCode != http.StatusCreated {
		h.t.Fatalf("create: %d %s", response.StatusCode, body)
	}
}

func (h *testHarness) metadata(message *testMessage) (testResponse, jsonFields) {
	h.t.Helper()
	response, body := h.do(&testRequest{
		method:  http.MethodGet,
		path:    "/api/v1/messages/" + message.id.String(),
		address: message.address,
		token:   message.readToken,
	})
	return response, decodeJSON(h.t, body)
}

func (h *testHarness) claim(message *testMessage, verifier, address string) (testResponse, responseBody) {
	h.t.Helper()
	return h.do(claimRequestFor(message, verifier, address))
}

func (h *testHarness) blob(message *testMessage, lease string) (testResponse, responseBody) {
	h.t.Helper()
	return h.do(&testRequest{
		method:  http.MethodGet,
		path:    "/api/v1/messages/" + message.id.String() + "/blob",
		address: message.address,
		token:   message.readToken,
		headers: map[string]string{"X-Nylzo-Lease": lease},
	})
}

func (h *testHarness) ack(message *testMessage, lease string) testResponse {
	h.t.Helper()
	response, _ := h.do(&testRequest{
		method:  http.MethodPost,
		path:    "/api/v1/messages/" + message.id.String() + "/ack",
		address: message.address,
		token:   message.readToken,
		headers: map[string]string{"X-Nylzo-Lease": lease},
	})
	return response
}

func (h *testHarness) status(message *testMessage) (testResponse, jsonFields) {
	h.t.Helper()
	response, body := h.do(&testRequest{
		method:  http.MethodGet,
		path:    "/api/v1/messages/" + message.id.String() + "/status",
		address: message.address,
		token:   message.manageToken,
	})
	return response, decodeJSON(h.t, body)
}

func (h *testHarness) post(message *testMessage, action, token string) (testResponse, responseBody) {
	h.t.Helper()
	return h.do(&testRequest{
		method: http.MethodPost,
		path:   "/api/v1/messages/" + message.id.String() + "/" + action,
		token:  token,
	})
}

// blobExists checks the file under the blob reference the message got at creation.
func (h *testHarness) blobExists(message *testMessage) bool {
	h.t.Helper()
	ref := h.blobRef(message)
	if ref == "" {
		return false
	}
	blob, _, err := h.blobs.Open(h.t.Context(), ref)
	if err != nil {
		return false
	}
	blob.Close()
	return true
}

func (h *testHarness) storedBlobs() int {
	h.t.Helper()
	objects, err := h.blobs.List(h.t.Context())
	if err != nil {
		h.t.Fatal(err)
	}
	return len(objects)
}

func (h *testHarness) blobRef(message *testMessage) string {
	if message.blobRef == "" {
		if record, err := h.record(message); err == nil {
			message.blobRef = record.Row.BlobRef
		}
	}
	return message.blobRef
}

// record opens the sealed row with the read key, as an authorized request would.
func (h *testHarness) record(message *testMessage) (store.Record, error) {
	return h.recordFrom(h.store, message)
}

func (h *testHarness) recordIn(storage store.Store, message *testMessage) store.Record {
	h.t.Helper()
	record, err := h.recordFrom(storage, message)
	if err != nil {
		h.t.Error(err)
	}
	return record
}

func (h *testHarness) recordFrom(storage store.Store, message *testMessage) (store.Record, error) {
	row, err := storage.Load(h.t.Context(), seal.LocatorOf(message.id[:]))
	if err != nil {
		return store.Record{}, err
	}
	readKey := core.PublicKey(&message.link.R)
	return openRow(&row, seal.Read, readKey[:])
}

func (h *testHarness) mustRecord(message *testMessage) store.Record {
	h.t.Helper()
	record, err := h.record(message)
	if err != nil {
		h.t.Fatal(err)
	}
	return record
}

// rewrite seals the row again after change, keeping its blob, the way a row stored by an older build would look.
func (h *testHarness) rewrite(message *testMessage, change func(*store.Message)) {
	h.t.Helper()
	record := h.mustRecord(message)
	blobKey, err := record.BlobKey()
	if err != nil {
		h.t.Fatal(err)
	}
	change(record.Message)
	readKey, manageKey := core.PublicKey(&message.link.R), core.PublicKey(message.manage)
	rebuilt := store.NewRecord(
		record.Row.Locator,
		record.Message,
		readKey[:],
		manageKey[:],
		record.Row.BlobRef,
		blobKey,
	)
	rebuilt.Row.BlobPurgeAt, rebuilt.Row.PurgeAt = record.Row.BlobPurgeAt, record.Row.PurgeAt
	err = h.store.Update(h.t.Context(), record.Row.Locator, func(row *store.Row) error {
		*row = rebuilt.Row
		return nil
	})
	if err != nil {
		h.t.Fatal(err)
	}
}

// endedState reports the state of a message whose blob is gone, the shape a tombstone had.
func (h *testHarness) endedState(message *testMessage) (store.State, bool) {
	record, err := h.record(message)
	if err != nil || record.HasBlob() {
		return "", false
	}
	return record.State(h.clock.Now()), true
}

func (h *testHarness) markEnding(message *testMessage, state store.State) {
	h.t.Helper()
	record := h.mustRecord(message)
	if _, err := store.MarkEnding(h.t.Context(), h.store, &record, state, h.clock.Now()); err != nil {
		h.t.Fatal(err)
	}
}

func (h *testHarness) finish(message *testMessage, now time.Time) (bool, error) {
	record, err := h.record(message)
	if err != nil {
		return false, err
	}
	return store.Finish(h.t.Context(), h.store, h.blobs, &record, now)
}

func (h *testHarness) openObject(message *testMessage, object []byte, policy string) (core.Payload, error) {
	h.t.Helper()
	decoded, err := decodePolicy(policy)
	if err != nil {
		return core.Payload{}, err
	}
	_, payload, err := core.Open(&message.content.K, message.id, decoded, message.kdf, object)
	return payload, err
}

func (h *testHarness) sign(r *http.Request, token, verifier string, body []byte) {
	path := r.URL.EscapedPath()
	purpose := core.PurposeRead
	if strings.HasSuffix(path, "/status") || strings.HasSuffix(path, "/cancel") {
		purpose = core.PurposeManage
	}
	challenge := h.challenge()
	if verifier != "" {
		r.Header.Set("X-Nylzo-Verifier", verifierHeader(verifier, r.Method, path, challenge))
	}
	r.Header.Set("Authorization", signedHeader(token, purpose, r.Method, path, challenge, body))
}

func (h *testHarness) withIDToken(header string) string {
	data, err := base64.RawURLEncoding.DecodeString(header)
	if err != nil {
		return header
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields == nil {
		return header
	}
	if _, ok := fields["id_token"]; ok {
		return header
	}
	var rawID string
	if json.Unmarshal(fields["id"], &rawID) != nil {
		return header
	}
	id, err := core.ParseID(rawID)
	if err != nil {
		return header
	}
	fields["id_token"], _ = json.Marshal(h.idToken(id))
	encoded, err := json.Marshal(fields)
	if err != nil {
		return header
	}
	return core.Encode(encoded)
}

func (h *testHarness) abuseFile(day time.Time) string {
	return filepath.Join(h.directory, "abuse", day.UTC().Format(time.DateOnly)+abuse.FileExtension)
}

// abuseReports opens the sealed reports with the operator's key and returns them as JSON lines.
func (h *testHarness) abuseReports() ([]byte, error) {
	files, err := filepath.Glob(filepath.Join(h.directory, "abuse", "*"+abuse.FileExtension))
	if err != nil || len(files) == 0 {
		return nil, cmp.Or(err, os.ErrNotExist)
	}
	slices.Sort(files)
	var all []byte
	for _, file := range files {
		data, readErr := os.ReadFile(file)
		if readErr != nil {
			return nil, readErr
		}
		reports, openErr := abuse.Open(testReportKey, data)
		if openErr != nil {
			return nil, openErr
		}
		for i := range reports {
			encoded, encodeErr := json.Marshal(&reports[i])
			if encodeErr != nil {
				return nil, encodeErr
			}
			all = append(append(all, encoded...), '\n')
		}
	}
	return all, nil
}

type faultyBlobs struct {
	store.Blobs

	mutex      sync.Mutex
	failDelete map[string]bool
	deleteErr  map[string]error
	failOpen   error
	failPut    error
	failList   error
	failRead   error
	onOpen     func(ref string)
}

func (h *testHarness) withFaultyBlobs() *faultyBlobs {
	faulty := &faultyBlobs{Blobs: h.blobs, failDelete: map[string]bool{}, deleteErr: map[string]error{}}
	h.blobs = faulty
	h.reopen(&h.settings)
	return faulty
}

func (b *faultyBlobs) Delete(ctx context.Context, ref string) error {
	b.mutex.Lock()
	failing, err := b.failDelete[ref], b.deleteErr[ref]
	b.mutex.Unlock()
	if err != nil {
		return err
	}
	if failing {
		return errInjected
	}
	return b.Blobs.Delete(ctx, ref)
}

func (b *faultyBlobs) DeleteBatch(ctx context.Context, refs []string) []error {
	errs := make([]error, len(refs))
	for i, ref := range refs {
		errs[i] = b.Delete(ctx, ref)
	}
	return errs
}

func (b *faultyBlobs) Open(ctx context.Context, ref string) (io.ReadCloser, int64, error) {
	b.mutex.Lock()
	openErr, readErr, hook := b.failOpen, b.failRead, b.onOpen
	b.mutex.Unlock()
	if hook != nil {
		hook(ref)
	}
	if openErr != nil {
		return nil, 0, openErr
	}
	blob, size, err := b.Blobs.Open(ctx, ref)
	if err != nil || readErr == nil {
		return blob, size, err
	}
	return failingReader{ReadCloser: blob, err: readErr}, size, nil
}

func (b *faultyBlobs) Put(ctx context.Context, ref string, reader io.Reader, size int64) error {
	b.mutex.Lock()
	err := b.failPut
	b.mutex.Unlock()
	if err != nil {
		io.Copy(io.Discard, reader)
		return err
	}
	return b.Blobs.Put(ctx, ref, reader, size)
}

func (b *faultyBlobs) List(ctx context.Context) ([]store.BlobInfo, error) {
	b.mutex.Lock()
	err := b.failList
	b.mutex.Unlock()
	if err != nil {
		return nil, err
	}
	return b.Blobs.List(ctx)
}

func (b *faultyBlobs) set(change func(*faultyBlobs)) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	change(b)
}

type failingReader struct {
	io.ReadCloser

	err error
}

func (f failingReader) Read([]byte) (int, error) {
	return 0, f.err
}

type testMessage struct {
	id          core.ID
	secret      *[core.SecretLen]byte
	manage      *[core.SeedLen]byte
	kdf         core.KDF
	link        core.LinkKeys
	content     core.ContentKeys
	policy      core.Policy
	object      []byte
	text        string
	readToken   string
	manageToken string
	verifier    string
	address     string
	blobRef     string
}

func newMessage(t *testing.T, kdf core.KDF, passphrase string, opens uint16, ttl uint32, text string) *testMessage {
	t.Helper()
	message, err := buildMessageWithID(core.NewID(), kdf, passphrase, opens, ttl, text)
	if err != nil {
		t.Fatal(err)
	}
	return message
}

// testPassphraseKey stands in for scrypt: the server never sees the passphrase key, only what derives from it.
func testPassphraseKey(kdf core.KDF, passphrase string, salt *[32]byte) []byte {
	if kdf == core.KDFNone {
		return nil
	}
	key := sha256.Sum256(append([]byte(passphrase), salt[:]...))
	return key[:]
}

func buildMessageWithID(
	id core.ID,
	kdf core.KDF,
	passphrase string,
	opens uint16,
	ttl uint32,
	text string,
) (*testMessage, error) {
	message := &testMessage{id: id, secret: core.NewSecret(), manage: core.NewSecret(), kdf: kdf, text: text}
	link, err := core.DeriveLink(id, message.secret)
	if err != nil {
		return nil, err
	}
	content, err := core.DeriveContent(id, message.secret, testPassphraseKey(kdf, passphrase, &link.PassphraseSalt))
	if err != nil {
		return nil, err
	}
	message.link, message.content = link, content
	message.policy = core.Policy{OpensMax: opens, TTLSeconds: ttl}
	payload := core.Payload{Meta: core.Meta{Text: &text}}
	if message.object, err = core.Seal(&content.K, core.NewHeader(kdf), id, message.policy, payload); err != nil {
		return nil, err
	}
	message.readToken = core.Encode(link.R[:])
	message.manageToken = core.Encode(message.manage[:])
	message.verifier = core.Encode(content.V[:])
	return message, nil
}

func publicKey(seed *[core.SeedLen]byte) string {
	key := core.PublicKey(seed)
	return core.Encode(key[:])
}

func (m *testMessage) createHeader(sizeOverride int64) string {
	fields := map[string]any{
		"id":         m.id.String(),
		"read_key":   publicKey(&m.link.R),
		"manage_key": publicKey(m.manage),
		"kdf_id":     int(m.kdf),
		"opens_max":  int(m.policy.OpensMax),
		"ttl_s":      int(m.policy.TTLSeconds),
		"size":       len(m.object),
	}
	if sizeOverride > 0 {
		fields["size"] = sizeOverride
	}
	if m.kdf != core.KDFNone {
		fields["verifier_key"] = publicKey(&m.content.V)
	}
	data, _ := json.Marshal(fields)
	return core.Encode(data)
}

func createRequestFor(message *testMessage) *testRequest {
	return &testRequest{
		method:  http.MethodPost,
		path:    "/api/v1/messages",
		body:    message.object,
		address: message.address,
		headers: map[string]string{
			"Content-Type":   "application/octet-stream",
			"X-Nylzo-Create": message.createHeader(0),
		},
	}
}

func claimRequestFor(message *testMessage, verifier, address string) *testRequest {
	return &testRequest{
		method:   http.MethodPost,
		path:     "/api/v1/messages/" + message.id.String() + "/claim",
		address:  cmp.Or(address, message.address),
		token:    message.readToken,
		verifier: verifier,
	}
}

func mutateHeader(t *testing.T, message *testMessage, change func(map[string]any)) string {
	t.Helper()
	data, err := base64.RawURLEncoding.DecodeString(message.createHeader(0))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err = json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	change(fields)
	encoded, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return core.Encode(encoded)
}

func decodeJSON(t *testing.T, body []byte) jsonFields {
	t.Helper()
	if len(body) == 0 {
		return nil
	}
	var decoded jsonFields
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("invalid json %q: %v", body, err)
	}
	return decoded
}

func decodePolicy(header string) (core.Policy, error) {
	var policy struct {
		OpensMax   int `json:"opens_max"`
		TTLSeconds int `json:"ttl_s"`
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(header)
	if err != nil {
		return core.Policy{}, err
	}
	if decodeErr := json.Unmarshal(data, &policy); decodeErr != nil {
		return core.Policy{}, decodeErr
	}
	if policy.OpensMax == 0 {
		return core.Policy{}, errors.New("policy header without opens_max")
	}
	return core.Policy{OpensMax: uint16(policy.OpensMax), TTLSeconds: uint32(policy.TTLSeconds)}, nil
}

func signedHeader(token, purpose, method, path string, challenge [core.ChallengeLen]byte, body []byte) string {
	seed, err := core.DecodeExact(token, core.SeedLen)
	if err != nil {
		return core.AuthScheme + " " + token
	}
	return core.SignRequest((*[core.SeedLen]byte)(seed), purpose, method, path, challenge, body).Authorization()
}

func verifierHeader(verifier, method, path string, challenge [core.ChallengeLen]byte) string {
	seed, err := core.DecodeExact(verifier, core.SeedLen)
	if err != nil {
		return verifier
	}
	signature := core.SignRequest((*[core.SeedLen]byte)(seed), core.PurposeVerifier, method, path, challenge, nil)
	return signature.VerifierHeader()
}

func signedRequest(
	seed *[core.SeedLen]byte,
	purpose, method, path string,
	challenge [core.ChallengeLen]byte,
	body []byte,
) *testRequest {
	signature := core.SignRequest(seed, purpose, method, path, challenge, body)
	return &testRequest{
		method:  method,
		path:    path,
		body:    body,
		headers: map[string]string{"Authorization": signature.Authorization()},
	}
}

func rawAuth(request *testRequest, authorization string) *testRequest {
	if request.headers == nil {
		request.headers = map[string]string{}
	}
	request.headers["Authorization"] = authorization
	return request
}

func solveWork(t *testing.T, challenge string, id core.ID, difficulty int) string {
	t.Helper()
	token, err := core.SolvePoW(t.Context(), challenge, id, difficulty)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

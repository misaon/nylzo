package server_test

import (
	"bytes"
	"cmp"
	"context"
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

	"github.com/misaon/nylzo/internal/server/config"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server"
	"github.com/misaon/nylzo/internal/store"
	"github.com/misaon/nylzo/internal/store/disk"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

var (
	pepperA = bytes.Repeat([]byte{0x11}, 32)
	pepperB = bytes.Repeat([]byte{0x22}, 32)
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

type timer struct {
	due time.Time
	fn  func()
}

type harness struct {
	t      *testing.T
	cfg    config.Server
	store  store.Store
	blobs  store.Blobs
	clock  *fakeClock
	logBuf *syncBuffer
	timers struct {
		mu   sync.Mutex
		list []timer
	}
	srv     *server.Server
	ts      *httptest.Server
	client  *http.Client
	dir     string
	capture struct {
		mu   sync.Mutex
		last []byte
	}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func baseConfig(pepper, previous []byte) config.Server {
	cfg := config.Defaults()
	cfg.Pepper = pepper
	cfg.PepperPrevious = previous
	cfg.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")}
	cfg.Rates.BucketPerSecond = 1 << 20
	cfg.Rates.BucketBurst = 1 << 20
	cfg.Rates.SourcePerSecond = 1 << 20
	cfg.Rates.SourceBurst = 1 << 20
	return cfg
}

type faultyBlobs struct {
	store.Blobs
	mu         sync.Mutex
	failDelete map[string]bool
	deleteErr  map[string]error
	failOpen   error
	failPut    error
	failList   error
	failRead   error
	onOpen     func(ref string)
}

type failingReader struct {
	io.ReadCloser
	err error
}

func (f failingReader) Read([]byte) (int, error) {
	return 0, f.err
}

var errInjected = errors.New("test: injected storage failure")

func (b *faultyBlobs) set(fn func(*faultyBlobs)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	fn(b)
}

func (b *faultyBlobs) Delete(ctx context.Context, ref string) error {
	b.mu.Lock()
	fail, err := b.failDelete[ref], b.deleteErr[ref]
	b.mu.Unlock()
	if err != nil {
		return err
	}
	if fail {
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
	b.mu.Lock()
	err, readErr, hook := b.failOpen, b.failRead, b.onOpen
	b.mu.Unlock()
	if hook != nil {
		hook(ref)
	}
	if err != nil {
		return nil, 0, err
	}
	f, size, err := b.Blobs.Open(ctx, ref)
	if err == nil && readErr != nil {
		return failingReader{ReadCloser: f, err: readErr}, size, nil
	}
	return f, size, err
}

func (b *faultyBlobs) Put(ctx context.Context, ref string, r io.Reader, size int64) error {
	b.mu.Lock()
	err := b.failPut
	b.mu.Unlock()
	if err != nil {
		io.Copy(io.Discard, r)
		return err
	}
	return b.Blobs.Put(ctx, ref, r, size)
}

func (b *faultyBlobs) List(ctx context.Context) ([]store.BlobInfo, error) {
	b.mu.Lock()
	err := b.failList
	b.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return b.Blobs.List(ctx)
}

func (h *harness) withFaultyBlobs() *faultyBlobs {
	fb := &faultyBlobs{Blobs: h.blobs, failDelete: map[string]bool{}, deleteErr: map[string]error{}}
	h.blobs = fb
	h.reopen(h.cfg)
	return fb
}

func (h *harness) startOfNextUTCDay() {
	h.clock.mu.Lock()
	h.clock.now = time.Now().UTC().Truncate(24 * time.Hour).Add(25 * time.Hour)
	h.clock.mu.Unlock()
}

func (h *harness) listen() string {
	h.t.Helper()
	httpServer := h.srv.HTTPServer()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		h.t.Fatal(err)
	}
	go httpServer.Serve(ln)
	h.t.Cleanup(func() { httpServer.Close() })
	return ln.Addr().String()
}

func newHarness(t *testing.T, cfg config.Server) *harness {
	t.Helper()
	dir := t.TempDir()
	cfg.DataDir = dir
	st, err := sqlite.Open(t.Context(), filepath.Join(dir, "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	blobs, err := disk.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, cfg: cfg, store: st, blobs: blobs, dir: dir, logBuf: &syncBuffer{}}
	h.clock = &fakeClock{now: time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)}
	h.open()
	return h
}

func (h *harness) open() {
	h.t.Helper()
	logger := slog.New(slog.NewJSONHandler(h.logBuf, nil))
	srv, err := server.New(h.cfg, h.store, h.blobs,
		server.WithClock(h.clock.Now),
		server.WithLogger(logger),
		server.WithAfterFunc(func(d time.Duration, fn func()) {
			h.timers.mu.Lock()
			h.timers.list = append(h.timers.list, timer{due: h.clock.Now().Add(d), fn: fn})
			h.timers.mu.Unlock()
		}),
	)
	if err != nil {
		h.t.Fatal(err)
	}
	h.srv = srv
	inner := srv.Handler()
	h.ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/api/v1/messages" {
			var dump bytes.Buffer
			fmt.Fprintf(&dump, "%s %s\n", r.Method, r.URL.RequestURI())
			r.Header.Write(&dump)
			body, _ := io.ReadAll(r.Body)
			dump.Write(body)
			r.Body = io.NopCloser(bytes.NewReader(body))
			h.capture.mu.Lock()
			h.capture.last = dump.Bytes()
			h.capture.mu.Unlock()
		}
		inner.ServeHTTP(w, r)
	}))
	h.t.Cleanup(h.ts.Close)
	h.client = h.ts.Client()
}

func (h *harness) reopen(cfg config.Server) {
	h.ts.Close()
	cfg.DataDir = h.dir
	h.cfg = cfg
	h.open()
}

func (h *harness) fireTimers() int {
	h.timers.mu.Lock()
	now := h.clock.Now()
	var due []func()
	var rest []timer
	for _, tm := range h.timers.list {
		if !tm.due.After(now) {
			due = append(due, tm.fn)
		} else {
			rest = append(rest, tm)
		}
	}
	h.timers.list = rest
	h.timers.mu.Unlock()
	for _, fn := range due {
		fn()
	}
	return len(due)
}

func (h *harness) pendingTimers() int {
	h.timers.mu.Lock()
	defer h.timers.mu.Unlock()
	return len(h.timers.list)
}

type message struct {
	id        core.ID
	secret    *[32]byte
	manage    *[32]byte
	kdf       core.KDF
	link      core.LinkKeys
	content   core.ContentKeys
	policy    core.Policy
	object    []byte
	text      string
	readToken string
	manageTok string
	verifier  string
	ip        string
}

type request struct {
	method    string
	path      string
	body      []byte
	headers   map[string]string
	ip        string
	token     string
	verifier  string
	rawCreate bool
}

func (h *harness) do(req request) (*http.Response, []byte) {
	h.t.Helper()
	resp, data, err := h.tryDo(req)
	if err != nil {
		h.t.Fatal(err)
	}
	return resp, data
}

func (h *harness) tryDo(req request) (*http.Response, []byte, error) {
	var body io.Reader
	if req.body != nil {
		body = bytes.NewReader(req.body)
	}
	r, err := http.NewRequest(req.method, h.ts.URL+req.path, body)
	if err != nil {
		return nil, nil, err
	}
	for k, v := range req.headers {
		r.Header.Set(k, v)
	}
	if req.ip != "" {
		r.Header.Set("X-Forwarded-For", req.ip)
	}
	if req.method == "POST" && req.path == "/api/v1/messages" && !req.rawCreate {
		if header := r.Header.Get("X-Nylzo-Create"); header != "" {
			r.Header.Set("X-Nylzo-Create", h.withIDToken(header))
		}
	}
	if req.token != "" {
		h.sign(r, req.token, req.verifier, req.body)
	}
	resp, err := h.client.Do(r)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode == http.StatusInternalServerError {
		logs := h.logBuf.String()
		if len(logs) > 2000 {
			logs = logs[len(logs)-2000:]
		}
		h.t.Logf("server log tail after 500: %s", logs)
	}
	return resp, data, nil
}

func (h *harness) statusFrom(req request) int {
	resp, _, err := h.tryDo(req)
	if err != nil {
		h.t.Error(err)
		return 0
	}
	return resp.StatusCode
}

func buildMessage(kdf core.KDF, passphrase string, opens uint16, ttl uint32, text string) (*message, error) {
	return buildMessageWithID(core.NewID(), kdf, passphrase, opens, ttl, text)
}

func buildMessageWithID(
	id core.ID,
	kdf core.KDF,
	passphrase string,
	opens uint16,
	ttl uint32,
	text string,
) (*message, error) {
	m := &message{id: id, secret: core.NewSecret(), manage: core.NewSecret(), kdf: kdf, text: text}
	link, content, err := core.DeriveAll(m.id, m.secret, kdf, passphrase)
	if err != nil {
		return nil, err
	}
	m.link = link
	m.content = content
	m.policy = core.Policy{OpensMax: opens, TTLSeconds: ttl}
	object, err := core.Seal(
		&content.K,
		core.NewHeader(kdf),
		m.id,
		m.policy,
		core.Payload{Meta: core.Meta{Text: &text}},
	)
	if err != nil {
		return nil, err
	}
	m.object = object
	m.readToken = core.Encode(link.R[:])
	m.manageTok = core.Encode(m.manage[:])
	m.verifier = core.Encode(content.V[:])
	return m, nil
}

func newMessage(t *testing.T, kdf core.KDF, passphrase string, opens uint16, ttl uint32, text string) *message {
	t.Helper()
	m, err := buildMessage(kdf, passphrase, opens, ttl, text)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func (h *harness) lastCreateRequest() []byte {
	h.capture.mu.Lock()
	defer h.capture.mu.Unlock()
	return append([]byte(nil), h.capture.last...)
}

func publicKey(seed *[core.TokenLen]byte) string {
	pk := core.PublicKey(seed)
	return core.Encode(pk[:])
}

func (m *message) createHeader(sizeOverride int64) string {
	fields := map[string]any{
		"id":         m.id.String(),
		"read_key":   publicKey(&m.link.R),
		"manage_key": publicKey(m.manage),
		"kdf_id": int(
			m.kdf,
		),
		"opens_max": int(m.policy.OpensMax),
		"ttl_s":     int(m.policy.TTLSeconds),
		"size":      len(m.object),
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

func (h *harness) create(m *message) (*http.Response, []byte) {
	h.t.Helper()
	return h.do(createRequest(m))
}

func createRequest(m *message) request {
	return request{
		method: "POST", path: "/api/v1/messages", body: m.object, ip: m.ip,
		headers: map[string]string{"Content-Type": "application/octet-stream", "X-Nylzo-Create": m.createHeader(0)},
	}
}

func (h *harness) mustCreate(m *message) {
	h.t.Helper()
	resp, body := h.create(m)
	if resp.StatusCode != http.StatusCreated {
		h.t.Fatalf("create: %d %s", resp.StatusCode, body)
	}
}

func (h *harness) metadata(m *message) (*http.Response, map[string]any) {
	h.t.Helper()
	resp, body := h.do(request{method: "GET", path: "/api/v1/messages/" + m.id.String(), ip: m.ip, token: m.readToken})
	return resp, decodeJSON(h.t, body)
}

func (h *harness) claim(m *message, verifier, ip string) (*http.Response, []byte) {
	h.t.Helper()
	return h.do(claimRequest(m, verifier, ip))
}

func claimRequest(m *message, verifier, ip string) request {
	req := request{
		method:   "POST",
		path:     "/api/v1/messages/" + m.id.String() + "/claim",
		ip:       ip,
		token:    m.readToken,
		verifier: verifier,
	}
	if ip == "" {
		req.ip = m.ip
	}
	return req
}

func (h *harness) blob(m *message, lease string) (*http.Response, []byte) {
	h.t.Helper()
	return h.do(
		request{
			method:  "GET",
			path:    "/api/v1/messages/" + m.id.String() + "/blob",
			ip:      m.ip,
			token:   m.readToken,
			headers: map[string]string{"X-Nylzo-Lease": lease},
		},
	)
}

func (h *harness) ack(m *message, lease string) *http.Response {
	h.t.Helper()
	resp, _ := h.do(
		request{
			method:  "POST",
			path:    "/api/v1/messages/" + m.id.String() + "/ack",
			ip:      m.ip,
			token:   m.readToken,
			headers: map[string]string{"X-Nylzo-Lease": lease},
		},
	)
	return resp
}

func (h *harness) status(m *message) (*http.Response, map[string]any) {
	h.t.Helper()
	resp, body := h.do(
		request{method: "GET", path: "/api/v1/messages/" + m.id.String() + "/status", ip: m.ip, token: m.manageTok},
	)
	return resp, decodeJSON(h.t, body)
}

func decodeJSON(t *testing.T, body []byte) map[string]any {
	t.Helper()
	if len(body) == 0 {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("invalid json %q: %v", body, err)
	}
	return out
}

func (h *harness) blobExists(id core.ID) bool {
	h.t.Helper()
	f, _, err := h.blobs.Open(h.t.Context(), id.String())
	if err != nil {
		return false
	}
	f.Close()
	return true
}

func (h *harness) openObject(m *message, object []byte, policy string) (core.Payload, error) {
	h.t.Helper()
	pol, err := decodePolicy(policy)
	if err != nil {
		return core.Payload{}, err
	}
	_, payload, err := core.Open(&m.content.K, m.id, pol, m.kdf, object)
	return payload, err
}

func decodePolicy(header string) (core.Policy, error) {
	var pol struct {
		OpensMax int `json:"opens_max"`
		TTL      int `json:"ttl_s"`
		KDFID    int `json:"kdf_id"`
		Flags    int `json:"flags"`
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(header)
	if err != nil {
		return core.Policy{}, err
	}
	if err := json.Unmarshal(data, &pol); err != nil {
		return core.Policy{}, err
	}
	if pol.OpensMax == 0 {
		return core.Policy{}, errors.New("policy header without opens_max")
	}
	return core.Policy{OpensMax: uint16(pol.OpensMax), TTLSeconds: uint32(pol.TTL)}, nil
}

func (h *harness) sign(r *http.Request, token, verifier string, body []byte) {
	path := r.URL.EscapedPath()
	purpose := core.PurposeRead
	if strings.HasSuffix(path, "/status") || strings.HasSuffix(path, "/cancel") {
		purpose = core.PurposeManage
	}
	challenge := h.srv.ChallengeForTest()
	if verifier != "" {
		r.Header.Set("X-Nylzo-Verifier", verifierHeader(verifier, r.Method, path, challenge))
	}
	r.Header.Set("Authorization", signedHeader(token, purpose, r.Method, path, challenge, body))
}

func signedHeader(token, purpose, method, path string, challenge [core.ChallengeLen]byte, body []byte) string {
	seed, err := core.DecodeExact(token, core.TokenLen)
	if err != nil {
		return core.AuthScheme + " " + token
	}
	return core.SignRequest((*[core.TokenLen]byte)(seed), purpose, method, path, challenge, body).Authorization()
}

func verifierHeader(verifier, method, path string, challenge [core.ChallengeLen]byte) string {
	seed, err := core.DecodeExact(verifier, core.TokenLen)
	if err != nil {
		return verifier
	}
	return core.SignRequest((*[core.TokenLen]byte)(seed), core.PurposeVerifier, method, path, challenge, nil).
		VerifierHeader()
}

func signedRequest(
	seed *[core.TokenLen]byte,
	purpose, method, path string,
	challenge [core.ChallengeLen]byte,
	body []byte,
) request {
	sig := core.SignRequest(seed, purpose, method, path, challenge, body)
	return rawAuth(request{method: method, path: path, body: body}, sig.Authorization())
}

func rawAuth(req request, authorization string) request {
	if req.headers == nil {
		req.headers = map[string]string{}
	}
	req.headers["Authorization"] = authorization
	return req
}

func (h *harness) withIDToken(header string) string {
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
	var idText string
	if json.Unmarshal(fields["id"], &idText) != nil {
		return header
	}
	id, err := core.ParseID(idText)
	if err != nil {
		return header
	}
	token, _ := json.Marshal(h.srv.IDTokenForTest(id))
	fields["id_token"] = token
	out, err := json.Marshal(fields)
	if err != nil {
		return header
	}
	return core.Encode(out)
}

func (h *harness) abuseFile(day time.Time) string {
	return filepath.Join(h.dir, "abuse", day.UTC().Format(time.DateOnly)+".jsonl")
}

func (h *harness) abuseReports() ([]byte, error) {
	files, err := filepath.Glob(filepath.Join(h.dir, "abuse", "*.jsonl"))
	if err != nil || len(files) == 0 {
		return nil, cmp.Or(err, os.ErrNotExist)
	}
	slices.Sort(files)
	var all []byte
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		all = append(all, data...)
	}
	return all, nil
}

func (h *harness) post(m *message, action, token string) (*http.Response, []byte) {
	h.t.Helper()
	return h.do(request{method: "POST", path: "/api/v1/messages/" + m.id.String() + "/" + action, token: token})
}

func mutateHeader(t *testing.T, m *message, f func(map[string]any)) string {
	t.Helper()
	data, err := base64.RawURLEncoding.DecodeString(m.createHeader(0))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	f(fields)
	out, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return core.Encode(out)
}

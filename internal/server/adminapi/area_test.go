package adminapi

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/api"
	"github.com/misaon/nylzo/internal/core/signing"
	"github.com/misaon/nylzo/internal/server/adminstore"
	"github.com/misaon/nylzo/internal/server/country"
	"github.com/misaon/nylzo/internal/server/hostusage"
	"github.com/misaon/nylzo/internal/server/passkey"
	"github.com/misaon/nylzo/internal/server/ratelimit"
	"github.com/misaon/nylzo/internal/server/statistics"
	"github.com/misaon/nylzo/internal/server/visits"
)

const (
	testOrigin       = "https://nylzo.test"
	testRelyingParty = "nylzo.test"
)

var errStoreBroken = errors.New("the store is broken")

type fakeStore struct {
	held         []adminstore.Passkey
	open         bool
	failing      map[string]bool
	notRecorded  bool
	beforeRecord func()
}

func (s *fakeStore) EnrollmentOpen(context.Context, adminstore.TokenHash, time.Time) (bool, error) {
	return s.open, s.failure("EnrollmentOpen")
}

func (s *fakeStore) Enroll(context.Context, adminstore.TokenHash, *passkey.Credential, time.Time) error {
	return s.failure("Enroll")
}

func (s *fakeStore) Passkey(_ context.Context, credentialID []byte) (adminstore.Passkey, bool, error) {
	for index := range s.held {
		if bytes.Equal(s.held[index].ID, credentialID) {
			return s.held[index], true, s.failure("Passkey")
		}
	}
	return adminstore.Passkey{}, false, s.failure("Passkey")
}

func (s *fakeStore) Passkeys(context.Context) ([]adminstore.Passkey, error) {
	return s.held, s.failure("Passkeys")
}

func (s *fakeStore) CountPasskeys(context.Context) (int, error) {
	return len(s.held), s.failure("CountPasskeys")
}

func (s *fakeStore) RecordUse(context.Context, *passkey.Credential, time.Time) (bool, error) {
	if s.beforeRecord != nil {
		s.beforeRecord()
	}
	return !s.notRecorded, s.failure("RecordUse")
}

func (s *fakeStore) failure(method string) error {
	if s.failing[method] {
		return errStoreBroken
	}
	return nil
}

type fakeChallenges struct {
	exhausted bool
}

func (c *fakeChallenges) Issue(now time.Time) ([signing.ChallengeLen]byte, time.Time, bool) {
	return [signing.ChallengeLen]byte{1}, now.Add(time.Minute), !c.exhausted
}

type fakeCountries struct{}

func (fakeCountries) Lookup(netip.Addr) country.Code {
	return country.Code{'C', 'Z'}
}

func (fakeCountries) Source() country.Source {
	return country.Source{Name: "table", Month: "2026-11"}
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

type testArea struct {
	t          *testing.T
	area       *API
	store      *fakeStore
	counts     *adminstore.Store
	challenges *fakeChallenges
	events     *syncBuffer
	refused    []string
	stored     error
	mux        *http.ServeMux
}

func newTestArea(t *testing.T, publicURL string) *testArea {
	t.Helper()
	counts, err := adminstore.Open(t.Context(), filepath.Join(t.TempDir(), adminstore.FileName))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { counts.Close() })
	usage := hostusage.NewSampler(4)
	stopped, stop := context.WithCancel(t.Context())
	stop()
	usage.Run(stopped, time.Hour)
	tested := &testArea{
		t:          t,
		store:      &fakeStore{failing: map[string]bool{}},
		counts:     counts,
		challenges: &fakeChallenges{},
		events:     &syncBuffer{},
		mux:        http.NewServeMux(),
	}
	statisticsCounts := statistics.New(counts)
	tested.area = New(&Config{
		Store:         tested.store,
		PublicURL:     publicURL,
		Challenges:    tested.challenges,
		Spent:         func(*http.Request, [signing.ChallengeLen]byte) bool { return true },
		Counts:        statisticsCounts,
		Visits:        visits.NewTracker(statisticsCounts, fakeCountries{}, time.Now()),
		Usage:         usage,
		Refusals:      ratelimit.NewRefusals(),
		Countries:     fakeCountries{},
		Stored:        func(context.Context) (int, error) { return 3, tested.stored },
		DataDirectory: t.TempDir(),
		Version:       "test",
		Events:        slog.New(slog.NewTextHandler(tested.events, nil)),
		Refuse:        func(_ *http.Request, reason string) { tested.refused = append(tested.refused, reason) },
	})
	tested.area.Register(tested.mux)
	return tested
}

type answer struct {
	status int
	fields map[string]any
}

func (a *testArea) serve(request *http.Request) answer {
	a.t.Helper()
	recorder := httptest.NewRecorder()
	a.mux.ServeHTTP(recorder, request)
	fields := map[string]any{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &fields); err != nil {
		a.t.Fatalf("%s %s answered %q", request.Method, request.URL, recorder.Body.String())
	}
	return answer{status: recorder.Code, fields: fields}
}

func (a *testArea) postRaw(path, host, body string) answer {
	a.t.Helper()
	request := httptest.NewRequest(http.MethodPost, api.AdminPath+path, strings.NewReader(body))
	request.Host = host
	return a.serve(request)
}

func (a *testArea) post(path string, body any) answer {
	a.t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		a.t.Fatal(err)
	}
	return a.postRaw(path, testRelyingParty, string(encoded))
}

type softwarePasskey struct {
	key   *ecdsa.PrivateKey
	id    []byte
	count uint32
}

func newSoftwarePasskey(t *testing.T) *softwarePasskey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 32)
	rand.Read(id)
	return &softwarePasskey{key: key, id: id}
}

func (p *softwarePasskey) publicKey(t *testing.T) []byte {
	t.Helper()
	encoded, err := x509.MarshalPKIXPublicKey(&p.key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func (p *softwarePasskey) stored(t *testing.T, lastUsed time.Time) adminstore.Passkey {
	t.Helper()
	return adminstore.Passkey{ID: p.id, PublicKey: p.publicKey(t), Algorithm: passkey.ES256, LastUsedAt: lastUsed}
}

func (p *softwarePasskey) authenticatorData(attested bool) []byte {
	hash := sha256.Sum256([]byte(testRelyingParty))
	flags := byte(0x05)
	if attested {
		flags |= 0x40
	}
	p.count++
	data := append(hash[:], flags)
	data = binary.BigEndian.AppendUint32(data, p.count)
	if attested {
		data = append(data, make([]byte, 16)...)
		data = binary.BigEndian.AppendUint16(data, uint16(len(p.id)))
		data = append(data, p.id...)
		data = append(data, 0xa0)
	}
	return data
}

func clientData(t *testing.T, kind string, challenge []byte) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{"type": kind, "challenge": core.Encode(challenge), "origin": testOrigin})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type signIn struct {
	key       sessionKey
	challenge []byte
	request   map[string]string
}

func (a *testArea) signInRequest(p *softwarePasskey) signIn {
	a.t.Helper()
	key := sessionKey(signing.PublicKey(core.NewSecret()))
	challenge := a.area.sessions.signInChallenge(key, time.Now())
	data := p.authenticatorData(false)
	client := clientData(a.t, "webauthn.get", challenge)
	clientHash := sha256.Sum256(client)
	digest := sha256.Sum256(append(bytes.Clone(data), clientHash[:]...))
	signature, err := ecdsa.SignASN1(rand.Reader, p.key, digest[:])
	if err != nil {
		a.t.Fatal(err)
	}
	return signIn{key: key, challenge: challenge, request: map[string]string{
		"key":                core.Encode(key[:]),
		"credential_id":      core.Encode(p.id),
		"client_data":        core.Encode(client),
		"authenticator_data": core.Encode(data),
		"signature":          core.Encode(signature),
	}}
}

func (a *testArea) registration(p *softwarePasskey, token string, challenge []byte) map[string]any {
	return map[string]any{
		"token":              token,
		"credential_id":      core.Encode(p.id),
		"client_data":        core.Encode(clientData(a.t, "webauthn.create", challenge)),
		"authenticator_data": core.Encode(p.authenticatorData(true)),
		"public_key":         core.Encode(p.publicKey(a.t)),
		"algorithm":          int(passkey.ES256),
	}
}

func (a *testArea) enrollmentChallenge(token string) []byte {
	a.t.Helper()
	options := a.post("/enrollment/options", map[string]string{"token": token})
	publicKey, _ := options.fields["public_key"].(map[string]any)
	encoded, _ := publicKey["challenge"].(string)
	challenge, err := core.DecodeExact(encoded, ceremonyChallenge)
	if err != nil {
		a.t.Fatalf("no ceremony challenge in %d %v", options.status, options.fields)
	}
	return challenge
}

type openedSession struct {
	seed *[core.SeedLen]byte
	key  sessionKey
}

func (a *testArea) openSession(previous time.Time) openedSession {
	seed := core.NewSecret()
	key := sessionKey(signing.PublicKey(seed))
	now := time.Now()
	a.area.sessions.open(key, []byte("credential"), a.area.sessions.signInChallenge(key, now), previous, now)
	return openedSession{seed: seed, key: key}
}

func (a *testArea) signed(signedIn openedSession, method, path string) answer {
	a.t.Helper()
	request := httptest.NewRequest(method, path, http.NoBody)
	authorization := signing.SignRequest(
		signedIn.seed,
		signing.PurposeAdmin,
		method,
		path,
		[signing.ChallengeLen]byte{1},
		nil,
		signing.Components{},
	).Authorization()
	request.Header.Set("Authorization", authorization)
	return a.serve(request)
}

func wantStatus(t *testing.T, what string, got answer, status int) {
	t.Helper()
	if got.status != status {
		t.Fatalf("%s: got %d %v, want %d", what, got.status, got.fields, status)
	}
}

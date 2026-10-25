package server

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/netip"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/server/config"
	"github.com/misaon/nylzo/internal/store"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

func FuzzCreateHeader(f *testing.F) {
	message, err := buildMessageWithID(core.NewID(), core.KDFNone, "", 1, 3600, "seed")
	if err != nil {
		f.Fatal(err)
	}
	f.Add(message.createHeader(0))
	f.Add(core.Encode([]byte(`{"id":"` + message.id.String() + `"}`)))
	f.Add("")
	f.Add("not base64!")
	settings := config.Defaults()
	allowed := map[string]bool{
		"id":           true,
		"id_token":     true,
		"read_key":     true,
		"verifier_key": true,
		"manage_key":   true,
		"kdf_id":       true,
		"opens_max":    true,
		"ttl_s":        true,
		"size":         true,
	}
	f.Fuzz(func(t *testing.T, value string) {
		if !parsesCreateHeader(settings.Limits, value) {
			return
		}
		data, strictErr := base64.RawURLEncoding.Strict().DecodeString(value)
		if strictErr != nil {
			t.Fatal("accepted non-canonical base64url")
		}
		var fields map[string]json.RawMessage
		if unmarshalErr := json.Unmarshal(data, &fields); unmarshalErr != nil {
			t.Fatalf("accepted invalid JSON: %v", unmarshalErr)
		}
		for k := range fields {
			if !allowed[k] {
				t.Fatalf("accepted key %q", k)
			}
		}
		var harness struct {
			KDFID       int     `json:"kdf_id"`
			VerifierKey *string `json:"verifier_key"`
			OpensMax    int     `json:"opens_max"`
			TTL         int64   `json:"ttl_s"`
		}
		json.Unmarshal(data, &harness)
		if (harness.KDFID != 0) != (harness.VerifierKey != nil) {
			t.Fatal("verifier_key does not match kdf_id")
		}
		if harness.OpensMax < 1 || harness.OpensMax > settings.Limits.MaxOpens || harness.TTL < core.MinTTLSeconds ||
			harness.TTL > int64(settings.Limits.MaxTTL.Seconds()) {
			t.Fatal("accepted a policy outside the limits")
		}
	})
}

func TestCreateHeaderKeysMatchStruct(t *testing.T) {
	t.Parallel()
	tags := map[string]bool{}
	for field := range reflect.TypeFor[createHeader]().Fields() {
		tag, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		tags[tag] = true
	}
	if !reflect.DeepEqual(tags, createHeaderKeys) {
		t.Fatalf("createHeaderKeys %v differ from the createHeader tags %v", createHeaderKeys, tags)
	}
}

func TestCreateHeaderRules(t *testing.T) {
	t.Parallel()
	settings := config.Defaults()
	none := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	pw := newMessage(t, core.KDFScrypt, "pw", 1, 3600, "x")
	if !parsesCreateHeader(settings.Limits, none.createHeader(0)) ||
		!parsesCreateHeader(settings.Limits, pw.createHeader(0)) {
		t.Fatal("valid headers rejected")
	}
	raw, _ := base64.RawURLEncoding.DecodeString(none.createHeader(0))
	cases := map[string]string{
		"unknown-key":      mutateHeader(t, none, func(f map[string]any) { f["extra"] = 1 }),
		"case-variant-key": mutateHeader(t, none, func(f map[string]any) { f["ID"] = f["id"]; delete(f, "id") }),
		"verifier-with-kdf0": mutateHeader(
			t,
			none,
			func(f map[string]any) { f["verifier_key"] = publicKey(core.NewSecret()) },
		),
		"no-verifier-with-kdf1": mutateHeader(t, pw, func(f map[string]any) { delete(f, "verifier_key") }),
		"kdf-2-retired":         mutateHeader(t, pw, func(f map[string]any) { f["kdf_id"] = 2 }),
		"opens-0":               mutateHeader(t, none, func(f map[string]any) { f["opens_max"] = 0 }),
		"ttl-59":                mutateHeader(t, none, func(f map[string]any) { f["ttl_s"] = 59 }),
		"kdf-3":                 mutateHeader(t, none, func(f map[string]any) { f["kdf_id"] = 3 }),
		"trailing-brace":        core.Encode(append(append([]byte(nil), raw...), '}')),
		"trailing-object":       core.Encode(append(append([]byte(nil), raw...), []byte(" {}")...)),
		"not-an-object":         core.Encode([]byte(`[1,2]`)),
	}
	for name, value := range cases {
		if parsesCreateHeader(settings.Limits, value) {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestAK1CreateCarriesNoSecrets(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	message := newMessage(t, core.KDFScrypt, "tajná fráze", 1, 3600, "plaintext secret body")
	harness.mustCreate(message)
	captured := harness.lastCreateRequest()
	if !strings.Contains(string(captured), "X-Nylzo-Create: "+harness.withIDToken(message.createHeader(0))) ||
		!strings.Contains(string(captured), string(message.object)) {
		t.Fatal("wire capture missing the create request")
	}
	p, err := core.DerivePassphraseKey(message.kdf, "tajná fráze", &message.link.SaltPW)
	if err != nil {
		t.Fatal(err)
	}
	secrets := map[string][]byte{
		"S": message.secret[:],
		"K": message.content.K[:],
		"P": p,
		"R": message.link.R[:],
		"V": message.content.V[:],
		"M": message.manage[:],
	}
	for name, b := range secrets {
		for _, enc := range []string{core.Encode(b), hex.EncodeToString(b)} {
			if strings.Contains(string(captured), enc) {
				t.Fatalf("create request carries %s", name)
			}
		}
		if name != "R" && name != "V" && name != "M" && contains(captured, b) {
			t.Fatalf("create request carries raw %s", name)
		}
	}
	if strings.Contains(string(captured), "plaintext secret body") ||
		strings.Contains(string(captured), "tajná fráze") {
		t.Fatal("create request carries plaintext or passphrase")
	}
}

func contains(haystack, needle []byte) bool {
	return len(needle) > 0 && strings.Contains(string(haystack), string(needle))
}

func TestCreateValidation(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	message := newMessage(t, core.KDFScrypt, "pw", 1, 3600, "v")
	cases := []struct {
		name    string
		mutate  func(t *testing.T, r *http.Request)
		status  int
		errCode string
	}{
		{"no-header", func(t *testing.T, r *http.Request) { r.Header.Del("X-Nylzo-Create") }, 400, "bad_request"},
		{
			"bad-base64",
			func(t *testing.T, r *http.Request) { r.Header.Set("X-Nylzo-Create", "***") },
			400,
			"bad_request",
		},
		{"unknown-field", func(t *testing.T, r *http.Request) {
			r.Header.Set(
				"X-Nylzo-Create",
				harness.withIDToken(mutateHeader(t, message, func(f map[string]any) { f["extra"] = 1 })),
			)
		}, 400, "bad_request"},
		{"size-mismatch", func(t *testing.T, r *http.Request) {
			r.Header.Set("X-Nylzo-Create", harness.withIDToken(message.createHeader(int64(len(message.object)+1))))
		}, 400, "bad_request"},
		{"size-above-limit-mismatch", func(t *testing.T, r *http.Request) {
			r.Header.Set(
				"X-Nylzo-Create",
				harness.withIDToken(message.createHeader(config.Defaults().Limits.MaxObjectBytes+1)),
			)
		}, 400, "bad_request"},
		{"chunked", func(t *testing.T, r *http.Request) { r.ContentLength = -1 }, 411, "length_required"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, _ := http.NewRequest(
				"POST",
				harness.httpServer.URL+"/api/v1/messages",
				strings.NewReader(string(message.object)),
			)
			r.Header.Set("X-Nylzo-Create", harness.withIDToken(message.createHeader(0)))
			c.mutate(t, r)
			response, err := harness.client.Do(r)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(response.Body)
			response.Body.Close()
			if response.StatusCode != c.status || decodeJSON(t, body)["error"] != c.errCode {
				t.Fatalf("%d %s", response.StatusCode, body)
			}
		})
	}
	wrongKDF := newMessage(t, core.KDFNone, "", 1, 3600, "v")
	header := wrongKDF.createHeader(0)
	wrongKDF.object[1] = byte(core.KDFScrypt)
	response, _ := harness.do(
		&testRequest{
			method:  http.MethodPost,
			path:    "/api/v1/messages",
			body:    wrongKDF.object,
			headers: map[string]string{"X-Nylzo-Create": header},
		},
	)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("kdf mismatch between header and envelope: %d", response.StatusCode)
	}
	if harness.blobExists(wrongKDF.id) {
		t.Fatal("rejected object stored")
	}
	tooMany := newMessage(t, core.KDFNone, "", 11, 3600, "v")
	if got, _ := harness.create(tooMany); got.StatusCode != http.StatusBadRequest {
		t.Fatal("opens_max above the instance limit accepted")
	}
	tooLong := newMessage(t, core.KDFNone, "", 1, 8*24*3600, "v")
	if got, _ := harness.create(tooLong); got.StatusCode != http.StatusBadRequest {
		t.Fatal("ttl above the instance limit accepted")
	}
	harness.mustCreate(message)
	if got, _ := harness.create(message); got.StatusCode != http.StatusConflict {
		t.Fatal("duplicate id")
	}
}

func TestInstanceOffersNoPassphraseProfile(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	_, body := harness.do(&testRequest{method: http.MethodGet, path: "/api/v1/instance"})
	if _, ok := decodeJSON(t, body)["limits"].(map[string]any)["default_kdf_id"]; ok {
		t.Fatal("the instance still reports default_kdf_id, but D71 leaves one profile")
	}
}

func TestDefaultObjectLimitMatchesSpec(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	_, body := harness.do(&testRequest{method: http.MethodGet, path: "/api/v1/instance"})
	limits := decodeJSON(t, body)["limits"].(map[string]any)
	if limits["max_object_bytes"] != float64(5373983) || limits["max_text_bytes"] != float64(core.MaxTextBytes) {
		t.Fatalf("instance reports %v", limits)
	}
}

func issuedID(t *testing.T, harness *testHarness) (core.ID, string, time.Time) {
	t.Helper()
	_, body := harness.do(&testRequest{method: http.MethodGet, path: "/api/v1/instance"})
	c, ok := decodeJSON(t, body)["create"].(map[string]any)
	if !ok {
		t.Fatalf("instance carries no create block: %s", body)
	}
	id, err := core.ParseID(c["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	expires, err := time.Parse(time.RFC3339, c["expires_at"].(string))
	if err != nil {
		t.Fatal(err)
	}
	return id, c["id_token"].(string), expires
}

func createWithToken(t *testing.T, harness *testHarness, message *testMessage, token any) (testResponse, []byte) {
	t.Helper()
	header := mutateHeader(t, message, func(f map[string]any) {
		if token != nil {
			f["id_token"] = token
		}
	})
	return harness.do(&testRequest{
		method:    http.MethodPost,
		path:      "/api/v1/messages",
		body:      message.object,
		address:   message.address,
		rawCreate: true,
		headers:   map[string]string{"Content-Type": "application/octet-stream", "X-Nylzo-Create": header},
	})
}

func TestCreateNeedsAnIssuedIDToken(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	id, token, expires := issuedID(t, harness)
	if want := harness.clock.Now().Add(10 * time.Minute).Truncate(time.Second); !expires.Equal(want) {
		t.Fatalf("id token expires %v, want %v", expires, want)
	}
	if other, _, _ := issuedID(t, harness); other == id {
		t.Fatal("two instance responses issued the same id")
	}
	message, err := buildMessageWithID(id, core.KDFNone, "", 1, 3600, "issued")
	if err != nil {
		t.Fatal(err)
	}
	stranger := newMessage(t, core.KDFNone, "", 1, 3600, "chosen by the client")
	for name, c := range map[string]struct {
		message *testMessage
		token   any
	}{
		"missing":        {message, nil},
		"empty":          {message, ""},
		"malformed":      {message, "not-a-token"},
		"for another id": {stranger, token},
	} {
		if response, body := createWithToken(
			t,
			harness,
			c.message,
			c.token,
		); response.StatusCode != http.StatusForbidden ||
			decodeJSON(t, body)["error"] != "id_token" {
			t.Fatalf("%s token: %d %s", name, response.StatusCode, body)
		}
	}
	if response, body := createWithToken(t, harness, message, token); response.StatusCode != http.StatusCreated {
		t.Fatalf("issued id: %d %s", response.StatusCode, body)
	}
	if response, _ := createWithToken(t, harness, message, token); response.StatusCode != http.StatusConflict {
		t.Fatalf("a second create with the same token: %d", response.StatusCode)
	}
	if response, body := createWithToken(
		t,
		harness,
		message,
		nil,
	); response.StatusCode != http.StatusForbidden ||
		decodeJSON(t, body)["error"] != "id_token" {
		t.Fatalf("a create without a token must not learn that the id exists: %d %s", response.StatusCode, body)
	}
	late, lateToken, _ := issuedID(t, harness)
	harness.clock.Advance(10*time.Minute + 6*time.Second)
	lm, err := buildMessageWithID(late, core.KDFNone, "", 1, 3600, "late")
	if err != nil {
		t.Fatal(err)
	}
	if response, body := createWithToken(
		t,
		harness,
		lm,
		lateToken,
	); response.StatusCode != http.StatusForbidden ||
		decodeJSON(t, body)["error"] != "id_token" {
		t.Fatalf("expired token: %d %s", response.StatusCode, body)
	}
}

func TestGlobalCounterCountsOnlyAdmittedCreates(t *testing.T) {
	t.Parallel()
	settings := baseSettings(pepperA, nil)
	settings.Rates.GlobalDailyCreates = 1000
	harness := newHarness(t, settings)
	for range 25 {
		message := newMessage(t, core.KDFNone, "", 1, 3600, "n")
		message.address = "203.0.113.5"
		harness.create(message)
	}
	day := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	if c, err := harness.store.RateGet(t.Context(), globalCreatesKey, day); err != nil || c.Count != 20 {
		t.Fatalf("rate-limited attempts moved the global counter: %+v %v", c, err)
	}
	small := baseSettings(pepperA, nil)
	small.Rates.GlobalDailyCreates = 40
	harness.reopen(small)
	for i := range 3 {
		message := newMessage(t, core.KDFNone, "", 1, 3600, "n")
		message.address = fmt.Sprintf("198.51.100.%d", i+1)
		if response, _ := harness.create(message); response.StatusCode != http.StatusForbidden {
			t.Fatalf("create without pow at 50 percent: %d", response.StatusCode)
		}
	}
	if c, _ := harness.store.RateGet(t.Context(), globalCreatesKey, day); c.Count != 20 {
		t.Fatalf("PoW failures moved the global counter: %+v", c)
	}
	harness.reopen(settings)
	existing := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	existing.address = "198.51.100.10"
	harness.mustCreate(existing)
	if response, _ := harness.create(existing); response.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate id: %d", response.StatusCode)
	}
	wrongKDF := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	wrongKDF.address = "198.51.100.11"
	headerKDF1 := mutateHeader(
		t,
		wrongKDF,
		func(f map[string]any) { f["kdf_id"] = 1; f["verifier_key"] = publicKey(core.NewSecret()) },
	)
	if response, _ := harness.do(
		&testRequest{
			method:  http.MethodPost,
			path:    "/api/v1/messages",
			body:    wrongKDF.object,
			address: wrongKDF.address,
			headers: map[string]string{"X-Nylzo-Create": headerKDF1},
		},
	); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("body kdf differs from the header: %d", response.StatusCode)
	}
	if c, _ := harness.store.RateGet(t.Context(), globalCreatesKey, day); c.Count != 21 {
		t.Fatalf("only stored messages may move the global counter: %+v", c)
	}
}

type slotSignals struct {
	store.Store

	reserved chan struct{}
	released chan struct{}
}

func (s *slotSignals) RateReserve(ctx context.Context, hit store.RateHit) (int64, bool, error) {
	n, ok, err := s.Store.RateReserve(ctx, hit)
	if hit.Key == globalCreatesKey {
		s.reserved <- struct{}{}
	}
	return n, ok, err
}

func (s *slotSignals) RateRelease(ctx context.Context, key string, windowStart time.Time) error {
	err := s.Store.RateRelease(ctx, key, windowStart)
	if key == globalCreatesKey {
		s.released <- struct{}{}
	}
	return err
}

func TestDisconnectedCreateReturnsItsCapSlot(t *testing.T) {
	t.Parallel()
	settings := baseSettings(pepperA, nil)
	settings.Rates.GlobalDailyCreates = 10
	harness := newHarness(t, settings)
	signals := &slotSignals{Store: harness.store, reserved: make(chan struct{}, 1), released: make(chan struct{}, 1)}
	harness.store = signals
	harness.reopen(&harness.settings)
	message := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	conn, err := net.Dial("tcp", harness.listen())
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(
		conn,
		"POST /api/v1/messages HTTP/1.1\r\nHost: nylzo\r\nContent-Length: %d\r\nX-Nylzo-Create: %s\r\n\r\n",
		len(message.object),
		harness.withIDToken(message.createHeader(0)),
	)
	conn.Write(message.object[:100])
	wait := func(ch chan struct{}, what string) {
		select {
		case <-ch:
		// gstack-timing-ok: an upper bound on waiting for the event, not a measurement
		case <-time.After(5 * time.Second):
			t.Fatalf("the create never %s its cap slot", what)
		}
	}
	wait(signals.reserved, "reserved")
	conn.Close()
	wait(signals.released, "released")
	day := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	if c, rateGetErr := harness.store.RateGet(t.Context(), globalCreatesKey, day); rateGetErr != nil || c.Count != 0 {
		t.Fatalf("a disconnected create kept its cap slot: %+v %v", c, rateGetErr)
	}
}

func TestCapFullOfPendingCreatesAsksForAShortRetry(t *testing.T) {
	t.Parallel()
	settings := baseSettings(pepperA, nil)
	settings.Rates.GlobalDailyCreates = 2
	harness := newHarness(t, settings)
	day := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	hit := store.RateHit{Key: globalCreatesKey, WindowStart: day, PurgeAt: day.Add(48 * time.Hour), Limit: 2}
	for range 2 {
		if _, ok, err := harness.store.RateReserve(t.Context(), hit); err != nil || !ok {
			t.Fatal(err)
		}
	}
	message := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	response, _ := harness.create(message)
	if response.StatusCode != http.StatusServiceUnavailable || response.Header.Get("Retry-After") != "60" {
		t.Fatalf(
			"pending creates must ask for a short retry: %d %q",
			response.StatusCode,
			response.Header.Get("Retry-After"),
		)
	}
	if _, err := harness.store.RateHit(
		t.Context(),
		[]store.RateHit{
			{Key: globalStoredKey, WindowStart: day, PurgeAt: day.Add(48 * time.Hour)},
			{Key: globalStoredKey, WindowStart: day, PurgeAt: day.Add(48 * time.Hour)},
		},
	); err != nil {
		t.Fatal(err)
	}
	response, _ = harness.create(message)
	if retry, _ := strconv.Atoi(
		response.Header.Get("Retry-After"),
	); response.StatusCode != http.StatusServiceUnavailable ||
		retry < 3600 {
		t.Fatalf(
			"a cap full of stored messages must point to the next day: %d %q",
			response.StatusCode,
			response.Header.Get("Retry-After"),
		)
	}
}

type staleGateStore struct{ store.Store }

func (s staleGateStore) RateGet(ctx context.Context, key string, ws time.Time) (store.RateCount, error) {
	if key == globalCreatesKey {
		return store.RateCount{}, nil
	}
	return s.Store.RateGet(ctx, key, ws)
}

func TestSlotPastHalfTheCapNeedsPoW(t *testing.T) {
	t.Parallel()
	settings := baseSettings(pepperA, nil)
	settings.Rates.GlobalDailyCreates = 4
	harness := newHarness(t, settings)
	harness.mustCreate(newMessage(t, core.KDFNone, "", 1, 3600, "a"))
	harness.mustCreate(newMessage(t, core.KDFNone, "", 1, 3600, "b"))
	inner := harness.store
	harness.store = staleGateStore{Store: inner}
	harness.reopen(&harness.settings)
	if response, body := harness.create(
		newMessage(t, core.KDFNone, "", 1, 3600, "c"),
	); response.StatusCode != http.StatusForbidden ||
		decodeJSON(t, body)["error"] != "pow" {
		t.Fatalf("the third of four slots was taken without PoW: %d %s", response.StatusCode, body)
	}
	day := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	if c, err := inner.RateGet(t.Context(), globalCreatesKey, day); err != nil || c.Count != 2 {
		t.Fatalf("the refused create must return its slot: %+v %v", c, err)
	}
}

type fillingStore struct {
	store.Store

	once sync.Once
	fill func()
}

func (s *fillingStore) RateGet(ctx context.Context, key string, ws time.Time) (store.RateCount, error) {
	c, err := s.Store.RateGet(ctx, key, ws)
	if key == globalCreatesKey {
		s.once.Do(s.fill)
	}
	return c, err
}

func TestReservationLostAfterTheGateAnswers503(t *testing.T) {
	t.Parallel()
	settings := baseSettings(pepperA, nil)
	settings.Rates.GlobalDailyCreates = 1
	harness := newHarness(t, settings)
	day := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	inner := harness.store
	harness.store = &fillingStore{Store: inner, fill: func() {
		if _, ok, err := inner.RateReserve(
			t.Context(),
			store.RateHit{Key: globalCreatesKey, WindowStart: day, PurgeAt: day.Add(48 * time.Hour), Limit: 1},
		); err != nil ||
			!ok {
			t.Error(ok, err)
		}
	}}
	harness.reopen(&harness.settings)
	message := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	if response, _ := harness.create(message); response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("a create that lost its reservation: %d", response.StatusCode)
	}
	if c, _ := inner.RateGet(t.Context(), globalCreatesKey, day); c.Count != 1 {
		t.Fatalf("the cap was exceeded: %+v", c)
	}
	if response, _ := harness.metadata(message); response.StatusCode != http.StatusNotFound {
		t.Fatalf("a refused create stored a row: %d", response.StatusCode)
	}
}

func TestCreateWaitsWhileTheDiskIsNearlyFull(t *testing.T) {
	t.Parallel()
	settings := baseSettings(pepperA, nil)
	settings.Rates.CreatePerHour = 2
	harness := newHarness(t, settings)
	live := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	harness.mustCreate(live)
	free := uint64(1<<30 - 1)
	harness.setFreeSpace(func(string) (uint64, error) { return free, nil })
	day := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	before, _ := harness.store.RateGet(t.Context(), globalCreatesKey, day)
	for range 3 {
		message := newMessage(t, core.KDFNone, "", 1, 3600, "n")
		message.address = "198.51.100.1"
		if response, _ := harness.create(
			message,
		); response.StatusCode != http.StatusServiceUnavailable ||
			response.Header.Get("Retry-After") != "60" {
			t.Fatalf("create on a nearly full disk: %d %q", response.StatusCode, response.Header.Get("Retry-After"))
		}
	}
	if c, _ := harness.store.RateGet(t.Context(), globalCreatesKey, day); c.Count != before.Count {
		t.Fatalf("a create refused for disk space took a global slot: %d, before %d", c.Count, before.Count)
	}
	if response, _ := harness.post(live, "burn", live.readToken); response.StatusCode != http.StatusNoContent {
		t.Fatalf("burn on a nearly full disk: %d", response.StatusCode)
	}
	if n := strings.Count(harness.logs.String(), `"msg":"disk-full"`); n != 1 {
		t.Fatalf("logged disk-full %d times, want once per change", n)
	}
	harness.setFreeSpace(func(string) (uint64, error) { return math.MaxUint64, errors.New("statfs failed") })
	message := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	message.address = "198.51.100.9"
	if response, _ := harness.create(message); response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("create when free space cannot be measured: %d", response.StatusCode)
	}
	message = newMessage(t, core.KDFNone, "", 1, 3600, "n")
	message.address = "198.51.100.1"
	free = 1<<30 + uint64(len(message.object)) - 1
	harness.setFreeSpace(func(string) (uint64, error) { return free, nil })
	if response, _ := harness.create(message); response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("create whose own upload would cross the reserve: %d", response.StatusCode)
	}
	free++
	if response, _ := harness.create(message); response.StatusCode != http.StatusCreated {
		t.Fatalf(
			"a client refused for disk space lost its rate budget or the upload did not fit: %d",
			response.StatusCode,
		)
	}
	if n := strings.Count(harness.logs.String(), `"msg":"disk-free-again"`); n != 1 {
		t.Fatalf("logged disk-free-again %d times", n)
	}

	off := baseSettings(pepperA, nil)
	off.MinFreeBytes = 0
	harness.reopen(off)
	harness.setFreeSpace(func(string) (uint64, error) { return 0, errors.New("statfs failed") })
	message = newMessage(t, core.KDFNone, "", 1, 3600, "n")
	message.address = "198.51.100.11"
	if response, _ := harness.create(message); response.StatusCode != http.StatusCreated {
		t.Fatalf("create with the guard off: %d", response.StatusCode)
	}
}

func TestAK12CreateRateLimitAndGlobalCap(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	for i := 1; i <= 20; i++ {
		message := newMessage(t, core.KDFNone, "", 1, 3600, "n")
		message.address = "203.0.113.5"
		harness.mustCreate(message)
	}
	over := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	over.address = "203.0.113.5"
	response, body := harness.create(over)
	if response.StatusCode != http.StatusTooManyRequests || response.Header.Get("Retry-After") == "" ||
		decodeJSON(t, body)["error"] != "rate_limited" {
		t.Fatalf("21st create: %d %s", response.StatusCode, body)
	}
	other := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	other.address = "198.51.100.9"
	harness.mustCreate(other)
}

func TestAK12GlobalCap(t *testing.T) {
	t.Parallel()
	settings := baseSettings(pepperA, nil)
	settings.Rates.GlobalDailyCreates = 2
	harness := newHarness(t, settings)
	readable := newMessage(t, core.KDFNone, "", 1, 3600, "read me")
	readable.address = "192.0.2.1"
	harness.mustCreate(readable)
	second := newMessage(t, core.KDFNone, "", 1, 3600, "two")
	second.address = "192.0.2.2"
	response, body := harness.create(second)
	if response.StatusCode != http.StatusForbidden || decodeJSON(t, body)["error"] != "pow" {
		t.Fatalf("at 50 percent PoW must be required: %d %s", response.StatusCode, body)
	}
	_, body = harness.do(&testRequest{method: http.MethodGet, path: "/api/v1/instance"})
	challenge := decodeJSON(t, body)["pow"].(map[string]any)["challenge"].(string)
	token := solveWork(t, challenge, second.id, harness.settings.PoWDifficulty)
	response, body = harness.do(
		&testRequest{
			method:  http.MethodPost,
			path:    "/api/v1/messages",
			body:    second.object,
			address: second.address,
			headers: map[string]string{"X-Nylzo-Create": second.createHeader(0), "X-Nylzo-Pow": token},
		},
	)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("second create with pow: %d %s", response.StatusCode, body)
	}
	third := newMessage(t, core.KDFNone, "", 1, 3600, "three")
	third.address = "192.0.2.3"
	response, body = harness.create(third)
	if response.StatusCode != http.StatusServiceUnavailable || decodeJSON(t, body)["error"] != "unavailable" ||
		response.Header.Get("Retry-After") == "" {
		t.Fatalf("over the cap: %d %s", response.StatusCode, body)
	}
	if got, _ := harness.claim(readable, "", ""); got.StatusCode != http.StatusOK {
		t.Fatalf("claim during cap: %d", got.StatusCode)
	}
	harness.reopen(settings)
	_, body = harness.do(&testRequest{method: http.MethodGet, path: "/api/v1/instance"})
	if decodeJSON(t, body)["pow"].(map[string]any)["required"] != true {
		t.Fatalf("instance after restart must still report pow: %s", body)
	}
}

func TestAK12ForwardedForOnlyFromTrustedProxy(t *testing.T) {
	t.Parallel()
	settings := baseSettings(pepperA, nil)
	settings.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	harness := newHarness(t, settings)
	for i := 1; i <= 20; i++ {
		message := newMessage(t, core.KDFNone, "", 1, 3600, "n")
		message.address = fmt.Sprintf("203.0.113.%d", i)
		harness.mustCreate(message)
	}
	spoofed := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	spoofed.address = "203.0.113.200"
	if response, _ := harness.create(spoofed); response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("spoofed X-Forwarded-For from an untrusted peer must share the peer bucket: %d", response.StatusCode)
	}
	trusted := baseSettings(pepperA, nil)
	trusted.TrustedProxies = append(trusted.TrustedProxies, netip.MustParsePrefix("10.0.0.0/8"))
	harness.reopen(trusted)
	for i := 1; i <= 25; i++ {
		message := newMessage(t, core.KDFNone, "", 1, 3600, "n")
		message.address = fmt.Sprintf("198.51.100.%d, 10.1.1.1", i)
		harness.mustCreate(message)
	}
}

func TestAK23PrefixBuckets(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	for i := 1; i <= 20; i++ {
		message := newMessage(t, core.KDFNone, "", 1, 3600, "n")
		message.address = fmt.Sprintf("2001:db8:1:2::%x", i)
		harness.mustCreate(message)
	}
	sameSlash64 := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	sameSlash64.address = "2001:db8:1:2:ffff::1"
	if response, _ := harness.create(sameSlash64); response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("second address in the same /64 must share the bucket: %d", response.StatusCode)
	}
	for sub := 3; sub <= 6; sub++ {
		for i := 1; i <= 20; i++ {
			message := newMessage(t, core.KDFNone, "", 1, 3600, "n")
			message.address = fmt.Sprintf("2001:db8:1:%d::%x", sub, i)
			harness.mustCreate(message)
		}
	}
	fifth := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	fifth.address = "2001:db8:1:7::1"
	if response, _ := harness.create(fifth); response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("101st create in the same /48 must hit the coarse bucket: %d", response.StatusCode)
	}
	otherSite := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	otherSite.address = "2001:db8:2:1::1"
	harness.mustCreate(otherSite)
	for i := 1; i <= 20; i++ {
		message := newMessage(t, core.KDFNone, "", 1, 3600, "n")
		message.address = fmt.Sprintf("203.0.113.%d", i)
		harness.mustCreate(message)
	}
	for i := 1; i <= 80; i++ {
		message := newMessage(t, core.KDFNone, "", 1, 3600, "n")
		message.address = fmt.Sprintf("203.0.113.%d", 20+i)
		harness.mustCreate(message)
	}
	v4 := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	v4.address = "203.0.113.250"
	if response, _ := harness.create(v4); response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("101st create in the same /24: %d", response.StatusCode)
	}
}

func TestRefusedSourceDoesNotSpendItsRange(t *testing.T) {
	t.Parallel()
	settings := baseSettings(pepperA, nil)
	settings.Rates.CreatePerHour = 1
	settings.Rates.CoarseMultiplier = 2
	harness := newHarness(t, settings)
	for i := range 4 {
		message := newMessage(t, core.KDFNone, "", 1, 3600, "n")
		message.address = "198.51.100.1"
		response, _ := harness.create(message)
		if want := map[bool]int{true: 201, false: 429}[i == 0]; response.StatusCode != want {
			t.Fatalf("create %d from one source: %d, want %d", i+1, response.StatusCode, want)
		}
	}
	neighbour := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	neighbour.address = "198.51.100.2"
	if response, body := harness.create(neighbour); response.StatusCode != http.StatusCreated {
		t.Fatalf("a refused source must not use up its /24: %d %s", response.StatusCode, body)
	}
	third := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	third.address = "198.51.100.3"
	if response, _ := harness.create(third); response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("the /24 budget still applies: %d", response.StatusCode)
	}
}

func TestRetryAfterPointsToTheRefusingWindow(t *testing.T) {
	t.Parallel()
	settings := baseSettings(pepperA, nil)
	settings.Rates.CreatePerHour = 1
	harness := newHarness(t, settings)
	harness.mustCreate(newMessage(t, core.KDFNone, "", 1, 3600, "a"))
	response, body := harness.create(newMessage(t, core.KDFNone, "", 1, 3600, "b"))
	if response.StatusCode != http.StatusTooManyRequests || response.Header.Get("Retry-After") != "3600" ||
		decodeJSON(t, body)["retry_after"] != float64(3600) {
		t.Fatalf(
			"an hourly refusal at 10:00 must point to 11:00: %d %q %s",
			response.StatusCode,
			response.Header.Get("Retry-After"),
			body,
		)
	}
	response, _ = harness.do(
		&testRequest{
			method:  http.MethodGet,
			path:    "/api/v1/instance",
			headers: map[string]string{"Sec-Purpose": "prefetch"},
		},
	)
	if response.StatusCode != http.StatusServiceUnavailable || response.Header.Get("Retry-After") != "60" {
		t.Fatalf("prefetch: %d %q", response.StatusCode, response.Header.Get("Retry-After"))
	}
}

func TestPoWTokenIsSingleUse(t *testing.T) {
	t.Parallel()
	settings := baseSettings(pepperA, nil)
	settings.Rates.GlobalDailyCreates = 4
	settings.PoWDifficulty = 1
	harness := newHarness(t, settings)
	for i := range 2 {
		message := newMessage(t, core.KDFNone, "", 1, 3600, "n")
		message.address = fmt.Sprintf("203.0.113.%d", i+1)
		harness.mustCreate(message)
	}
	solve := func(message *testMessage) string {
		_, body := harness.do(&testRequest{method: http.MethodGet, path: "/api/v1/instance"})
		token := solveWork(t, decodeJSON(t, body)["pow"].(map[string]any)["challenge"].(string), message.id, 1)
		return token
	}
	message := newMessage(t, core.KDFNone, "", 1, 3600, "pow")
	message.address = "203.0.113.9"
	token := solve(message)
	badHeader := mutateHeader(
		t,
		message,
		func(f map[string]any) { f["kdf_id"] = 1; f["verifier_key"] = publicKey(core.NewSecret()) },
	)
	send := func(header, pow string) int {
		response, _ := harness.do(
			&testRequest{
				method:  http.MethodPost,
				path:    "/api/v1/messages",
				body:    message.object,
				address: message.address,
				headers: map[string]string{"X-Nylzo-Create": header, "X-Nylzo-Pow": pow},
			},
		)
		return response.StatusCode
	}
	if code := send(badHeader, token); code != http.StatusBadRequest {
		t.Fatalf("first use with a mismatched body: %d", code)
	}
	if code := send(message.createHeader(0), token); code != http.StatusForbidden {
		t.Fatalf("replayed token: %d", code)
	}
	day := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	if c, _ := harness.store.RateGet(t.Context(), globalCreatesKey, day); c.Count != 2 {
		t.Fatalf("failed and replayed creates moved the global counter: %+v", c)
	}
	other := newMessage(t, core.KDFNone, "", 1, 3600, "pow")
	other.address = "203.0.113.10"
	challenge, _, _ := strings.Cut(token, ".")
	reuse := solveWork(t, challenge, other.id, 1)
	if response, _ := harness.do(
		&testRequest{
			method:  http.MethodPost,
			path:    "/api/v1/messages",
			body:    other.object,
			address: other.address,
			headers: map[string]string{"X-Nylzo-Create": other.createHeader(0), "X-Nylzo-Pow": reuse},
		},
	); response.StatusCode != http.StatusForbidden {
		t.Fatalf("a second token for a used challenge: %d", response.StatusCode)
	}
	if code := send(message.createHeader(0), solve(message)); code != http.StatusCreated {
		t.Fatalf("fresh challenge: %d", code)
	}
}

type failingCreateStore struct{ store.Store }

func (failingCreateStore) Create(context.Context, *store.Message, ...store.RateHit) error {
	return errInjected
}

func TestFailedRowWriteRemovesTheUploadedBlob(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	harness.store = failingCreateStore{Store: harness.store}
	harness.reopen(&harness.settings)
	message := newMessage(t, core.KDFNone, "", 1, 3600, "orphan")
	if response, _ := harness.create(message); response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("create with a failing row write: %d", response.StatusCode)
	}
	if harness.blobExists(message.id) {
		t.Fatal("the ciphertext of a create that stored no row stays in the blob store")
	}
}

func TestDailyCreateLimits(t *testing.T) {
	t.Parallel()
	settings := baseSettings(pepperA, nil)
	settings.Rates.CreatePerHour = 1000
	settings.Rates.CreatePerDay = 2
	harness := newHarness(t, settings)
	create := func(ip string) int {
		message := newMessage(t, core.KDFNone, "", 1, 3600, "n")
		message.address = ip
		response, _ := harness.create(message)
		return response.StatusCode
	}
	for range 2 {
		if code := create("203.0.113.1"); code != http.StatusCreated {
			t.Fatalf("create within the daily limit: %d", code)
		}
	}
	if code := create("203.0.113.1"); code != http.StatusTooManyRequests {
		t.Fatalf("a third create in one day from one /32: %d", code)
	}
	for i := 2; i <= 5; i++ {
		for range 2 {
			if code := create(fmt.Sprintf("203.0.113.%d", i)); code != http.StatusCreated {
				t.Fatalf("create from 203.0.113.%d: %d", i, code)
			}
		}
	}
	if code := create("203.0.113.6"); code != http.StatusTooManyRequests {
		t.Fatalf("a create past the daily count of its /24: %d", code)
	}
	if code := create("198.51.100.1"); code != http.StatusCreated {
		t.Fatalf("another /24 must keep its own budget: %d", code)
	}
}

func TestRangeDailyBytesLimit(t *testing.T) {
	t.Parallel()
	settings := baseSettings(pepperA, nil)
	harness := newHarness(t, settings)
	probe := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	settings.Rates.CreateBytesPerDay = int64(2*len(probe.object) + 1)
	harness.reopen(settings)
	for source := 1; source <= 5; source++ {
		for range 2 {
			message := newMessage(t, core.KDFNone, "", 1, 3600, "n")
			message.address = fmt.Sprintf("203.0.113.%d", source)
			harness.mustCreate(message)
		}
	}
	message := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	message.address = "203.0.113.6"
	if response, _ := harness.create(message); response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("a create past the daily bytes of its /24: %d", response.StatusCode)
	}
}

func TestDegenerateKeysAreRejectedAtCreate(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	identity := core.Encode(append([]byte{1}, make([]byte, 31)...))
	order4 := core.Encode(make([]byte, 32))
	nonCanonical := func() string {
		b := make([]byte, 32)
		for i := range b {
			b[i] = 0xff
		}
		b[0], b[31] = 0xee, 0x7f
		return core.Encode(b)
	}()
	none := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	pw := newMessage(t, core.KDFScrypt, "correct horse battery staple", 1, 3600, "x")
	for name, header := range map[string]string{
		"identity read key":      mutateHeader(t, none, func(f map[string]any) { f["read_key"] = identity }),
		"small-order manage key": mutateHeader(t, none, func(f map[string]any) { f["manage_key"] = order4 }),
		"non-canonical read key": mutateHeader(t, none, func(f map[string]any) { f["read_key"] = nonCanonical }),
		"identity verifier key":  mutateHeader(t, pw, func(f map[string]any) { f["verifier_key"] = identity }),
		"manage key equals read": mutateHeader(t, none, func(f map[string]any) { f["manage_key"] = f["read_key"] }),
		"verifier equals read":   mutateHeader(t, pw, func(f map[string]any) { f["verifier_key"] = f["read_key"] }),
		"verifier equals manage": mutateHeader(t, pw, func(f map[string]any) { f["verifier_key"] = f["manage_key"] }),
	} {
		message := none
		if strings.Contains(name, "verifier") {
			message = pw
		}
		response, _ := harness.do(
			&testRequest{
				method:  http.MethodPost,
				path:    "/api/v1/messages",
				body:    message.object,
				headers: map[string]string{"Content-Type": "application/octet-stream", "X-Nylzo-Create": header},
			},
		)
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: %d", name, response.StatusCode)
		}
	}
	for _, message := range []*testMessage{none, pw} {
		if _, err := harness.store.Get(t.Context(), message.id.String()); err == nil {
			t.Fatalf("a rejected create stored %s", message.id)
		}
	}
	harness.mustCreate(none)
}

func TestConcurrentCreateWithSameID(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	for i := range 20 {
		message := newMessage(t, core.KDFNone, "", 1, 3600, "race")
		message.address = fmt.Sprintf("198.51.100.%d", i+1)
		var wg sync.WaitGroup
		codes := make([]int, 2)
		for j := range codes {
			wg.Go(func() { codes[j] = harness.statusFrom(createRequestFor(message)) })
		}
		wg.Wait()
		if !slices.Equal(slices.Sorted(slices.Values(codes)), []int{201, 409}) {
			t.Fatalf("iteration %d: %v", i, codes)
		}
		response, object := harness.claim(message, "", "")
		if response.StatusCode != http.StatusOK {
			t.Fatalf("iteration %d: claim after the race: %d", i, response.StatusCode)
		}
		if _, err := harness.openObject(message, object, response.Header.Get("X-Nylzo-Policy")); err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
	}
}

func TestTruncatedCreateStoresNothing(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	message := newMessage(t, core.KDFNone, "", 1, 3600, "short")
	conn, err := net.Dial("tcp", harness.listen())
	if err != nil {
		t.Fatal(err)
	}
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprintf(
		conn,
		"POST /api/v1/messages HTTP/1.1\r\nHost: nylzo\r\nContent-Length: %d\r\nX-Nylzo-Create: %s\r\n\r\n",
		len(message.object),
		harness.withIDToken(message.createHeader(0)),
	)
	conn.Write(message.object[:100])
	conn.(*net.TCPConn).CloseWrite()
	line, _ := bufio.NewReader(conn).ReadString('\n')
	conn.Close()
	if !strings.Contains(line, " 400 ") {
		t.Fatalf("status line %q", line)
	}
	if harness.blobExists(message.id) {
		t.Fatal("truncated body stored")
	}
	if response, _ := harness.metadata(message); response.StatusCode != http.StatusNotFound {
		t.Fatal("truncated body created a row")
	}
}

func TestCreateStorageFailureAnswers500(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	faulty := harness.withFaultyBlobs()
	faulty.set(func(b *faultyBlobs) { b.failPut = errInjected })
	message := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	if response, _ := harness.create(message); response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("storage failure: %d", response.StatusCode)
	}
	if response, _ := harness.metadata(message); response.StatusCode != http.StatusNotFound {
		t.Fatal("a failed Put created a row")
	}
}

func TestStorageConflictMatchesEarlyConflict(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	existing := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	harness.mustCreate(existing)
	early, earlyBody := harness.create(existing)
	faulty := harness.withFaultyBlobs()
	faulty.set(func(b *faultyBlobs) { b.failPut = store.ErrConflict })
	late, lateBody := harness.create(newMessage(t, core.KDFNone, "", 1, 3600, "x"))
	if early.StatusCode != http.StatusConflict || late.StatusCode != http.StatusConflict || late.Close != early.Close ||
		!bytes.Equal(earlyBody, lateBody) {
		t.Fatalf("early %d close=%v, late %d close=%v", early.StatusCode, early.Close, late.StatusCode, late.Close)
	}
}

type slowPutBlobs struct {
	store.Blobs

	clock *fakeClock
	delay time.Duration
}

func (b slowPutBlobs) Put(ctx context.Context, ref string, r io.Reader, size int64) error {
	b.clock.Advance(b.delay)
	return b.Blobs.Put(ctx, ref, r, size)
}

func TestExpiryCountsFromTheStoredUpload(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	harness.blobs = slowPutBlobs{Blobs: harness.blobs, clock: harness.clock, delay: 90 * time.Second}
	harness.reopen(&harness.settings)
	message := newMessage(t, core.KDFNone, "", 1, 60, "x")
	harness.mustCreate(message)
	if response, metadata := harness.metadata(
		message,
	); response.StatusCode != http.StatusOK ||
		metadata["state"] != "available" {
		t.Fatalf("a slow upload must not use up the TTL: %d %v", response.StatusCode, metadata)
	}
}

func TestStoredTimesAreWholeMinutes(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	harness.clock.Advance(17*time.Second + 123*time.Millisecond)
	message := newMessage(t, core.KDFNone, "", 2, 3600, "minute")
	response, body := harness.create(message)
	if response.StatusCode != http.StatusCreated || decodeJSON(t, body)["expires_at"] != "2026-10-22T11:01:00Z" {
		t.Fatalf("expires_at must be rounded up to the full minute: %d %s", response.StatusCode, body)
	}
	if got, _ := harness.claim(message, "", ""); got.StatusCode != http.StatusOK {
		t.Fatalf("claim: %d", got.StatusCode)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(harness.dir, "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var created, expires, lease int64
	if queryRowErr := db.QueryRow(`SELECT m.created_at, m.expires_at, l.lease_expires_at FROM messages m JOIN leases l ON l.id = m.id WHERE m.id = ?`, message.id.String()).
		Scan(&created, &expires, &lease); queryRowErr != nil {
		t.Fatal(queryRowErr)
	}
	minute := time.Minute.Milliseconds()
	if created%minute != 0 || expires%minute != 0 || lease%minute != 0 {
		t.Fatalf("stored times must be whole minutes: created %d, expires %d, lease %d", created, expires, lease)
	}
	if created != time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC).UnixMilli() ||
		lease < harness.clock.Now().Add(store.LeaseDuration).UnixMilli() {
		t.Fatalf("created_at rounds down, the lease rounds up: created %d, lease %d", created, lease)
	}
}

type cancelAfterCommit struct{ store.Store }

func (s cancelAfterCommit) RateReserve(ctx context.Context, hit store.RateHit) (int64, bool, error) {
	n, ok, err := s.Store.RateReserve(context.WithoutCancel(ctx), hit)
	if ctx.Err() != nil {
		return 0, false, ctx.Err()
	}
	return n, ok, err
}

func TestReserveKeepsItsSlotWhenTheRequestIsCanceled(t *testing.T) {
	t.Parallel()
	database, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	s := &Server{
		settings: config.Server{Rates: config.Rates{GlobalDailyCreates: 3}},
		store:    cancelAfterCommit{database},
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if slot, reserveGlobalCreateErr := s.reserveGlobalCreate(
		ctx,
		time.Unix(1_800_000_000, 0),
	); reserveGlobalCreateErr != nil || !slot.reserved ||
		slot.number != 1 {
		t.Fatalf("a canceled request must still learn about the slot it reserved: %+v %v", slot, reserveGlobalCreateErr)
	}
}

func parsesCreateHeader(limits config.Limits, raw string) bool {
	_, ok := parseCreateHeader(raw, limits)
	return ok
}

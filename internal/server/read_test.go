package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/store"
)

func TestFlowCreateMetadataClaimBlobAck(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	message := newMessage(t, core.KDFNone, "", 1, 86400, "hello")
	harness.mustCreate(message)
	for _, ua := range []string{"", "Slackbot-LinkExpanding 1.0"} {
		harness.do(
			&testRequest{
				method:  http.MethodGet,
				path:    "/m/" + message.id.String(),
				headers: map[string]string{"User-Agent": ua},
			},
		)
	}
	for range 5 {
		response, metadata := harness.metadata(message)
		if response.StatusCode != http.StatusOK || metadata["opens_remaining"].(float64) != 1 ||
			metadata["state"] != "available" ||
			metadata["passphrase_required"] != false ||
			metadata["kdf_id"].(float64) != 0 {
			t.Fatalf("metadata %d: %v", response.StatusCode, metadata)
		}
	}
	response, object := harness.claim(message, "", "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("claim: %d %s", response.StatusCode, object)
	}
	lease := response.Header.Get("X-Nylzo-Lease")
	policy := response.Header.Get("X-Nylzo-Policy")
	if lease == "" || policy == "" || response.Header.Get("Content-Type") != "application/octet-stream" {
		t.Fatal("claim response headers")
	}
	payload, err := harness.openObject(message, object, policy)
	if err != nil || payload.Meta.Text == nil || *payload.Meta.Text != "hello" {
		t.Fatalf("decrypt: %v", err)
	}
	decoded, err := decodePolicy(policy)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []core.Policy{
		{OpensMax: decoded.OpensMax + 1, TTLSeconds: decoded.TTLSeconds},
		{OpensMax: decoded.OpensMax, TTLSeconds: decoded.TTLSeconds - 1},
	} {
		if _, _, openErr := core.Open(
			&message.content.K,
			message.id,
			bad,
			message.kdf,
			object,
		); !errors.Is(
			openErr,
			core.ErrIntegrity,
		) {
			t.Fatalf("changed policy %+v: want ErrIntegrity, got %v", bad, openErr)
		}
	}
	_, metadata := harness.metadata(message)
	if metadata["state"] != "opened" || metadata["opens_remaining"].(float64) != 0 ||
		metadata["first_opened_at"] == nil {
		t.Fatalf("metadata after claim: %v", metadata)
	}
	for i := range 3 {
		got, again := harness.blob(message, lease)
		if got.StatusCode != http.StatusOK || string(again) != string(object) {
			t.Fatalf("blob %d: %d", i, got.StatusCode)
		}
	}
	if got, _ := harness.blob(message, lease); got.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("fourth download: %d", got.StatusCode)
	}
	if got, _ := harness.blob(message, core.Encode(make([]byte, 32))); got.StatusCode != http.StatusGone {
		t.Fatalf("bogus lease: %d", got.StatusCode)
	}
	if harness.pendingTimers() != 1 {
		t.Fatal("lease timer not scheduled")
	}
	if got := harness.ack(message, lease); got.StatusCode != http.StatusNoContent {
		t.Fatalf("ack: %d", got.StatusCode)
	}
	if harness.blobExists(message.id) {
		t.Fatal("blob survived the last ack")
	}
	if got, _ := harness.blob(message, lease); got.StatusCode != http.StatusGone {
		t.Fatalf("blob after ack: %d", got.StatusCode)
	}
	response, metadata = harness.metadata(message)
	if response.StatusCode != http.StatusOK || metadata["state"] != "opened" || metadata["ended_at"] == nil {
		t.Fatalf("tombstone metadata: %v", metadata)
	}
	if got, _ := harness.claim(message, "", ""); got.StatusCode != http.StatusGone {
		t.Fatalf("claim after end: %d", got.StatusCode)
	}
	response, status := harness.status(message)
	if response.StatusCode != http.StatusOK || status["state"] != "opened" {
		t.Fatalf("status: %v", status)
	}
	harness.clock.Advance(store.LeaseDuration + 2*time.Second)
	harness.fireTimers()
}

func TestAK4ConcurrentClaimsHTTP(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	settings := harness.settings
	settings.Rates.ReadPerHour = 100000
	settings.Rates.CreatePerHour = 100000
	settings.Rates.CreatePerDay = 100000
	settings.Rates.BucketPerSecond = 100000
	settings.Rates.BucketBurst = 100000
	harness.reopen(&settings)
	for i := range 100 {
		message := newMessage(t, core.KDFNone, "", 1, 3600, "race")
		harness.mustCreate(message)
		var wg sync.WaitGroup
		codes := make([]int, 2)
		for j := range codes {
			wg.Go(func() { codes[j] = harness.statusFrom(claimRequestFor(message, "", "")) })
		}
		wg.Wait()
		if !slices.Equal(slices.Sorted(slices.Values(codes)), []int{200, 410}) {
			t.Fatalf("iteration %d: %v", i, codes)
		}
	}
}

func TestAK5PassphrasePolicyHTTP(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	settings := harness.settings
	settings.Rates.ReadPerHour = 100000
	harness.reopen(&settings)
	message := newMessage(t, core.KDFScrypt, "correct", 2, 604800, "guarded")
	message.address = "203.0.113.10"
	harness.mustCreate(message)
	wrong := core.Encode(make([]byte, 32))
	expect := func(ip string, retry, left float64) {
		t.Helper()
		response, body := harness.claim(message, wrong, ip)
		j := decodeJSON(t, body)
		if response.StatusCode != http.StatusForbidden || j["error"] != "passphrase" ||
			j["retry_after"].(float64) != retry ||
			j["attempts_left"].(float64) != left {
			t.Fatalf("got %d %s, want retry %v left %v", response.StatusCode, body, retry, left)
		}
	}
	for i := 1; i <= 5; i++ {
		expect("203.0.113.10", 0, float64(5-i))
	}
	expect("203.0.113.10", 60, 0)
	for range 4 {
		harness.clock.Advance(time.Minute)
		expect("203.0.113.10", 60, 0)
	}
	_, metadata := harness.metadata(message)
	if metadata["opens_remaining"].(float64) != 2 {
		t.Fatalf("wrong passphrase consumed an open: %v", metadata)
	}
	if response, _ := harness.claim(
		message,
		message.verifier,
		"203.0.113.99",
	); response.StatusCode != http.StatusForbidden {
		t.Fatalf("the backoff belongs to the message, so another address waits too: %d", response.StatusCode)
	}
	harness.clock.Advance(time.Minute)
	response, _ := harness.claim(message, message.verifier, "203.0.113.99")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("correct passphrase after the wait: %d", response.StatusCode)
	}
	_, status := harness.status(message)
	if status["failed_passphrase_attempts"].(float64) != 10 || status["opens_used"].(float64) != 1 {
		t.Fatalf("status: %v", status)
	}
	for i := range 90 {
		got, body := harness.claim(message, wrong, "198.51.100."+strconv.Itoa(i%250))
		if got.StatusCode != http.StatusForbidden {
			t.Fatalf("flood attempt %d: %d", i, got.StatusCode)
		}
		harness.clock.Advance(time.Duration(decodeJSON(t, body)["retry_after"].(float64)) * time.Second)
	}
	response, body := harness.claim(message, message.verifier, "192.0.2.77")
	j := decodeJSON(t, body)
	if response.StatusCode != http.StatusForbidden || j["retry_after"].(float64) <= 0 ||
		j["attempts_left"].(float64) != 0 {
		t.Fatalf("100 failed attempts must lock the message for every address: %d %s", response.StatusCode, body)
	}
	harness.clock.Advance(24 * time.Hour)
	if got, _ := harness.claim(
		message,
		message.verifier,
		"192.0.2.78",
	); got.StatusCode != http.StatusForbidden {
		t.Fatalf("the lock must outlast the UTC day: %d", got.StatusCode)
	}
	_, status = harness.status(message)
	if status["failed_passphrase_attempts"].(float64) != 100 {
		t.Fatalf("status after cap: %v", status)
	}
	fresh := newMessage(t, core.KDFScrypt, "pw", 1, 3600, "x")
	harness.mustCreate(fresh)
	response, body = harness.claim(fresh, "", "192.0.2.78")
	j = decodeJSON(t, body)
	if response.StatusCode != http.StatusForbidden || j["attempts_left"].(float64) != 4 ||
		j["retry_after"].(float64) != 0 {
		t.Fatalf("missing verifier: %d %s", response.StatusCode, body)
	}
	if _, statusNow := harness.status(fresh); statusNow["failed_passphrase_attempts"].(float64) != 1 {
		t.Fatalf("status after missing verifier: %v", statusNow)
	}
}

func TestAK6LeaseExpiryWithoutAck(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	settings := harness.settings
	settings.Rates.ReadPerHour = 100000
	harness.reopen(&settings)
	message := newMessage(t, core.KDFNone, "", 3, 86400, "three")
	harness.mustCreate(message)
	var leases []string
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i := range 3 {
		wg.Go(func() {
			response, _, err := harness.tryDo(claimRequestFor(message, "", fmt.Sprintf("198.51.100.%d", 20+i)))
			if err != nil {
				t.Error(err)
				return
			}
			if response.StatusCode != http.StatusOK {
				t.Errorf("claim: %d", response.StatusCode)
				return
			}
			mu.Lock()
			leases = append(leases, response.Header.Get("X-Nylzo-Lease"))
			mu.Unlock()
		})
	}
	wg.Wait()
	distinct := map[string]bool{}
	for _, l := range leases {
		distinct[l] = true
	}
	if len(distinct) != 3 {
		t.Fatalf("leases not distinct: %v", leases)
	}
	harness.clock.Advance(store.LeaseDuration - time.Second)
	for _, l := range leases {
		if response, _ := harness.blob(message, l); response.StatusCode != http.StatusOK {
			t.Fatalf("blob within lease: %d", response.StatusCode)
		}
	}
	harness.clock.Advance(time.Second)
	if !harness.blobExists(message.id) {
		t.Fatal("blob deleted before any request, timer or sweep")
	}
	harness.clock.Advance(store.LeaseTimerSlack)
	if fired := harness.fireTimers(); fired != 1 {
		t.Fatalf("timers fired: %d", fired)
	}
	if harness.blobExists(message.id) {
		t.Fatal("timer did not delete the blob")
	}
	for _, l := range leases {
		if response, _ := harness.blob(message, l); response.StatusCode != http.StatusGone {
			t.Fatalf("blob after lease expiry: %d", response.StatusCode)
		}
	}
	_, metadata := harness.metadata(message)
	if metadata["state"] != "opened" {
		t.Fatalf("metadata: %v", metadata)
	}
}

func TestAK6RequestAfterLeaseExpiryReleasesBlob(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	message := newMessage(t, core.KDFNone, "", 1, 86400, "one")
	harness.mustCreate(message)
	response, _ := harness.claim(message, "", "")
	if response.StatusCode != http.StatusOK {
		t.Fatal("claim")
	}
	lease := response.Header.Get("X-Nylzo-Lease")
	harness.clock.Advance(store.LeaseDuration)
	if got, _ := harness.blob(message, lease); got.StatusCode != http.StatusGone {
		t.Fatalf("blob at expiry: %d", got.StatusCode)
	}
	if harness.blobExists(message.id) {
		t.Fatal("request after the last lease expired must release the blob")
	}
	if harness.pendingTimers() != 1 {
		t.Fatal("timer should still be pending")
	}
	harness.clock.Advance(store.LeaseTimerSlack)
	harness.fireTimers()
}

func TestAK6SweepAfterRestart(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	message := newMessage(t, core.KDFNone, "", 1, 86400, "restart")
	harness.mustCreate(message)
	if response, _ := harness.claim(message, "", ""); response.StatusCode != http.StatusOK {
		t.Fatal("claim")
	}
	harness.reopen(&harness.settings)
	harness.clock.Advance(store.LeaseDuration + time.Second)
	if !harness.blobExists(message.id) {
		t.Fatal("blob gone before sweep")
	}
	report, err := harness.server.SweepOnce(t.Context())
	if err != nil || report.BlobsReleased != 1 {
		t.Fatalf("sweep: %+v %v", report, err)
	}
	if harness.blobExists(message.id) {
		t.Fatal("sweep left the blob")
	}
}

func TestAK7ExpiryLazyAndSweep(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	message := newMessage(t, core.KDFNone, "", 1, 60, "short")
	harness.mustCreate(message)
	harness.clock.Advance(61 * time.Second)
	response, metadata := harness.metadata(message)
	if response.StatusCode != http.StatusOK || metadata["state"] != "expired" {
		t.Fatalf("lazy expiry: %d %v", response.StatusCode, metadata)
	}
	if harness.blobExists(message.id) {
		t.Fatal("expired blob kept")
	}
	if got, _ := harness.claim(message, "", ""); got.StatusCode != http.StatusGone {
		t.Fatalf("claim expired: %d", got.StatusCode)
	}
	secondMessage := newMessage(t, core.KDFNone, "", 1, 60, "swept")
	harness.mustCreate(secondMessage)
	harness.clock.Advance(2 * time.Minute)
	report, err := harness.server.SweepOnce(t.Context())
	if err != nil || report.Expired != 1 || harness.blobExists(secondMessage.id) {
		t.Fatalf("sweep: %+v %v", report, err)
	}
	harness.clock.Advance(7 * 24 * time.Hour)
	if _, sweepOnceErr := harness.server.SweepOnce(t.Context()); sweepOnceErr != nil {
		t.Fatal(sweepOnceErr)
	}
	if got, _ := harness.metadata(message); got.StatusCode != http.StatusNotFound {
		t.Fatalf("after tombstone purge: %d", got.StatusCode)
	}
	if got, body := createWithToken(
		t,
		harness,
		message,
		nil,
	); got.StatusCode != http.StatusForbidden ||
		decodeJSON(t, body)["error"] != "id_token" {
		t.Fatalf(
			"an ended id must not be registered again without a token the server issued: %d %s",
			got.StatusCode,
			body,
		)
	}
}

func TestAckRefusesALeaseThatIsNotActive(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	message := newMessage(t, core.KDFNone, "", 1, 3600, "ack")
	harness.mustCreate(message)
	response, _ := harness.claim(message, "", "")
	lease := response.Header.Get("X-Nylzo-Lease")
	if response.StatusCode != http.StatusOK || lease == "" {
		t.Fatalf("claim: %d", response.StatusCode)
	}
	if code := harness.ack(message, core.Encode(core.NewSecret()[:])).StatusCode; code != http.StatusGone {
		t.Fatalf("an ack with a lease the server never issued: %d", code)
	}
	if !harness.blobExists(message.id) {
		t.Fatal("an ack with a foreign lease deleted the blob")
	}
	harness.clock.Advance(store.LeaseDuration + time.Minute)
	response, body := harness.do(
		&testRequest{
			method:  http.MethodPost,
			path:    "/api/v1/messages/" + message.id.String() + "/ack",
			address: message.address,
			token:   message.readToken,
			headers: map[string]string{"X-Nylzo-Lease": lease},
		},
	)
	if response.StatusCode != http.StatusGone || decodeJSON(t, body)["state"] != "opened" {
		t.Fatalf("an ack after the lease ended: %d %s", response.StatusCode, body)
	}
}

func TestClaimBodyVariants(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	message := newMessage(t, core.KDFScrypt, "pw", 1, 3600, "x")
	harness.mustCreate(message)
	path := "/api/v1/messages/" + message.id.String() + "/claim"
	for name, request := range map[string]*testRequest{
		"malformed-json": {method: http.MethodPost, path: path, body: []byte(`{"verifier":`), token: message.readToken},
		"short-verifier": {method: http.MethodPost, path: path, token: message.readToken, verifier: message.verifier[:42]},
	} {
		response, _ := harness.do(request)
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: %d", name, response.StatusCode)
		}
	}
	if _, status := harness.status(
		message,
	); status["failed_passphrase_attempts"].(float64) != 0 ||
		status["opens_used"].(float64) != 0 {
		t.Fatalf("malformed claim bodies changed the message: %v", status)
	}
	r, _ := http.NewRequest(
		"POST",
		harness.httpServer.URL+path,
		io.MultiReader(strings.NewReader(`{"verifier":"`+message.verifier+`"}`)),
	)
	r.Header.Set(
		"Authorization",
		core.SignRequest(&message.link.R, core.PurposeRead, "POST", path, harness.challenge(), nil).Authorization(),
	)
	r.Header.Set("X-Forwarded-For", "203.0.113.4")
	response, err := harness.client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest || r.ContentLength != 0 {
		t.Fatalf("claim with a chunked body: %d (content length %d)", response.StatusCode, r.ContentLength)
	}
	if _, status := harness.status(message); status["opens_used"].(float64) != 0 {
		t.Fatalf("a chunked claim body spent an open: %v", status)
	}
}

func TestDownloadAfterExpiryWithAStuckDeleteIsGone(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	faulty := harness.withFaultyBlobs()
	message := newMessage(t, core.KDFNone, "", 2, 60, "expires under an active lease")
	harness.mustCreate(message)
	response, _ := harness.claim(message, "", "")
	lease := response.Header.Get("X-Nylzo-Lease")
	if response.StatusCode != http.StatusOK || lease == "" {
		t.Fatalf("claim: %d", response.StatusCode)
	}
	faulty.set(func(b *faultyBlobs) { b.failDelete[message.id.String()] = true })
	harness.clock.Advance(61 * time.Second)
	if got, body := harness.blob(
		message,
		lease,
	); got.StatusCode != http.StatusGone ||
		decodeJSON(t, body)["state"] != "expired" {
		t.Fatalf("a download after the expiry while the blob cannot be deleted: %d %s", got.StatusCode, body)
	}
	if code := harness.ack(message, lease).StatusCode; code != http.StatusGone {
		t.Fatalf("an ack after the expiry while the blob cannot be deleted: %d", code)
	}
}

func TestFirstOpenedAtKeepsTheFirstClaim(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	message := newMessage(t, core.KDFNone, "", 2, 3600, "opened twice")
	harness.mustCreate(message)
	if response, _ := harness.claim(message, "", ""); response.StatusCode != http.StatusOK {
		t.Fatalf("first claim: %d", response.StatusCode)
	}
	_, first := harness.status(message)
	harness.clock.Advance(2 * time.Minute)
	response, _ := harness.claim(message, "", "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("second claim: %d", response.StatusCode)
	}
	_, second := harness.status(message)
	if first["first_opened_at"] == nil || second["first_opened_at"] != first["first_opened_at"] {
		t.Fatalf("first_opened_at moved from %v to %v", first["first_opened_at"], second["first_opened_at"])
	}
	if code := harness.ack(message, response.Header.Get("X-Nylzo-Lease")).StatusCode; code != http.StatusNoContent {
		t.Fatalf("ack: %d", code)
	}
	if _, ended := harness.status(message); ended["first_opened_at"] != first["first_opened_at"] {
		t.Fatalf(
			"the tombstone moved first_opened_at from %v to %v",
			first["first_opened_at"],
			ended["first_opened_at"],
		)
	}
}

func TestBlobCarriesThePolicyOfTheClaim(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	message := newMessage(t, core.KDFNone, "", 1, 3600, "resume")
	harness.mustCreate(message)
	response, _ := harness.claim(message, "", "")
	policy := response.Header.Get("X-Nylzo-Policy")
	if response.StatusCode != http.StatusOK || policy == "" {
		t.Fatalf("claim: %d policy=%q", response.StatusCode, policy)
	}
	again, object := harness.blob(message, response.Header.Get("X-Nylzo-Lease"))
	if again.StatusCode != http.StatusOK || again.Header.Get("X-Nylzo-Policy") != policy {
		t.Fatalf("blob: %d policy=%q, want %q", again.StatusCode, again.Header.Get("X-Nylzo-Policy"), policy)
	}
	if _, err := harness.openObject(message, object, again.Header.Get("X-Nylzo-Policy")); err != nil {
		t.Fatalf("the blob does not open with its own policy: %v", err)
	}
}

func TestAckRefusesAnExpiredLeaseWhileOpensRemain(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	message := newMessage(t, core.KDFNone, "", 2, 3600, "ack")
	harness.mustCreate(message)
	response, _ := harness.claim(message, "", "")
	lease := response.Header.Get("X-Nylzo-Lease")
	if response.StatusCode != http.StatusOK || lease == "" {
		t.Fatalf("claim: %d", response.StatusCode)
	}
	harness.clock.Advance(store.LeaseDuration + time.Minute)
	if code := harness.ack(message, lease).StatusCode; code != http.StatusGone {
		t.Fatalf("an ack after the lease ended: %d", code)
	}
	if got, _ := harness.blob(message, lease); got.StatusCode != http.StatusGone {
		t.Fatalf("a download after the lease ended: %d", got.StatusCode)
	}
}

type lifecycleKind struct {
	name   string
	opens  uint16
	ttl    uint32
	ending store.State
	after  time.Duration
	opened bool
	expect [11]string
}

var lifecycleActions = [11]string{
	"burn",
	"cancel",
	"expiry by request",
	"expiry by sweep",
	"release",
	"resume by request",
	"resume by sweep",
	"claim",
	"lease download",
	"blob vanished",
	"ack",
}

func recordedRow(state store.State) [11]string {
	s := string(state)
	return [11]string{
		"410 " + s + " tomb:" + s,
		"410 " + s + " tomb:" + s,
		"200 " + s + " tomb:" + s,
		"- - tomb:" + s,
		"- - live:" + s,
		"200 " + s + " tomb:" + s,
		"- - tomb:" + s,
		"410 " + s + " tomb:" + s,
		"410 " + s + " tomb:" + s,
		"410 " + s + " tomb:" + s,
		"410 " + s + " tomb:" + s,
	}
}

func expiredRow(state store.State) [11]string {
	row := recordedRow(state)
	row[4] = "- - live:"
	return row
}

func openedRow() [11]string {
	row := recordedRow(store.StateOpened)
	row[4] = "- - tomb:opened"
	return row
}

var lifecycleKinds = []lifecycleKind{
	{name: "unspent", opens: 2, ttl: 3600, expect: [11]string{
		"204 - tomb:deleted", "204 - tomb:cancelled", "200 expired tomb:expired", "- - tomb:expired", "- - live:",
		"200 available live:", "- - live:", "200 - live:", "200 - live:", "500 - live:", "204 - live:",
	}},
	{name: "spent during the lease", opens: 1, ttl: 3600, expect: [11]string{
		"204 - tomb:deleted", "204 - tomb:cancelled", "200 opened tomb:opened", "- - tomb:opened", "- - live:",
		"200 opened live:", "- - live:", "410 opened live:", "200 - live:", "500 - live:", "204 - tomb:opened",
	}},
	{
		name:   "spent after the lease",
		opens:  1,
		ttl:    3600,
		after:  store.LeaseDuration + store.LeaseTimerSlack,
		expect: openedRow(),
	},
	{name: "unspent and expired", opens: 2, ttl: 60, after: 61 * time.Second, expect: expiredRow(store.StateExpired)},
	{name: "spent and expired", opens: 1, ttl: 60, after: 61 * time.Second, expect: expiredRow(store.StateOpened)},
	{
		name:   "recorded deleted",
		opens:  2,
		ttl:    3600,
		ending: store.StateDeleted,
		expect: recordedRow(store.StateDeleted),
	},
	{
		name:   "recorded cancelled",
		opens:  2,
		ttl:    3600,
		ending: store.StateCancelled,
		expect: recordedRow(store.StateCancelled),
	},
	{
		name:   "recorded opened",
		opens:  1,
		ttl:    3600,
		after:  store.LeaseDuration + store.LeaseTimerSlack,
		opened: true,
		expect: openedRow(),
	},
	{
		name:   "recorded expired",
		opens:  2,
		ttl:    3600,
		ending: store.StateExpired,
		expect: recordedRow(store.StateExpired),
	},
}

func TestLifecycleMatrix(t *testing.T) {
	t.Parallel()
	for _, kind := range lifecycleKinds {
		for i, action := range lifecycleActions {
			t.Run(kind.name+"/"+action, func(t *testing.T) {
				t.Parallel()
				harness := newHarness(t, baseSettings(pepperA, nil))
				message, lease := setupLifecycleKind(t, harness, kind)
				response, body := runLifecycleAction(t, harness, message, lease, action)
				checkLifecycleCell(t, harness, message, action, kind.expect[i], response, body)
			})
		}
	}
}

func setupLifecycleKind(t *testing.T, harness *testHarness, kind lifecycleKind) (*testMessage, string) {
	t.Helper()
	message := newMessage(t, core.KDFNone, "", kind.opens, kind.ttl, "x")
	harness.mustCreate(message)
	response, _ := harness.claim(message, "", "")
	lease := response.Header.Get("X-Nylzo-Lease")
	if response.StatusCode != http.StatusOK || lease == "" {
		t.Fatalf("claim: %d", response.StatusCode)
	}
	harness.clock.Advance(kind.after)
	ctx := t.Context()
	if kind.ending != "" {
		if _, _, _, err := harness.store.MarkEnding(
			ctx,
			message.id.String(),
			kind.ending,
			harness.clock.Now(),
		); err != nil {
			t.Fatal(err)
		}
	}
	if kind.opened {
		if ref, err := harness.store.ReleaseIfDone(
			ctx,
			message.id.String(),
			harness.clock.Now(),
		); err != nil ||
			ref == "" {
			t.Fatalf("record opened: %q %v", ref, err)
		}
	}
	return message, lease
}

func runLifecycleAction(
	t *testing.T,
	harness *testHarness,
	message *testMessage,
	lease, action string,
) (testResponse, []byte) {
	t.Helper()
	ctx := t.Context()
	switch action {
	case "burn":
		return harness.post(message, "burn", message.readToken)
	case "cancel":
		return harness.post(message, "cancel", message.manageToken)
	case "expiry by request":
		harness.clock.Advance(2 * time.Hour)
		return harness.do(
			&testRequest{
				method:  http.MethodGet,
				path:    "/api/v1/messages/" + message.id.String(),
				address: message.address,
				token:   message.readToken,
			},
		)
	case "expiry by sweep":
		harness.clock.Advance(2 * time.Hour)
		if _, err := harness.server.SweepOnce(ctx); err != nil {
			t.Fatal(err)
		}
	case "release":
		if _, err := store.Release(
			ctx,
			harness.store,
			harness.blobs,
			message.id.String(),
			harness.clock.Now(),
		); err != nil {
			t.Fatal(err)
		}
	case "resume by request":
		return harness.do(
			&testRequest{
				method:  http.MethodGet,
				path:    "/api/v1/messages/" + message.id.String() + "/status",
				address: message.address,
				token:   message.manageToken,
			},
		)
	case "resume by sweep":
		if _, err := harness.server.SweepOnce(ctx); err != nil {
			t.Fatal(err)
		}
	case "claim":
		return harness.claim(message, "", "")
	case "lease download":
		return harness.blob(message, lease)
	case "blob vanished":
		if err := harness.blobs.Delete(ctx, message.id.String()); err != nil {
			t.Fatal(err)
		}
		return harness.claim(message, "", "")
	case "ack":
		return harness.do(
			&testRequest{
				method:  http.MethodPost,
				path:    "/api/v1/messages/" + message.id.String() + "/ack",
				address: message.address,
				token:   message.readToken,
				headers: map[string]string{"X-Nylzo-Lease": lease},
			},
		)
	}
	return testResponse{}, nil
}

func checkLifecycleCell(
	t *testing.T,
	harness *testHarness,
	message *testMessage,
	action, expect string,
	response testResponse,
	body []byte,
) {
	t.Helper()
	parts := strings.Fields(expect)
	code, state, final := parts[0], parts[1], parts[2]
	if code != "-" && response.StatusCode == 0 {
		t.Fatalf("no response, want %s", code)
	}
	if code != "-" && strconv.Itoa(response.StatusCode) != code {
		t.Fatalf("response %d, want %s (%s)", response.StatusCode, code, body)
	}
	if state != "-" && decodeJSON(t, body)["state"] != state {
		t.Fatalf("state %s, want %s", body, state)
	}
	if code == "410" || code == "500" {
		if lease := response.Header.Get("X-Nylzo-Lease"); lease != "" && action == "claim" {
			t.Fatalf("a refused claim handed out a lease")
		}
	}
	record, err := harness.store.Get(t.Context(), message.id.String())
	if err != nil {
		t.Fatal(err)
	}
	kind, want, _ := strings.Cut(final, ":")
	switch kind {
	case "tomb":
		if record.Tombstone == nil || string(record.Tombstone.State) != want {
			t.Fatalf("row %+v %+v, want tombstone %s", record.Message, record.Tombstone, want)
		}
		if harness.blobExists(message.id) {
			t.Fatal("an ended message kept its blob")
		}
	case "live":
		if record.Message == nil || string(record.Message.Ending) != want {
			t.Fatalf("row %+v %+v, want a live row with ending %q", record.Message, record.Tombstone, want)
		}
		if exists := harness.blobExists(message.id); exists != (action != "blob vanished") {
			t.Fatalf("blob present %v on a live row", exists)
		}
	}
}

func TestVanishedBlobDuringLeaseIsAStorageFault(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	message := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	harness.mustCreate(message)
	response, _ := harness.claim(message, "", "")
	lease := response.Header.Get("X-Nylzo-Lease")
	if err := harness.blobs.Delete(t.Context(), message.id.String()); err != nil {
		t.Fatal(err)
	}
	if got, _ := harness.blob(message, lease); got.StatusCode != http.StatusInternalServerError {
		t.Fatalf("download of a vanished blob: %d", got.StatusCode)
	}
	if record, err := harness.store.Get(
		t.Context(),
		message.id.String(),
	); err != nil || record.Message == nil ||
		record.Message.Ending != "" {
		t.Fatalf("the row must stay live without an ending: %+v %v", record, err)
	}
}

func TestExpiryRacingADownloadAnswersGone(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	faulty := harness.withFaultyBlobs()
	message := newMessage(t, core.KDFNone, "", 2, 60, "x")
	harness.mustCreate(message)
	response, _ := harness.claim(message, "", "")
	lease := response.Header.Get("X-Nylzo-Lease")
	var once sync.Once
	faulty.set(func(b *faultyBlobs) {
		b.onOpen = func(ref string) {
			once.Do(func() {
				harness.clock.Advance(61 * time.Second)
				if err := b.Blobs.Delete(t.Context(), ref); err != nil {
					t.Error(err)
				}
			})
		}
	})
	response, body := harness.blob(message, lease)
	if response.StatusCode != http.StatusGone || decodeJSON(t, body)["state"] != "expired" {
		t.Fatalf("a download racing the expiry ending: %d %s", response.StatusCode, body)
	}
	if record, err := harness.store.Get(
		t.Context(),
		message.id.String(),
	); err != nil || record.Tombstone == nil ||
		record.Tombstone.State != store.StateExpired {
		t.Fatalf("the download must finish the expiry: %+v %v", record, err)
	}
}

func TestClaimReadsItsBodyBeforeOpeningTheBlob(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	faulty := harness.withFaultyBlobs()
	message := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	harness.mustCreate(message)
	var opens atomic.Int32
	faulty.set(func(b *faultyBlobs) { b.onOpen = func(string) { opens.Add(1) } })
	response, _ := harness.do(
		&testRequest{
			method:  http.MethodPost,
			path:    "/api/v1/messages/" + message.id.String() + "/claim",
			address: message.address,
			body:    []byte("{"),
			token:   message.readToken,
			headers: map[string]string{"Content-Type": "application/json"},
		},
	)
	if response.StatusCode != http.StatusBadRequest || opens.Load() != 0 {
		t.Fatalf(
			"a malformed claim body must be refused before the blob is opened: %d, %d opens",
			response.StatusCode,
			opens.Load(),
		)
	}
}

func TestTruncatedBlobSpendsNoOpen(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	message := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	harness.mustCreate(message)
	short := message.object[:len(message.object)-1]
	if err := harness.blobs.Delete(t.Context(), message.id.String()); err != nil {
		t.Fatal(err)
	}
	if err := harness.blobs.Put(
		t.Context(),
		message.id.String(),
		bytes.NewReader(short),
		int64(len(short)),
	); err != nil {
		t.Fatal(err)
	}
	response, _ := harness.claim(message, "", "")
	if response.StatusCode != http.StatusInternalServerError || response.Header.Get("X-Nylzo-Lease") != "" {
		t.Fatalf("a blob of the wrong size must answer 500 before an open is spent: %d", response.StatusCode)
	}
	if _, metadata := harness.metadata(message); metadata["opens_remaining"] != float64(1) {
		t.Fatalf("a truncated blob spent an open: %v", metadata)
	}
}

func TestHeadOnBlobSpendsNoDownload(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	message := newMessage(t, core.KDFNone, "", 2, 3600, "x")
	harness.mustCreate(message)
	response, _ := harness.claim(message, "", "")
	lease := response.Header.Get("X-Nylzo-Lease")
	head, _ := harness.do(
		&testRequest{
			method:  http.MethodHead,
			path:    "/api/v1/messages/" + message.id.String() + "/blob",
			address: message.address,
			token:   message.readToken,
			headers: map[string]string{"X-Nylzo-Lease": lease},
		},
	)
	if head.StatusCode != http.StatusNotFound {
		t.Fatalf("HEAD on /blob: %d", head.StatusCode)
	}
	for i := range store.MaxLeaseDownload {
		if got, _ := harness.blob(message, lease); got.StatusCode != http.StatusOK {
			t.Fatalf("download %d after a HEAD: %d", i+1, got.StatusCode)
		}
	}
}

type leaseCount struct {
	store.Store

	n *atomic.Int32
}

func (s leaseCount) Leases(ctx context.Context, id string) ([]store.Lease, error) {
	s.n.Add(1)
	return s.Store.Leases(ctx, id)
}

func TestDownloadReadsLeasesOnce(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	n := &atomic.Int32{}
	harness.store = leaseCount{harness.store, n}
	harness.reopen(&harness.settings)
	message := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	harness.mustCreate(message)
	response, _ := harness.claim(message, "", "")
	n.Store(0)
	if got, _ := harness.blob(
		message,
		response.Header.Get("X-Nylzo-Lease"),
	); got.StatusCode != http.StatusOK {
		t.Fatalf("download: %d", got.StatusCode)
	}
	if got := n.Load(); got != 1 {
		t.Fatalf("a download read the leases %d times", got)
	}
}

type burnAfterClaim struct {
	store.Store

	blobs store.Blobs
}

func (s burnAfterClaim) Claim(ctx context.Context, p store.ClaimParams) (store.ClaimResult, error) {
	res, err := s.Store.Claim(ctx, p)
	if err == nil {
		if _, endMessageErr := store.EndMessage(
			ctx,
			s.Store,
			s.blobs,
			p.ID,
			store.StateDeleted,
			p.Clock(),
		); endMessageErr != nil {
			return res, endMessageErr
		}
	}
	return res, err
}

func TestClaimStreamsTheBlobItOpenedWhenABurnFollows(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	message := newMessage(t, core.KDFNone, "", 2, 3600, "x")
	harness.mustCreate(message)
	harness.store = burnAfterClaim{Store: harness.store, blobs: harness.blobs}
	harness.reopen(&harness.settings)
	response, object := harness.claim(message, "", "")
	if response.StatusCode != http.StatusOK || response.Header.Get("X-Nylzo-Lease") == "" ||
		!bytes.Equal(object, message.object) {
		t.Fatalf(
			"a claim that spent an open must stream the blob it opened: %d %d bytes",
			response.StatusCode,
			len(object),
		)
	}
}

func TestExpiryRacingADownloadWithAStuckEndAnswersExpired(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	faulty := harness.withFaultyBlobs()
	message := newMessage(t, core.KDFNone, "", 2, 60, "x")
	harness.mustCreate(message)
	response, _ := harness.claim(message, "", "")
	lease := response.Header.Get("X-Nylzo-Lease")
	harness.store = failingEndStore{Store: harness.store}
	harness.reopen(&harness.settings)
	var once sync.Once
	faulty.set(func(b *faultyBlobs) {
		b.onOpen = func(ref string) {
			once.Do(func() {
				harness.clock.Advance(61 * time.Second)
				if err := b.Blobs.Delete(t.Context(), ref); err != nil {
					t.Error(err)
				}
			})
		}
	})
	response, body := harness.blob(message, lease)
	if response.StatusCode != http.StatusGone || decodeJSON(t, body)["state"] != "expired" {
		t.Fatalf("an expiry that cannot finish must still report expired: %d %s", response.StatusCode, body)
	}
}

func TestReleaseAfterLastLeaseKeepsUnspentMessage(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	message := newMessage(t, core.KDFNone, "", 2, 3600, "x")
	harness.mustCreate(message)
	response, _ := harness.claim(message, "", "")
	if ack := harness.ack(message, response.Header.Get("X-Nylzo-Lease")); ack.StatusCode != http.StatusNoContent {
		t.Fatalf("ack: %d", ack.StatusCode)
	}
	harness.clock.Advance(store.LeaseDuration + store.LeaseTimerSlack)
	if released, err := store.Release(
		t.Context(),
		harness.store,
		harness.blobs,
		message.id.String(),
		harness.clock.Now(),
	); err != nil ||
		released {
		t.Fatalf("release: %v %v", released, err)
	}
	record, err := harness.store.Get(t.Context(), message.id.String())
	if err != nil || record.Message == nil || record.Message.Ending != "" || record.Message.OpensRemaining != 1 ||
		!harness.blobExists(message.id) {
		t.Fatalf("row %+v %v", record.Message, err)
	}
}

func TestReleaseAfterLastLeaseKeepsRecordedExpiry(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	message := newMessage(t, core.KDFNone, "", 2, 3600, "x")
	harness.mustCreate(message)
	if response, _ := harness.claim(message, "", ""); response.StatusCode != http.StatusOK {
		t.Fatalf("claim: %d", response.StatusCode)
	}
	if _, _, _, err := harness.store.MarkEnding(
		t.Context(),
		message.id.String(),
		store.StateExpired,
		harness.clock.Now(),
	); err != nil {
		t.Fatal(err)
	}
	harness.clock.Advance(store.LeaseDuration + store.LeaseTimerSlack)
	if released, err := store.Release(
		t.Context(),
		harness.store,
		harness.blobs,
		message.id.String(),
		harness.clock.Now(),
	); err != nil ||
		released {
		t.Fatalf("release: %v %v", released, err)
	}
	record, err := harness.store.Get(t.Context(), message.id.String())
	if err != nil || record.Message == nil || record.Message.Ending != store.StateExpired ||
		record.Message.OpensRemaining != 1 ||
		!harness.blobExists(message.id) {
		t.Fatalf("row %+v %v", record.Message, err)
	}
}

func TestDownloadAfterAFinishedConcurrentBurnReportsDeleted(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	message := newMessage(t, core.KDFNone, "", 2, 3600, "x")
	harness.mustCreate(message)
	response, _ := harness.claim(message, "", "")
	lease := response.Header.Get("X-Nylzo-Lease")
	inner := harness.store
	harness.store = &racingStore{Store: inner, race: func(id string) {
		if _, err := store.EndMessage(
			t.Context(),
			inner,
			harness.blobs,
			id,
			store.StateDeleted,
			harness.clock.Now(),
		); err != nil {
			t.Error(err)
		}
	}}
	harness.reopen(&harness.settings)
	for _, action := range []string{"lease download", "ack"} {
		got, body := runLifecycleAction(t, harness, message, lease, action)
		if got.StatusCode != http.StatusGone || decodeJSON(t, body)["state"] != "deleted" {
			t.Fatalf("%s after a finished concurrent burn: %d %s", action, got.StatusCode, body)
		}
	}
}

func TestAckAfterARecordedBurnFinishesIt(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	message := newMessage(t, core.KDFNone, "", 2, 3600, "x")
	harness.mustCreate(message)
	response, _ := harness.claim(message, "", "")
	lease := response.Header.Get("X-Nylzo-Lease")
	inner := harness.store
	harness.store = &racingStore{Store: inner, race: func(id string) {
		if _, _, _, err := inner.MarkEnding(
			t.Context(),
			id,
			store.StateDeleted,
			harness.clock.Now(),
		); err != nil {
			t.Error(err)
		}
	}}
	harness.reopen(&harness.settings)
	response, body := runLifecycleAction(t, harness, message, lease, "ack")
	if response.StatusCode != http.StatusGone || decodeJSON(t, body)["state"] != "deleted" {
		t.Fatalf("an ack after a recorded burn: %d %s", response.StatusCode, body)
	}
	if record, err := inner.Get(
		t.Context(),
		message.id.String(),
	); err != nil || record.Tombstone == nil || record.Tombstone.State != store.StateDeleted ||
		harness.blobExists(message.id) {
		t.Fatalf("the ack must finish the recorded burn: %+v %v", record, err)
	}
}

type slowWriterStore struct {
	store.Store

	clock *fakeClock
	wait  time.Duration
}

func (s slowWriterStore) Claim(ctx context.Context, p store.ClaimParams) (store.ClaimResult, error) {
	s.clock.Advance(s.wait)
	return s.Store.Claim(ctx, p)
}

func TestClaimThatWaitsPastExpiryIsRefused(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	message := newMessage(t, core.KDFNone, "", 1, 60, "x")
	harness.mustCreate(message)
	harness.clock.Advance(59 * time.Second)
	harness.store = slowWriterStore{Store: harness.store, clock: harness.clock, wait: 2 * time.Second}
	harness.reopen(&harness.settings)
	response, body := harness.claim(message, "", "")
	if response.StatusCode != http.StatusGone || decodeJSON(t, body)["state"] != "expired" ||
		response.Header.Get("X-Nylzo-Lease") != "" {
		t.Fatalf(
			"a claim that reached the store after expires_at must be refused: %d %s",
			response.StatusCode,
			response.Header.Get("Content-Type"),
		)
	}
}

func TestBlobLossWithoutEndingIsAStorageFault(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	for _, opens := range []uint16{1, 2} {
		message := newMessage(t, core.KDFNone, "", opens, 3600, "x")
		harness.mustCreate(message)
		if err := harness.blobs.Delete(t.Context(), message.id.String()); err != nil {
			t.Fatal(err)
		}
		response, _ := harness.claim(message, "", "")
		if response.StatusCode != http.StatusInternalServerError || response.Header.Get("X-Nylzo-Lease") != "" {
			t.Fatalf("opens %d: a lost blob must answer 500 before any open is spent: %d", opens, response.StatusCode)
		}
		harness.clock.Advance(store.LeaseDuration + time.Minute)
		if _, err := harness.server.SweepOnce(t.Context()); err != nil {
			t.Fatal(err)
		}
		if got, metadata := harness.metadata(
			message,
		); got.StatusCode != http.StatusOK || metadata["state"] != "available" ||
			metadata["opens_remaining"] != float64(opens) {
			t.Fatalf("opens %d: a lost blob must not spend an open or end the message: %v", opens, metadata)
		}
	}
}

func TestInterruptedEndingIsFinishedOnAccess(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	crash := func(message *testMessage, state store.State) {
		if _, _, _, err := harness.store.MarkEnding(
			t.Context(),
			message.id.String(),
			state,
			harness.clock.Now(),
		); err != nil {
			t.Error(err)
		}
		if err := harness.blobs.Delete(t.Context(), message.id.String()); err != nil {
			t.Error(err)
		}
	}
	claimed := newMessage(t, core.KDFNone, "", 2, 3600, "x")
	harness.mustCreate(claimed)
	crash(claimed, store.StateCancelled)
	response, body := harness.claim(claimed, "", "")
	if response.StatusCode != http.StatusGone || response.Header.Get("X-Nylzo-Lease") != "" ||
		decodeJSON(t, body)["state"] != "cancelled" {
		t.Fatalf("claim after an interrupted cancel: %d %s", response.StatusCode, body)
	}
	if _, status := harness.status(claimed); status["state"] != "cancelled" {
		t.Fatalf("status: %v", status)
	}
	downloaded := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	harness.mustCreate(downloaded)
	response, _ = harness.claim(downloaded, "", "")
	lease := response.Header.Get("X-Nylzo-Lease")
	if response.StatusCode != http.StatusOK || lease == "" {
		t.Fatalf("claim: %d", response.StatusCode)
	}
	inner := harness.store
	harness.store = &racingStore{Store: inner, race: func(id string) {
		if id == downloaded.id.String() {
			crash(downloaded, store.StateDeleted)
		}
	}}
	harness.reopen(&harness.settings)
	response, body = harness.blob(downloaded, lease)
	if response.StatusCode != http.StatusGone || decodeJSON(t, body)["state"] != "deleted" {
		t.Fatalf("download racing a burn: %d %s", response.StatusCode, body)
	}
	if record, err := inner.Get(
		t.Context(),
		downloaded.id.String(),
	); err != nil || record.Tombstone == nil ||
		record.Tombstone.State != store.StateDeleted {
		t.Fatalf("the download must finish the interrupted burn: %+v %v", record, err)
	}
}

func TestClaimKeepsLeaseWhenTheStreamFails(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	faulty := harness.withFaultyBlobs()
	message := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	harness.mustCreate(message)
	faulty.set(func(b *faultyBlobs) { b.failRead = errInjected })
	request, err := http.NewRequest(
		"POST",
		harness.httpServer.URL+"/api/v1/messages/"+message.id.String()+"/claim",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	harness.sign(request, message.readToken, "", nil)
	request.Header.Set("X-Forwarded-For", message.address)
	claimed, err := harness.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_, readErr := io.ReadAll(claimed.Body)
	claimed.Body.Close()
	lease := claimed.Header.Get("X-Nylzo-Lease")
	if claimed.StatusCode != http.StatusOK || lease == "" || readErr == nil {
		t.Fatalf(
			"a stream that fails after the claim must still carry the lease: %d %q %v",
			claimed.StatusCode,
			lease,
			readErr,
		)
	}
	if !strings.Contains(harness.logs.String(), `"msg":"blob-stream"`) {
		t.Fatalf("a failed stream must be logged: %s", harness.logs.String())
	}
	for line := range strings.SplitSeq(harness.logs.String(), "\n") {
		var entry map[string]any
		if json.Unmarshal([]byte(line), &entry) != nil || entry["msg"] != "blob-stream" {
			continue
		}
		delete(entry, "time")
		delete(entry, "level")
		delete(entry, "msg")
		if len(entry) != 0 {
			t.Fatalf("the stream warning carries more than its name: %s", line)
		}
	}
	faulty.set(func(b *faultyBlobs) { b.failRead = nil })
	response, object := harness.blob(message, lease)
	if response.StatusCode != http.StatusOK || !bytes.Equal(object, message.object) {
		t.Fatalf("retry through the lease: %d", response.StatusCode)
	}
}

func TestStorageCancellationIsInternalWhileClientWaits(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	faulty := harness.withFaultyBlobs()
	faulty.set(func(b *faultyBlobs) { b.failPut = context.Canceled })
	if response, _ := harness.create(
		newMessage(t, core.KDFNone, "", 1, 3600, "x"),
	); response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("storage cancellation with a live client: %d", response.StatusCode)
	}
}

func TestClaimFindsBlobRemovedByCommittedEnding(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	faulty := harness.withFaultyBlobs()
	message := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	harness.mustCreate(message)
	inner := harness.store
	faulty.set(func(b *faultyBlobs) {
		b.onOpen = func(ref string) {
			if _, err := store.EndMessage(
				t.Context(),
				inner,
				b.Blobs,
				ref,
				store.StateDeleted,
				harness.clock.Now(),
			); err != nil {
				t.Error(err)
			}
		}
	})
	response, body := harness.claim(message, "", "")
	if response.StatusCode != http.StatusGone || response.Header.Get("X-Nylzo-Lease") != "" ||
		decodeJSON(t, body)["state"] != "deleted" {
		t.Fatalf("claim racing a committed burn: %d %s", response.StatusCode, body)
	}
}

type ackRacingStore struct {
	store.Store

	race func(id string)
}

func (s *ackRacingStore) Ack(ctx context.Context, l store.Lease, now time.Time) (string, error) {
	s.race(l.ID)
	return s.Store.Ack(ctx, l, now)
}

func TestAckAfterAConcurrentBurnAnswersGone(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	message := newMessage(t, core.KDFNone, "", 2, 3600, "x")
	harness.mustCreate(message)
	response, _ := harness.claim(message, "", "")
	lease := response.Header.Get("X-Nylzo-Lease")
	if response.StatusCode != http.StatusOK || lease == "" {
		t.Fatalf("claim: %d", response.StatusCode)
	}
	inner, blobs := harness.store, harness.blobs
	harness.store = &ackRacingStore{Store: inner, race: func(id string) {
		if _, err := store.EndMessage(
			t.Context(),
			inner,
			blobs,
			id,
			store.StateDeleted,
			harness.clock.Now(),
		); err != nil {
			t.Error(err)
		}
	}}
	harness.reopen(&harness.settings)
	response, body := harness.do(
		&testRequest{
			method:  http.MethodPost,
			path:    "/api/v1/messages/" + message.id.String() + "/ack",
			address: message.address,
			token:   message.readToken,
			headers: map[string]string{"X-Nylzo-Lease": lease},
		},
	)
	if response.StatusCode != http.StatusGone || decodeJSON(t, body)["state"] != "deleted" {
		t.Fatalf("ack after a burn removed its lease: %d %s", response.StatusCode, body)
	}
}

type countingReleaseStore struct {
	store.Store

	releases atomic.Int32
}

func (s *countingReleaseStore) ReleaseIfDone(ctx context.Context, id string, now time.Time) (string, error) {
	s.releases.Add(1)
	return s.Store.ReleaseIfDone(ctx, id, now)
}

func TestRequestDuringTheLeaseTakesNoWriteLock(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	message := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	harness.mustCreate(message)
	if response, _ := harness.claim(message, "", ""); response.StatusCode != http.StatusOK {
		t.Fatalf("claim: %d", response.StatusCode)
	}
	counting := &countingReleaseStore{Store: harness.store}
	harness.store = counting
	harness.reopen(&harness.settings)
	wrong := *message
	wrong.readToken = core.Encode(core.NewSecret()[:])
	harness.metadata(message)
	harness.metadata(&wrong)
	if n := counting.releases.Load(); n != 0 {
		t.Fatalf("requests during the lease tried to release the blob %d times", n)
	}
}

func TestExpiredMessageWithStuckDeleteReportsItsEnd(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	faulty := harness.withFaultyBlobs()
	unspent := newMessage(t, core.KDFNone, "", 2, 60, "x")
	spent := newMessage(t, core.KDFNone, "", 1, 3600, "y")
	harness.mustCreate(unspent)
	harness.mustCreate(spent)
	if response, _ := harness.claim(spent, "", ""); response.StatusCode != http.StatusOK {
		t.Fatalf("claim: %d", response.StatusCode)
	}
	faulty.set(func(b *faultyBlobs) {
		b.failDelete[unspent.id.String()] = true
		b.failDelete[spent.id.String()] = true
	})
	harness.clock.Advance(time.Hour)
	if _, metadata := harness.metadata(unspent); metadata["state"] != "expired" {
		t.Fatalf("unspent: %v", metadata)
	}
	if _, metadata := harness.metadata(spent); metadata["state"] != "opened" {
		t.Fatalf("spent: %v", metadata)
	}
	if response, body := harness.claim(
		unspent,
		"",
		"",
	); response.StatusCode != http.StatusGone ||
		decodeJSON(t, body)["state"] != "expired" {
		t.Fatalf("claim: %d %s", response.StatusCode, body)
	}
}

func TestClaimOpenErrorSpendsNoOpen(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	faulty := harness.withFaultyBlobs()
	message := newMessage(t, core.KDFNone, "", 1, 3600, "x")
	harness.mustCreate(message)
	faulty.set(func(b *faultyBlobs) { b.failOpen = errInjected })
	response, _ := harness.claim(message, "", "")
	if response.StatusCode != http.StatusInternalServerError || response.Header.Get("X-Nylzo-Lease") != "" {
		t.Fatalf(
			"a blob open error before the claim: %d lease=%q",
			response.StatusCode,
			response.Header.Get("X-Nylzo-Lease"),
		)
	}
	faulty.set(func(b *faultyBlobs) { b.failOpen = nil })
	if _, metadata := harness.metadata(message); metadata["opens_remaining"] != float64(1) {
		t.Fatalf("an open error spent an open: %v", metadata)
	}
}

func TestMalformedIDsDoNotSpendReadLimit(t *testing.T) {
	t.Parallel()
	settings := baseSettings(pepperA, nil)
	settings.Rates.ReadPerHour = 1
	harness := newHarness(t, settings)
	message := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	message.address = "203.0.113.5"
	harness.mustCreate(message)
	for _, path := range []string{"/api/v1/messages/short", "/api/v1/messages/short/claim", "/api/v1/messages/short/blob"} {
		method := "GET"
		if strings.HasSuffix(path, "/claim") {
			method = "POST"
		}
		if response, _ := harness.do(
			&testRequest{method: method, path: path, address: message.address, token: message.readToken},
		); response.StatusCode != http.StatusNotFound {
			t.Fatalf("%s %s: %d", method, path, response.StatusCode)
		}
	}
	if response, _ := harness.metadata(message); response.StatusCode != http.StatusOK {
		t.Fatalf("malformed ids spent the read limit: %d", response.StatusCode)
	}
}

func TestRefusedReaderDoesNotSpendItsRange(t *testing.T) {
	t.Parallel()
	settings := baseSettings(pepperA, nil)
	settings.Rates.ReadPerHour = 1
	settings.Rates.CoarseMultiplier = 2
	harness := newHarness(t, settings)
	message := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	harness.mustCreate(message)
	read := func(ip string) int {
		reader := *message
		reader.address = ip
		response, _ := harness.metadata(&reader)
		return response.StatusCode
	}
	for i := range 4 {
		if want := map[bool]int{true: 200, false: 429}[i == 0]; read("198.51.100.1") != want {
			t.Fatalf("read %d from one source, want %d", i+1, want)
		}
	}
	if code := read("198.51.100.2"); code != http.StatusOK {
		t.Fatalf("a refused reader must not use up its /24: %d", code)
	}
	if code := read("198.51.100.3"); code != http.StatusTooManyRequests {
		t.Fatalf("the /24 read budget still applies: %d", code)
	}
}

func TestReadRateLimitCountsMetadataClaimAndBlob(t *testing.T) {
	t.Parallel()
	settings := baseSettings(pepperA, nil)
	settings.Rates.ReadPerHour = 3
	harness := newHarness(t, settings)
	message := newMessage(t, core.KDFNone, "", 1, 3600, "n")
	message.address = "203.0.113.5"
	harness.mustCreate(message)
	for i := range 2 {
		if response, _ := harness.metadata(message); response.StatusCode != http.StatusOK {
			t.Fatalf("metadata %d: %d", i, response.StatusCode)
		}
	}
	claimed, _ := harness.claim(message, "", "")
	lease := claimed.Header.Get("X-Nylzo-Lease")
	if claimed.StatusCode != http.StatusOK || lease == "" {
		t.Fatalf("the third read, a claim: %d", claimed.StatusCode)
	}
	if response, _ := harness.blob(message, lease); response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("a download over the read limit: %d", response.StatusCode)
	}
	if response, _ := harness.metadata(message); response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("metadata over the read limit: %d", response.StatusCode)
	}
	message.address = "203.0.113.6"
	if response, _ := harness.blob(message, lease); response.StatusCode != http.StatusOK {
		t.Fatalf("another address keeps its own read budget: %d", response.StatusCode)
	}
}

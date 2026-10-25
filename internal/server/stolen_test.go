package server

import (
	"bytes"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/core"
)

type stolenNeedles struct {
	t     *testing.T
	named map[string][]byte
}

func (n *stolenNeedles) secret(name string, value []byte) {
	n.named[name] = value
	n.named[name+" base64url"] = []byte(core.Encode(value))
	n.named[name+" hex"] = []byte(hex.EncodeToString(value))
}

func (n *stolenNeedles) text(name, value string) {
	n.named[name] = []byte(value)
}

func (n *stolenNeedles) addMessage(label string, message *testMessage, passphrase string) {
	n.t.Helper()
	n.secret(label+" S", message.secret[:])
	n.secret(label+" R", message.link.R[:])
	n.secret(label+" K", message.content.K[:])
	n.secret(label+" V", message.content.V[:])
	n.secret(label+" M", message.manage[:])
	readKey, verifierKey := core.PublicKey(&message.link.R), core.PublicKey(&message.content.V)
	manageKey := core.PublicKey(message.manage)
	n.secret(label+" pk_R", readKey[:])
	n.secret(label+" pk_V", verifierKey[:])
	n.secret(label+" pk_M", manageKey[:])
	if passphrase != "" {
		n.secret(label+" P", testPassphraseKey(message.kdf, passphrase, &message.link.SaltPW))
		n.text(label+" passphrase", passphrase)
	}
	if message.text != "" {
		n.text(label+" plaintext", message.text)
	}
}

func fileMessage(t *testing.T, content []byte) *testMessage {
	t.Helper()
	message := newMessage(t, core.KDFNone, "", 1, 3600, "unused")
	message.text = ""
	object, err := core.Seal(&message.content.K, core.NewHeader(core.KDFNone), message.id, message.policy, core.Payload{
		Meta: core.Meta{
			File: &core.FileMeta{
				Name: "quarterly-plan-sentinel.pdf",
				Mime: "application/pdf",
				Size: int64(len(content)),
			},
		},
		File: content,
	})
	if err != nil {
		t.Fatal(err)
	}
	message.object = object
	return message
}

func TestStolenServerRevealsNothing(t *testing.T) {
	t.Parallel()
	settings := baseSettings(pepperA, nil)
	settings.Rates.GlobalDailyCreates = 16
	settings.PoWDifficulty = 1
	harness := newHarness(t, settings)
	faulty := harness.withFaultyBlobs()
	needles := &stolenNeedles{t: t, named: map[string][]byte{}}
	const ip4, ip6 = "203.0.113.77", "2001:db8:77::1"
	needles.text("IPv4 address", ip4)
	needles.text("IPv6 address", ip6)
	needles.text("IPv6 address, expanded", netip.MustParseAddr(ip6).StringExpanded())
	raw6 := netip.MustParseAddr(ip6).As16()
	needles.named["IPv6 address, raw"] = raw6[:]
	var lease string
	claimed := func(message *testMessage, verifier, ip string) {
		t.Helper()
		response, _ := harness.claim(message, verifier, ip)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("claim: %d", response.StatusCode)
		}
		lease = response.Header.Get("X-Nylzo-Lease")
		raw, err := core.DecodeExact(lease, core.TokenLen)
		if err != nil {
			t.Fatal(err)
		}
		needles.secret("lease of "+message.id.String(), raw)
	}

	expired := newMessage(t, core.KDFNone, "", 1, 60, "plaintext expired sentinel")
	expired.address = ip4
	harness.mustCreate(expired)
	needles.addMessage("expired", expired, "")
	harness.clock.Advance(2*time.Minute + 17*time.Second)
	if _, metadata := harness.metadata(expired); metadata["state"] != "expired" {
		t.Fatalf("expired: %v", metadata)
	}

	opened := newMessage(t, core.KDFNone, "", 1, 3600, "plaintext opened sentinel")
	opened.address = ip4
	harness.mustCreate(opened)
	needles.addMessage("opened", opened, "")
	claimed(opened, "", ip4)
	if response := harness.ack(opened, lease); response.StatusCode != http.StatusNoContent {
		t.Fatalf("ack: %d", response.StatusCode)
	}

	const pass = "correct horse battery staple"
	leased := newMessage(t, core.KDFScrypt, pass, 2, 3600, "plaintext leased sentinel")
	leased.address = ip6
	harness.mustCreate(leased)
	needles.addMessage("leased", leased, pass)
	harness.claim(leased, core.Encode(make([]byte, 32)), ip6)
	claimed(leased, leased.verifier, ip6)

	content := bytes.Repeat([]byte("file-content-sentinel "), 3000)
	file := fileMessage(t, content)
	file.address = ip4
	harness.mustCreate(file)
	needles.addMessage("file", file, "")
	needles.text("file name", "quarterly-plan-sentinel.pdf")
	needles.named["file content"] = content[:64]

	burned := newMessage(t, core.KDFNone, "", 1, 3600, "plaintext burned sentinel")
	burned.address = ip4
	harness.mustCreate(burned)
	needles.addMessage("burned", burned, "")
	if response, _ := harness.post(burned, "burn", burned.readToken); response.StatusCode != http.StatusNoContent {
		t.Fatalf("burn: %d", response.StatusCode)
	}

	cancelled := newMessage(t, core.KDFScrypt, pass, 1, 3600, "plaintext cancelled sentinel")
	cancelled.address = ip6
	harness.mustCreate(cancelled)
	needles.addMessage("cancelled", cancelled, pass)
	if response, _ := harness.post(
		cancelled,
		"cancel",
		cancelled.manageToken,
	); response.StatusCode != http.StatusNoContent {
		t.Fatalf("cancel: %d", response.StatusCode)
	}

	stuck := newMessage(t, core.KDFNone, "", 1, 3600, "plaintext stuck sentinel")
	stuck.address = ip4
	harness.mustCreate(stuck)
	needles.addMessage("stuck", stuck, "")
	faulty.set(func(b *faultyBlobs) {
		b.deleteErr[stuck.id.String()] = &fs.PathError{
			Op:   "remove",
			Path: filepath.Join(harness.dir, "blobs", hex.EncodeToString([]byte(stuck.id.String()))),
			Err:  syscall.EROFS,
		}
	})
	if response, _ := harness.post(
		stuck,
		"burn",
		stuck.readToken,
	); response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("burn with a failing blob store: %d", response.StatusCode)
	}
	if !strings.Contains(harness.logs.String(), "remove: "+syscall.EROFS.Error()) {
		t.Fatal("the failing delete was not logged")
	}

	pow := newMessage(t, core.KDFNone, "", 1, 3600, "plaintext pow sentinel")
	pow.address = ip6
	filler := newMessage(t, core.KDFNone, "", 1, 3600, "filler")
	filler.address = ip4
	harness.mustCreate(filler)
	_, instance := harness.do(&testRequest{method: http.MethodGet, path: "/api/v1/instance"})
	powInfo := decodeJSON(t, instance)["pow"].(map[string]any)
	if powInfo["required"] != true {
		t.Fatalf("PoW is not required yet: %v", powInfo)
	}
	token := solveWork(t, powInfo["challenge"].(string), pow.id, 1)
	if response, body := harness.do(
		&testRequest{
			method:  http.MethodPost,
			path:    "/api/v1/messages",
			body:    pow.object,
			address: pow.address,
			headers: map[string]string{
				"Content-Type":   "application/octet-stream",
				"X-Nylzo-Create": pow.createHeader(0),
				"X-Nylzo-Pow":    token,
			},
		},
	); response.StatusCode != http.StatusCreated {
		t.Fatalf("create with PoW: %d %s", response.StatusCode, body)
	}
	needles.addMessage("pow", pow, "")

	link := core.FormatLink("https://nylzo.example", opened.id, opened.kdf, opened.secret)
	body, _ := json.Marshal(map[string]string{"id": leased.id.String(), "reason": "spam, see " + link})
	if response, _ := harness.do(
		&testRequest{
			method:  http.MethodPost,
			path:    "/api/v1/abuse",
			address: ip4,
			body:    body,
			token:   leased.readToken,
			headers: map[string]string{"Content-Type": "application/json"},
		},
	); response.StatusCode != http.StatusAccepted {
		t.Fatalf("abuse report: %d", response.StatusCode)
	}

	stolen := map[string][]byte{"log": []byte(harness.logs.String())}
	if err := filepath.WalkDir(harness.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(harness.dir, path)
		stolen[rel] = data
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"nylzo.db", "nylzo.db-wal", filepath.Join("abuse", harness.clock.Now().UTC().Format(time.DateOnly)+".jsonl")} {
		if _, ok := stolen[want]; !ok {
			t.Fatalf("the stolen copy misses %s: %v", want, keys(stolen))
		}
	}
	for where, data := range stolen {
		for what, needle := range needles.named {
			if bytes.Contains(data, needle) {
				t.Errorf("%s reveals %s", where, what)
			}
		}
	}
	everyone := []*testMessage{expired, opened, leased, file, burned, cancelled, stuck, pow}
	for _, message := range everyone {
		forms := []string{
			message.id.String(),
			hex.EncodeToString([]byte(message.id.String())),
			hex.EncodeToString(message.id[:]),
		}
		if slices.ContainsFunc(forms, func(form string) bool { return bytes.Contains(stolen["log"], []byte(form)) }) {
			t.Errorf("the log names message %s", message.id)
		}
	}

	db, err := sql.Open("sqlite", "file:"+filepath.Join(harness.dir, "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	minute := time.Minute.Milliseconds()
	for _, q := range []string{
		`SELECT created_at FROM messages`, `SELECT expires_at FROM messages`, `SELECT next_attempt_at FROM messages`,
		`SELECT first_opened_at FROM messages WHERE first_opened_at IS NOT NULL`, `SELECT lease_expires_at FROM leases`,
		`SELECT first_opened_at FROM tombstones WHERE first_opened_at IS NOT NULL`, `SELECT ended_at FROM tombstones`, `SELECT purge_at FROM tombstones`,
		`SELECT window_start FROM rate_counters`, `SELECT purge_at FROM rate_counters`,
	} {
		rows, queryErr := db.Query(q)
		if queryErr != nil {
			t.Fatal(queryErr)
		}
		for rows.Next() {
			var v int64
			if scanErr := rows.Scan(&v); scanErr != nil {
				t.Fatal(scanErr)
			}
			if v%minute != 0 {
				t.Errorf("%s holds %d, which is more precise than a minute", q, v)
			}
		}
		rows.Close()
	}
	var tombstones, leases, spent, nonces int
	db.QueryRow(`SELECT COUNT(*) FROM tombstones`).Scan(&tombstones)
	db.QueryRow(`SELECT COUNT(*) FROM leases`).Scan(&leases)
	db.QueryRow(`SELECT COUNT(*) FROM rate_counters WHERE key LIKE 'challenge:%' AND window_start = 0`).Scan(&spent)
	db.QueryRow(`SELECT COUNT(*) FROM rate_counters WHERE key LIKE 'pow:%' AND window_start = 0`).Scan(&nonces)
	if tombstones != 4 || leases != 1 || spent == 0 || nonces != 1 {
		t.Fatalf(
			"the flows did not leave the expected rows: %d tombstones, %d leases, %d spent challenges, %d PoW nonces",
			tombstones,
			leases,
			spent,
			nonces,
		)
	}
	rows, err := db.Query(`SELECT key FROM rate_counters`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		if scanErr := rows.Scan(&key); scanErr != nil {
			t.Fatal(scanErr)
		}
		if !slices.ContainsFunc(
			[]string{"global:", "pow:", "challenge:", "abuse:id:", "sweep:reconcile:"},
			func(p string) bool { return strings.HasPrefix(key, p) },
		) {
			t.Errorf("counter %q is not one of the address-free keys", key)
		}
		for _, message := range everyone {
			if strings.Contains(key, message.id.String()) && key != "abuse:id:"+message.id.String() {
				t.Errorf("counter %q ties message %s to something else", key, message.id)
			}
		}
	}
}

func keys(message map[string][]byte) []string {
	out := make([]string, 0, len(message))
	for k := range message {
		out = append(out, k)
	}
	return out
}

func TestAK11LogsContainNoSecrets(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	message := newMessage(t, core.KDFScrypt, "log fráze", 1, 3600, "logged text")
	message.address = "203.0.113.55"
	harness.mustCreate(message)
	response, _ := harness.metadata(message)
	codes := []int{response.StatusCode}
	response, _ = harness.claim(message, message.verifier, "")
	lease := response.Header.Get("X-Nylzo-Lease")
	if lease == "" {
		t.Fatalf("the claim gave no lease: %d", response.StatusCode)
	}
	codes = append(codes, response.StatusCode)
	response, _ = harness.blob(message, lease)
	codes = append(codes, response.StatusCode, harness.ack(message, lease).StatusCode)
	response, _ = harness.status(message)
	codes = append(codes, response.StatusCode)
	response, _ = harness.do(
		&testRequest{
			method: http.MethodPost,
			path:   "/api/v1/messages/" + message.id.String() + "/cancel",
			token:  message.manageToken,
		},
	)
	codes = append(codes, response.StatusCode)
	response, _ = harness.do(&testRequest{method: http.MethodGet, path: "/m/" + message.id.String()})
	codes = append(codes, response.StatusCode)
	if want := []int{200, 200, 200, 204, 200, 410, 200}; !slices.Equal(codes, want) {
		t.Fatalf("the flow behind the log check answered %v, want %v", codes, want)
	}
	logs := harness.logs.String()
	if !strings.Contains(logs, "POST /api/v1/messages/{id}/claim") {
		t.Fatal("log should contain the route pattern")
	}
	if strings.Contains(logs, message.id.String()) || strings.Contains(logs, hex.EncodeToString(message.id[:])) {
		t.Fatal("a log line must not name the message, so a stolen log cannot be joined with the database")
	}
	leaseBytes, _ := core.DecodeExact(lease, 32)
	forbidden := map[string][]byte{
		"S": message.secret[:],
		"R": message.link.R[:],
		"V": message.content.V[:],
		"M": message.manage[:],
		"K": message.content.K[:],
		"L": leaseBytes,
	}
	for name, b := range forbidden {
		if strings.Contains(logs, core.Encode(b)) || strings.Contains(logs, hex.EncodeToString(b)) {
			t.Fatalf("log contains %s", name)
		}
	}
	for _, needle := range []string{"Authorization", "Bearer", "203.0.113.55", "127.0.0.1", "log fráze", "logged text", "X-Nylzo"} {
		if strings.Contains(logs, needle) {
			t.Fatalf("log contains %q", needle)
		}
	}
}

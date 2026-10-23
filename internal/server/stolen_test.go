package server_test

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
	"github.com/misaon/nylzo/internal/server"
)

type stolenNeedles struct {
	t     *testing.T
	named map[string][]byte
}

func (n *stolenNeedles) secret(name string, b []byte) {
	n.named[name] = b
	n.named[name+" base64url"] = []byte(core.Encode(b))
	n.named[name+" hex"] = []byte(hex.EncodeToString(b))
}

func (n *stolenNeedles) text(name, s string) {
	n.named[name] = []byte(s)
}

func (n *stolenNeedles) message(label string, m *message, passphrase string) {
	n.t.Helper()
	n.secret(label+" S", m.secret[:])
	n.secret(label+" R", m.link.R[:])
	n.secret(label+" K", m.content.K[:])
	n.secret(label+" V", m.content.V[:])
	n.secret(label+" M", m.manage[:])
	pkR, pkV, pkM := core.PublicKey(&m.link.R), core.PublicKey(&m.content.V), core.PublicKey(m.manage)
	n.secret(label+" pk_R", pkR[:])
	n.secret(label+" pk_V", pkV[:])
	n.secret(label+" pk_M", pkM[:])
	if passphrase != "" {
		p, err := core.DerivePassphraseKey(m.kdf, passphrase, &m.link.SaltPW)
		if err != nil {
			n.t.Fatal(err)
		}
		n.secret(label+" P", p)
		n.text(label+" passphrase", passphrase)
	}
	if m.text != "" {
		n.text(label+" plaintext", m.text)
	}
}

func fileMessage(t *testing.T, content []byte) *message {
	t.Helper()
	m := newMessage(t, core.KDFNone, "", 1, 3600, "unused")
	m.text = ""
	object, err := core.Seal(&m.content.K, core.NewHeader(core.KDFNone), m.id, m.policy, core.Payload{
		Meta: core.Meta{File: &core.FileMeta{Name: "quarterly-plan-sentinel.pdf", Mime: "application/pdf", Size: int64(len(content))}},
		File: content,
	})
	if err != nil {
		t.Fatal(err)
	}
	m.object = object
	return m
}

func TestStolenServerRevealsNothing(t *testing.T) {
	cfg := baseConfig(pepperA, nil)
	cfg.Rates.GlobalDailyCreates = 16
	cfg.PoWDifficulty = 1
	h := newHarness(t, cfg)
	fb := h.withFaultyBlobs()
	needles := &stolenNeedles{t: t, named: map[string][]byte{}}
	const ip4, ip6 = "203.0.113.77", "2001:db8:77::1"
	needles.text("IPv4 address", ip4)
	needles.text("IPv6 address", ip6)
	needles.text("IPv6 address, expanded", netip.MustParseAddr(ip6).StringExpanded())
	raw6 := netip.MustParseAddr(ip6).As16()
	needles.named["IPv6 address, raw"] = raw6[:]
	var lease string
	claimed := func(m *message, verifier, ip string) {
		t.Helper()
		resp, _ := h.claim(m, verifier, ip)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("claim: %d", resp.StatusCode)
		}
		lease = resp.Header.Get("X-Nylzo-Lease")
		raw, err := core.DecodeExact(lease, core.TokenLen)
		if err != nil {
			t.Fatal(err)
		}
		needles.secret("lease of "+m.id.String(), raw)
	}

	expired := newMessage(t, core.KDFNone, "", 1, 60, "plaintext expired sentinel")
	expired.ip = ip4
	h.mustCreate(expired)
	needles.message("expired", expired, "")
	h.clock.Advance(2*time.Minute + 17*time.Second)
	if _, meta := h.metadata(expired); meta["state"] != "expired" {
		t.Fatalf("expired: %v", meta)
	}

	opened := newMessage(t, core.KDFNone, "", 1, 3600, "plaintext opened sentinel")
	opened.ip = ip4
	h.mustCreate(opened)
	needles.message("opened", opened, "")
	claimed(opened, "", ip4)
	if resp := h.ack(opened, lease); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("ack: %d", resp.StatusCode)
	}

	const pass = "correct horse battery staple"
	leased := newMessage(t, core.KDFPBKDF2, pass, 2, 3600, "plaintext leased sentinel")
	leased.ip = ip6
	h.mustCreate(leased)
	needles.message("leased", leased, pass)
	h.claim(leased, core.Encode(make([]byte, 32)), ip6)
	claimed(leased, leased.verifier, ip6)

	content := bytes.Repeat([]byte("file-content-sentinel "), 3000)
	file := fileMessage(t, content)
	file.ip = ip4
	h.mustCreate(file)
	needles.message("file", file, "")
	needles.text("file name", "quarterly-plan-sentinel.pdf")
	needles.named["file content"] = content[:64]

	burned := newMessage(t, core.KDFNone, "", 1, 3600, "plaintext burned sentinel")
	burned.ip = ip4
	h.mustCreate(burned)
	needles.message("burned", burned, "")
	if resp, _ := h.post(burned, "burn", burned.readToken); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("burn: %d", resp.StatusCode)
	}

	cancelled := newMessage(t, core.KDFPBKDF2, pass, 1, 3600, "plaintext cancelled sentinel")
	cancelled.ip = ip6
	h.mustCreate(cancelled)
	needles.message("cancelled", cancelled, pass)
	if resp, _ := h.post(cancelled, "cancel", cancelled.manageTok); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("cancel: %d", resp.StatusCode)
	}

	stuck := newMessage(t, core.KDFNone, "", 1, 3600, "plaintext stuck sentinel")
	stuck.ip = ip4
	h.mustCreate(stuck)
	needles.message("stuck", stuck, "")
	fb.set(func(b *faultyBlobs) {
		b.deleteErr[stuck.id.String()] = &fs.PathError{Op: "remove", Path: filepath.Join(h.dir, "blobs", hex.EncodeToString([]byte(stuck.id.String()))), Err: syscall.EROFS}
	})
	if resp, _ := h.post(stuck, "burn", stuck.readToken); resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("burn with a failing blob store: %d", resp.StatusCode)
	}
	if !strings.Contains(h.logBuf.String(), "remove: "+syscall.EROFS.Error()) {
		t.Fatal("the failing delete was not logged")
	}

	pow := newMessage(t, core.KDFNone, "", 1, 3600, "plaintext pow sentinel")
	pow.ip = ip6
	filler := newMessage(t, core.KDFNone, "", 1, 3600, "filler")
	filler.ip = ip4
	h.mustCreate(filler)
	_, instance := h.do(request{method: "GET", path: "/api/v1/instance"})
	powInfo := decodeJSON(t, instance)["pow"].(map[string]any)
	if powInfo["required"] != true {
		t.Fatalf("PoW is not required yet: %v", powInfo)
	}
	token, err := server.SolvePoW(powInfo["challenge"].(string), pow.id, 1)
	if err != nil {
		t.Fatal(err)
	}
	if resp, body := h.do(request{method: "POST", path: "/api/v1/messages", body: pow.object, ip: pow.ip, headers: map[string]string{"Content-Type": "application/octet-stream", "X-Nylzo-Create": pow.createHeader(0), "X-Nylzo-Pow": token}}); resp.StatusCode != http.StatusCreated {
		t.Fatalf("create with PoW: %d %s", resp.StatusCode, body)
	}
	needles.message("pow", pow, "")

	link := core.FormatLink("https://nylzo.example", opened.id, opened.kdf, opened.secret)
	body, _ := json.Marshal(map[string]string{"id": leased.id.String(), "reason": "spam, see " + link})
	if resp, _ := h.do(request{method: "POST", path: "/api/v1/abuse", ip: ip4, body: body, token: leased.readToken, headers: map[string]string{"Content-Type": "application/json"}}); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("abuse report: %d", resp.StatusCode)
	}

	stolen := map[string][]byte{"log": []byte(h.logBuf.String())}
	if err := filepath.WalkDir(h.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(h.dir, path)
		stolen[rel] = data
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"nylzo.db", "nylzo.db-wal", filepath.Join("abuse", h.clock.Now().UTC().Format(time.DateOnly)+".jsonl")} {
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
	everyone := []*message{expired, opened, leased, file, burned, cancelled, stuck, pow}
	for _, m := range everyone {
		forms := []string{m.id.String(), hex.EncodeToString([]byte(m.id.String())), hex.EncodeToString(m.id[:])}
		if slices.ContainsFunc(forms, func(form string) bool { return bytes.Contains(stolen["log"], []byte(form)) }) {
			t.Errorf("the log names message %s", m.id)
		}
	}

	db, err := sql.Open("sqlite", "file:"+filepath.Join(h.dir, "nylzo.db"))
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
		rows, err := db.Query(q)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var v int64
			if err := rows.Scan(&v); err != nil {
				t.Fatal(err)
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
		t.Fatalf("the flows did not leave the expected rows: %d tombstones, %d leases, %d spent challenges, %d PoW nonces", tombstones, leases, spent, nonces)
	}
	rows, err := db.Query(`SELECT key FROM rate_counters`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			t.Fatal(err)
		}
		if !slices.ContainsFunc([]string{"global:", "pow:", "challenge:", "abuse:id:", "sweep:reconcile:"}, func(p string) bool { return strings.HasPrefix(key, p) }) {
			t.Errorf("counter %q is not one of the address-free keys", key)
		}
		for _, m := range everyone {
			if strings.Contains(key, m.id.String()) && key != "abuse:id:"+m.id.String() {
				t.Errorf("counter %q ties message %s to something else", key, m.id)
			}
		}
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

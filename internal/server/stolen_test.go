package server

import (
	"bytes"
	"database/sql"
	"encoding/hex"
	"errors"
	"io/fs"
	"maps"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/core/envelope"
	"github.com/misaon/nylzo/internal/core/signing"
	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/server/abuse"
	"github.com/misaon/nylzo/internal/store"
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
	readKey, verifierKey := signing.PublicKey(&message.link.R), signing.PublicKey(&message.content.V)
	manageKey := signing.PublicKey(message.manage)
	n.secret(label+" pk_R", readKey[:])
	n.secret(label+" pk_V", verifierKey[:])
	n.secret(label+" pk_M", manageKey[:])
	if passphrase != "" {
		n.secret(label+" P", testPassphraseKey(message.kdf, passphrase, &message.link.PassphraseSalt))
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
	object, err := envelope.Seal(
		&message.content.K,
		envelope.NewHeader(core.KDFNone),
		message.id,
		message.policy,
		envelope.Payload{
			Meta: envelope.Meta{
				File: &envelope.FileMeta{
					Name: "quarterly-plan-sentinel.pdf",
					Mime: "application/pdf",
					Size: int64(len(content)),
				},
			},
			File: content,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	message.object = object
	return message
}

// TestAnEndedMessageStaysUnreadableWithItsLinkAndAnOlderRow covers A4-1: the file is kept open to read its own blocks.
func TestAnEndedMessageStaysUnreadableWithItsLinkAndAnOlderRow(t *testing.T) {
	t.Parallel()
	harness := newHarness(t, baseSettings(pepperA, nil))
	for _, ending := range []string{"burn", "ack"} {
		message := newMessage(t, core.KDFNone, "", 1, 3600, "ending sentinel "+ending)
		harness.mustCreate(message)
		older, err := harness.store.Load(t.Context(), seal.LocatorOf(message.id[:]))
		if err != nil {
			t.Fatal(err)
		}
		stored, err := os.Open(filepath.Join(harness.directory, "blobs", hex.EncodeToString([]byte(older.BlobRef))))
		if err != nil {
			t.Fatal(err)
		}
		defer stored.Close()
		switch ending {
		case "burn":
			if response, _ := harness.post(message, "burn", message.readToken); response.StatusCode != http.StatusNoContent {
				t.Fatalf("burn: %d", response.StatusCode)
			}
		default:
			claimed, _ := harness.claim(message, "", message.address)
			if response := harness.ack(message, claimed.Header.Get("X-Nylzo-Lease")); response.StatusCode != http.StatusNoContent {
				t.Fatalf("ack: %d", response.StatusCode)
			}
		}
		readKey := signing.PublicKey(&message.link.R)
		record, err := openRow(&older, seal.Read, readKey[:])
		if err != nil {
			t.Fatal(err)
		}
		if _, err = record.BlobKey(); err != nil {
			t.Fatalf("%s: the older row must still give the blob key, as a remnant on the disk would: %v", ending, err)
		}
		if _, err = seal.ReadEraseBlock(stored); !errors.Is(err, seal.ErrErased) {
			t.Fatalf("%s: the erase block survived in the blob's own blocks, so the link opens it: %v", ending, err)
		}
	}
}

func TestStolenServerRevealsNothing(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		settings := baseSettings(pepperA, nil)
		settings.Rates.GlobalDailyCreates = 18
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
			raw, err := core.DecodeExact(lease, core.SeedLen)
			if err != nil {
				t.Fatal(err)
			}
			needles.secret("lease of "+message.id.String(), raw)
		}

		expired := newMessage(t, core.KDFNone, "", 1, 60, "plaintext expired sentinel")
		expired.address = ip4
		harness.mustCreate(expired)
		needles.addMessage("expired", expired, "")
		synctest.Sleep(2*time.Minute + 17*time.Second)
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

		const passphrase = "correct horse battery staple"
		leased := newMessage(t, core.KDFScrypt, passphrase, 2, 3600, "plaintext leased sentinel")
		leased.address = ip6
		harness.mustCreate(leased)
		needles.addMessage("leased", leased, passphrase)
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

		cancelled := newMessage(t, core.KDFScrypt, passphrase, 1, 3600, "plaintext cancelled sentinel")
		cancelled.address = ip6
		harness.mustCreate(cancelled)
		needles.addMessage("cancelled", cancelled, passphrase)
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
		faulty.FailDelete(harness.blobRef(stuck), &fs.PathError{
			Op:   "remove",
			Path: filepath.Join(harness.directory, "blobs", hex.EncodeToString([]byte(harness.blobRef(stuck)))),
			Err:  syscall.EROFS,
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

		removed := newMessage(t, core.KDFNone, "", 1, 3600, "plaintext removed sentinel")
		removed.address = ip4
		harness.mustCreate(removed)
		needles.addMessage("removed", removed, "")
		if _, err := store.TakeDown(
			t.Context(),
			harness.store,
			harness.blobs,
			seal.LocatorOf(removed.id[:]),
			time.Now(),
		); err != nil {
			t.Fatal(err)
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
		contact := "notifier.contact@example.cz"
		report := map[string]any{
			"id":         leased.id.String(),
			"reason":     "spam, see " + link,
			"contact":    contact,
			"good_faith": true,
		}
		if got := reportAbuse(t, harness, leased.readToken, ip4, report); got != "202 accepted" {
			t.Fatalf("abuse report: %s", got)
		}

		everyone := []*testMessage{expired, opened, leased, file, burned, cancelled, stuck, filler, pow, removed}
		for _, message := range everyone {
			needles.secret("id of "+message.id.String(), message.id[:])
			needles.text("id text of "+message.id.String(), message.id.String())
			needles.text("hex id text of "+message.id.String(), hex.EncodeToString([]byte(message.id.String())))
			needles.secret("envelope header of "+message.id.String(), message.object[:envelope.HeaderLen])
			if record, err := harness.record(message); err == nil {
				needles.secret("record key of "+message.id.String(), record.Key[:])
			}
		}
		needles.text("contact", contact)
		needles.text("reason", "spam, see")
		harness.server.requestCounts.Flush()
		stolen := harness.stolenCopy()
		reportFile := filepath.Join("abuse", time.Now().UTC().Format(time.DateOnly)+abuse.FileExtension)
		for _, want := range []string{"nylzo.db", "nylzo.db-wal", reportFile} {
			if _, ok := stolen[want]; !ok {
				t.Fatalf("the stolen copy misses %s: %v", want, slices.Sorted(maps.Keys(stolen)))
			}
		}
		reports, err := abuse.Open(testReportKey, stolen[reportFile])
		if err != nil || len(reports) != 1 || reports[0].ID != leased.id.String() || reports[0].Contact != contact ||
			!reports[0].GoodFaith {
			t.Errorf("the operator's key must open the sealed report: %+v %v", reports, err)
		}
		for where, data := range stolen {
			for what, needle := range needles.named {
				if bytes.Contains(data, needle) {
					t.Errorf("%s reveals %s", where, what)
				}
			}
		}
		checkStolenDatabase(t, harness, len(everyone))
	})
}

func (h *testHarness) stolenCopy() map[string][]byte {
	h.t.Helper()
	stolen := map[string][]byte{"log": []byte(h.logs.String())}
	if err := filepath.WalkDir(h.directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(h.directory, path)
		stolen[rel] = data
		return nil
	}); err != nil {
		h.t.Fatal(err)
	}
	return stolen
}

// checkStolenDatabase covers D101: a row shows its locator, sealed bytes and two rounded cleanup times, nothing else.
func checkStolenDatabase(t *testing.T, harness *testHarness, created int) {
	t.Helper()
	database, err := sql.Open("sqlite", "file:"+filepath.Join(harness.directory, "nylzo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	columns := column[string](t, database, `SELECT name FROM pragma_table_info('messages') ORDER BY cid`)
	if !slices.Equal(
		columns,
		[]string{"locator", "read_wrap", "manage_wrap", "record", "blob_ref", "blob_wrap", "blob_purge_at", "purge_at"},
	) {
		t.Fatalf("messages keeps more columns than the spec lists: %v", columns)
	}
	if tables := column[string](
		t,
		database,
		`SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name`,
	); !slices.Equal(
		tables,
		[]string{"messages", "rate_counters", "wal_scrub"},
	) {
		t.Fatalf("the database keeps more tables than the spec lists: %v", tables)
	}
	for query, step := range map[string]time.Duration{
		`SELECT blob_purge_at FROM messages WHERE blob_purge_at IS NOT NULL`: store.BlobPurgeStep,
		`SELECT purge_at FROM messages`:                                      store.RowPurgeStep,
		`SELECT window_start FROM rate_counters`:                             time.Minute,
		`SELECT purge_at FROM rate_counters`:                                 time.Minute,
	} {
		for _, value := range column[int64](t, database, query) {
			if value%step.Milliseconds() != 0 {
				t.Errorf("%s holds %d, which is more precise than %s", query, value, step)
			}
		}
	}
	count := func(query string) int64 { return column[int64](t, database, query)[0] }
	rows, ended := count(
		`SELECT COUNT(*) FROM messages`,
	), count(
		`SELECT COUNT(*) FROM messages WHERE blob_wrap IS NULL`,
	)
	spent := count(`SELECT COUNT(*) FROM rate_counters WHERE key LIKE 'challenge:%' AND window_start = 0`)
	nonces := count(`SELECT COUNT(*) FROM rate_counters WHERE key LIKE 'pow:%' AND window_start = 0`)
	if rows != int64(created)+1 || ended != 6 || spent == 0 || nonces != 1 {
		t.Fatalf(
			"the flows did not leave the expected rows: %d rows, %d without a blob key, %d spent challenges, %d PoW nonces",
			rows,
			ended,
			spent,
			nonces,
		)
	}
	if filled := count(
		`SELECT COUNT(*) FROM wal_scrub WHERE generation NOT IN (0, 1) OR filler != zeroblob(length(filler))`,
	); filled != 0 {
		t.Fatalf("wal_scrub holds %d rows with something other than zeros", filled)
	}
	for _, key := range column[string](t, database, `SELECT key FROM rate_counters`) {
		if !slices.ContainsFunc(
			[]string{"global:", "pow:", "challenge:", "sweep:reconcile:"},
			func(prefix string) bool { return strings.HasPrefix(key, prefix) },
		) {
			t.Errorf("counter %q is not one of the address-free keys", key)
		}
	}
}

func column[T any](t *testing.T, database *sql.DB, query string) []T {
	t.Helper()
	rows, err := database.QueryContext(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var values []T
	for rows.Next() {
		var value T
		if scanErr := rows.Scan(&value); scanErr != nil {
			t.Fatal(scanErr)
		}
		values = append(values, value)
	}
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
	return values
}

func TestAK11LogsContainNoSecrets(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
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
		harness.server.requestCounts.Flush()
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
		for name, forbiddenBytes := range forbidden {
			if strings.Contains(logs, core.Encode(forbiddenBytes)) ||
				strings.Contains(logs, hex.EncodeToString(forbiddenBytes)) {
				t.Fatalf("log contains %s", name)
			}
		}
		for _, needle := range []string{"Authorization", "Bearer", "203.0.113.55", "127.0.0.1", "log fráze", "logged text", "X-Nylzo"} {
			if strings.Contains(logs, needle) {
				t.Fatalf("log contains %q", needle)
			}
		}
	})
}

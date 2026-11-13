package sqlite_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	mathrand "math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/seal"
	"github.com/misaon/nylzo/internal/store"
	"github.com/misaon/nylzo/internal/store/sqlite"
)

func TestPurgedMessageLeavesNoResidue(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := filepath.Join(directory, "nylzo.db")
	storage, blobs := openStore(t, path), newBlobs(t, directory)
	record := newRecord(1, true)
	if _, err := blobs.PutExpiring(
		t.Context(),
		record.Row.BlobReference,
		bytes.NewReader(make([]byte, 64)),
		64,
		time.Hour,
	); err != nil {
		t.Fatal(err)
	}
	create(t, storage, record)
	locator, sealed := record.Row.Locator, slices.Clone(record.Row.Record)
	claimedAt := testNow.Add(time.Minute)
	parameters := claimParameters(claimedAt)
	parameters.Verifier = store.Verifier{Sent: true, Check: func(byte, []byte) bool { return true }}
	claimed, err := store.Claim(t.Context(), storage, record, parameters)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Ack(t.Context(), storage, blobs, record, claimed.Lease.MAC, claimedAt); err != nil {
		t.Fatal(err)
	}
	if _, err = store.SweepAll(t.Context(), storage, blobs, testNow.Add(9*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = storage.Load(t.Context(), locator); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("the ended message was not purged: %v", err)
	}
	for _, name := range []string{path, path + "-wal"} {
		data, readErr := os.ReadFile(name)
		if readErr != nil && !os.IsNotExist(readErr) {
			t.Fatal(readErr)
		}
		if bytes.Contains(data, locator[:]) || bytes.Contains(data, sealed[:64]) ||
			bytes.Contains(data, []byte(record.Row.BlobReference)) {
			t.Fatalf("%s still holds a trace of a purged message", filepath.Base(name))
		}
	}
}

// TestTheDatabaseHoldsNoMessageFieldInTheClear covers D101: only the locator, sealed bytes and two rounded times stay readable.
func TestTheDatabaseHoldsNoMessageFieldInTheClear(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "nylzo.db")
	storage := openStore(t, path)
	locked := newRecord(1, true)
	create(t, storage, locked)
	attemptAt := testNow.Add(17*time.Second + 123*time.Millisecond)
	parameters := claimParameters(attemptAt)
	parameters.Verifier = store.Verifier{Sent: true, Check: func(byte, []byte) bool { return false }}
	for attempt := 1; attempt <= store.FreeAttempts+1; attempt++ {
		if _, err := store.Claim(t.Context(), storage, locked, parameters); !isPassphraseError(err) {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}
	shared := newRecord(3, false)
	create(t, storage, shared)
	var leaseMACs [][]byte
	for range 3 {
		leaseMACs = append(leaseMACs, claim(t, storage, shared, attemptAt).Lease.MAC)
	}
	storage.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for name, secret := range map[string][]byte{
		"a verifier mac":       testMAC,
		"a lease mac":          leaseMACs[0],
		"the record key":       shared.Key[:],
		"a time to the second": binary.BigEndian.AppendUint64(nil, uint64(testNow.UnixMilli())),
	} {
		if bytes.Contains(data, secret) {
			t.Errorf("the database file holds %s in the clear", name)
		}
	}
	rows, err := openRaw(t, path).Query(`SELECT blob_purge_at, purge_at FROM messages`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var blobPurgeAt, purgeAt int64
		if err = rows.Scan(&blobPurgeAt, &purgeAt); err != nil {
			t.Fatal(err)
		}
		if blobPurgeAt%store.BlobPurgeStep.Milliseconds() != 0 || purgeAt%store.RowPurgeStep.Milliseconds() != 0 {
			t.Fatalf("a cleanup time is finer than its step: %d %d", blobPurgeAt, purgeAt)
		}
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
}

// TestLookupDoesTheSameWorkForEveryLocator covers AK21 and F1; it runs alone because allocations are counted per
// process.
func TestLookupDoesTheSameWorkForEveryLocator(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nylzo.db")
	storage := openStore(t, path)
	live, ended := newRecord(1, false), newRecord(1, false)
	create(t, storage, live)
	create(t, storage, ended)
	if _, err := openRaw(t, path).Exec(
		`UPDATE messages SET blob_ref = NULL, blob_wrap = NULL, blob_purge_at = NULL WHERE locator = ?`,
		ended.Row.Locator[:],
	); err != nil {
		t.Fatal(err)
	}
	unknown := seal.LocatorOf([]byte("never created"))
	work := map[string]float64{}
	lengths := map[string]int{}
	for name, locator := range map[string]seal.Locator{
		"live":    live.Row.Locator,
		"ended":   ended.Row.Locator,
		"unknown": unknown,
	} {
		row, known, err := storage.Lookup(t.Context(), locator)
		if err != nil || known == (name == "unknown") {
			t.Fatalf("%s: known %v, %v", name, known, err)
		}
		lengths[name] = len(row.ReadWrap) + len(row.ManageWrap) + len(row.Record)
		work[name] = math.MaxFloat64
		for range 20 {
			work[name] = min(work[name], testing.AllocsPerRun(20, func() { storage.Lookup(t.Context(), locator) }))
		}
	}
	const driverNoise = 5
	spread := max(work["live"], work["ended"], work["unknown"]) - min(work["live"], work["ended"], work["unknown"])
	if spread > driverNoise || lengths["live"] != lengths["unknown"] || lengths["ended"] != lengths["unknown"] {
		t.Fatalf("a lookup must do the same work for every locator: allocations %v, sealed bytes %v", work, lengths)
	}
}

func isPassphraseError(err error) bool {
	_, ok := errors.AsType[*store.PassphraseError](err)
	return ok
}

// TestTheLogKeepsNoOlderRowVersionAfterAWrite reproduces the audit probe: a write that fills fewer frames than an
// earlier one leaves the older frames behind, so every write asks for a scrub.
func TestTheLogKeepsNoOlderRowVersionAfterAWrite(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "nylzo.db")
	storage := openStore(t, path)
	records := make([]*store.Record, 40)
	for i := range records {
		records[i] = newRecord(2, false)
		hit := store.RateHit{Key: "global:stored:d", WindowStart: testNow, PurgeAt: testNow.Add(time.Hour)}
		create(t, storage, records[i], hit)
	}
	hits := make([]store.RateHit, 0, 300)
	for i := range cap(hits) {
		hits = append(hits, store.RateHit{Key: fmt.Sprintf("counter:%03d", i), PurgeAt: testNow.Add(time.Hour)})
	}
	if _, err := storage.RateHit(t.Context(), hits); err != nil {
		t.Fatal(err)
	}
	older := make([][]byte, 0, 10)
	for _, record := range records[20:30] {
		older = append(older, slices.Clone(record.Row.Record[:64]))
		claim(t, storage, record, testNow)
	}
	current := make([][]byte, 0, len(records))
	for _, record := range records {
		current = append(current, record.Row.Record[:64])
	}
	var stale []string
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		wal, err := os.ReadFile(path + "-wal")
		if err != nil {
			t.Fatal(err)
		}
		if stale = staleFrames(wal, older, current); len(stale) == 0 {
			return
		}
	}
	t.Fatalf("the log still holds older row versions after the scrubs: %v", stale)
}

// staleFrames reads every frame of the log: none may hold an older version of a row, and a frame that the last log
// generation did not write may hold no row at all, since it would show the order of earlier writes.
func staleFrames(wal []byte, older, current [][]byte) []string {
	const header, frameHeader = 32, 24
	if len(wal) < header {
		return nil
	}
	pageSize := int(binary.BigEndian.Uint32(wal[8:12]))
	salts := wal[16:24]
	var stale []string
	for offset, frame := header, 1; offset+frameHeader+pageSize <= len(wal); offset, frame = offset+frameHeader+pageSize, frame+1 {
		page := wal[offset+frameHeader : offset+frameHeader+pageSize]
		earlier := !bytes.Equal(wal[offset+8:offset+16], salts)
		for _, record := range older {
			if bytes.Contains(page, record) {
				stale = append(stale, fmt.Sprintf("frame %d: a record before its claim", frame))
			}
		}
		for _, record := range current {
			if earlier && bytes.Contains(page, record) {
				stale = append(stale, fmt.Sprintf("frame %d: a row from an earlier generation of the log", frame))
			}
		}
	}
	return stale
}

func TestABlockedCheckpointFailsTheSweepQuickly(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := filepath.Join(directory, "nylzo.db")
	storage, blobs := openStore(t, path), newBlobs(t, directory)
	hitCounter(t, storage, "a")
	reading, err := openRaw(t, path).Begin()
	if err != nil {
		t.Fatal(err)
	}
	var counters int
	if err = reading.QueryRow(`SELECT COUNT(*) FROM rate_counters`).Scan(&counters); err != nil {
		t.Fatal(err)
	}
	hitCounter(t, storage, "after the reader")
	start := time.Now()
	if err = storage.Checkpoint(t.Context()); !errors.Is(err, sqlite.ErrCheckpointBusy) {
		t.Fatalf("a checkpoint with a reader holding the WAL: %v", err)
	}
	if held := time.Since(start); held > 2*time.Second {
		t.Fatalf("a blocked checkpoint held the only writer for %v", held)
	}
	if _, err = store.SweepAll(t.Context(), storage, blobs, time.Now()); !errors.Is(err, sqlite.ErrCheckpointBusy) {
		t.Fatalf("a sweep with a reader holding the WAL: %v", err)
	}
	reading.Rollback()
	if _, err = store.SweepAll(t.Context(), storage, blobs, time.Now()); err != nil {
		t.Fatalf("a sweep after the reader left: %v", err)
	}
	locking, err := openRaw(t, path, "_txlock=immediate").Begin()
	if err != nil {
		t.Fatal(err)
	}
	released := time.AfterFunc(time.Second, func() { locking.Rollback() })
	defer released.Stop()
	hitCounter(t, storage, "b")
}

func TestPreparedStatementsReleaseTheLog(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "nylzo.db")
	storage, ctx := openStore(t, path), t.Context()
	record := newRecord(2, false)
	create(
		t,
		storage,
		record,
		store.RateHit{Key: "global:stored:d", WindowStart: testNow, PurgeAt: testNow.Add(time.Hour)},
	)
	claim(t, storage, record, testNow)
	hit := store.RateHit{Key: "r", WindowStart: testNow, PurgeAt: testNow.Add(time.Hour), Limit: 1}
	errs := []error{}
	_, err := storage.Load(ctx, record.Row.Locator)
	errs = append(errs, err)
	_, err = storage.RateHit(ctx, []store.RateHit{hit})
	errs = append(errs, err)
	_, err = storage.RateReserve(ctx, hit)
	errs = append(errs, err)
	if err = errors.Join(errs...); err != nil {
		t.Fatal(err)
	}
	if err = storage.Checkpoint(ctx); err != nil {
		t.Fatalf("a prepared statement kept a read open: %v", err)
	}
}

// TestTheLogKeepsItsBlocksAndHoldsNoOlderRowVersion covers A4-1: a truncated log leaves its frames in freed blocks.
func TestTheLogKeepsItsBlocksAndHoldsNoOlderRowVersion(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "nylzo.db")
	storage := openStore(t, path)
	var largest int64
	checkLog := func(step string) {
		info, err := os.Stat(path + "-wal")
		if err != nil || info.Size() < largest {
			t.Fatalf(
				"%s: the log shrank from %d bytes, which frees blocks that hold its frames: %v",
				step,
				largest,
				err,
			)
		}
		largest = info.Size()
	}
	hits := make([]store.RateHit, 0, 300)
	for i := range cap(hits) {
		hits = append(hits, store.RateHit{Key: fmt.Sprintf("spend:%03d", i), PurgeAt: testNow.Add(time.Hour)})
	}
	if _, err := storage.RateHit(t.Context(), hits); err != nil {
		t.Fatal(err)
	}
	for range 30 {
		create(t, storage, newRecord(1, false))
	}
	if err := storage.Checkpoint(t.Context()); err != nil {
		t.Fatal(err)
	}
	snapshot, beginErr := openRaw(t, path).Begin()
	if beginErr != nil {
		t.Fatal(beginErr)
	}
	var messages int
	if err := snapshot.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&messages); err != nil {
		t.Fatal(err)
	}
	record := newRecord(2, false)
	create(t, storage, record, hits...)
	checkLog("create")
	older := slices.Clone(record.Row.Record)
	claim(t, storage, record, testNow)
	checkLog("claim")
	if before, err := os.ReadFile(path + "-wal"); err != nil || !bytes.Contains(before, older[:64]) {
		t.Fatalf("the test needs a stale frame with the older record before the scrub: %v", err)
	}
	if err := snapshot.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := storage.Checkpoint(t.Context()); err != nil {
		t.Fatal(err)
	}
	checkLog("checkpoint")
	for _, name := range []string{path, path + "-wal"} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, older[:64]) {
			t.Fatalf("%s still holds the record before the claim after the scrub", filepath.Base(name))
		}
	}
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}
	checkLog("close")
}

// TestChurnAndPurgeLeaveNoCopyOfARow reproduces the audit probe: a page split leaves copies of the cells it moved in
// the free space of the page, which secure_delete never zeroes, so only the rebuild in the sweep removes them. The
// sweep rebuilds after writes of its own process and after commits of another one.
func TestChurnAndPurgeLeaveNoCopyOfARow(t *testing.T) {
	t.Parallel()
	t.Run("swept by the process that wrote", func(t *testing.T) {
		t.Parallel()
		directory := t.TempDir()
		path := filepath.Join(directory, "nylzo.db")
		storage := openStore(t, path)
		checkpoint(t, storage)
		churned := churn(t, storage)
		if _, err := store.SweepAll(t.Context(), storage, newBlobs(t, directory), testNow); err != nil {
			t.Fatal(err)
		}
		churned.requireNoCopy(t, storage, path)
	})
	t.Run("swept by another process", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "nylzo.db")
		writing, sweeping := openStore(t, path), openStore(t, path)
		checkpoint(t, sweeping)
		churned := churn(t, writing)
		for _, purge := range []func(context.Context, time.Time, int) (int, error){writing.PurgeRows, writing.RatePurge} {
			for {
				purged, err := purge(t.Context(), testNow, store.SweepBatch)
				if err != nil {
					t.Fatal(err)
				}
				if purged < store.SweepBatch {
					break
				}
			}
		}
		checkpoint(t, sweeping)
		churned.requireNoCopy(t, sweeping, path)
	})
}

// TestResealingLeavesNoOlderWrap covers the start of serve: once the wraps are sealed again, neither free space nor
// the log keeps a wrap they replaced, or any other row version that churn left behind.
func TestResealingLeavesNoOlderWrap(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "nylzo.db")
	storage := openStore(t, path)
	checkpoint(t, storage)
	churned := churn(t, storage)
	err := storage.ResealWraps(t.Context(), func(row *store.Row) bool {
		if row.Locator == store.DummyLocator {
			return false
		}
		row.ReadWrap = churned.written.value("RWR:", seal.SealedWrapLen)
		row.ManageWrap = churned.written.value("MWR:", seal.SealedWrapLen)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	churned.requireNoCopy(t, storage, path)
}

// checkpoint retries while the scrubber of another store holds the log.
func checkpoint(t *testing.T, storage *sqlite.Store) {
	t.Helper()
	err := storage.Checkpoint(t.Context())
	for retry := 0; retry < 20 && errors.Is(err, sqlite.ErrCheckpointBusy); retry++ {
		time.Sleep(50 * time.Millisecond)
		err = storage.Checkpoint(t.Context())
	}
	if err != nil {
		t.Fatal(err)
	}
}

type churned struct {
	written    *traces
	locators   []seal.Locator
	challenges []string
}

// churn creates rows and counters, changes and purges rows in turn, and leaves half the rows and counters due.
func churn(t *testing.T, storage *sqlite.Store) *churned {
	t.Helper()
	ctx, choice := t.Context(), mathrand.New(mathrand.NewPCG(1, 2))
	due, later := testNow.Add(-time.Hour), testNow.Add(24*time.Hour)
	result := &churned{written: newTraces()}
	createRow := func() {
		row := result.written.row()
		challenge := store.RateHit{Key: string(result.written.value("CHL-", 24)), PurgeAt: later}
		result.challenges = append(result.challenges, challenge.Key)
		if choice.IntN(2) == 0 {
			challenge.PurgeAt = due
		}
		if err := storage.Create(ctx, &row, challenge); err != nil {
			t.Fatal(err)
		}
		result.locators = append(result.locators, row.Locator)
	}
	update := func(locator seal.Locator, change func(row *store.Row)) {
		err := storage.Update(ctx, locator, func(row *store.Row) error {
			change(row)
			return nil
		})
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			t.Fatal(err)
		}
	}
	for range churnRows {
		createRow()
	}
	for step := range churnSteps {
		locator := result.locators[choice.IntN(len(result.locators))]
		switch step % 4 {
		case 0:
			update(locator, func(row *store.Row) { row.Record = result.written.value("REC:", seal.RecordBlock) })
		case 1:
			update(locator, func(row *store.Row) {
				row.Record, row.BlobWrap = result.written.value("REC:", seal.RecordBlock), nil
				row.PurgeAt = later.Add(time.Duration(choice.IntN(1000)) * time.Hour)
			})
		case 2:
			update(
				locator,
				func(row *store.Row) { row.BlobReference, row.BlobWrap, row.BlobPurgeAt = "", nil, time.Time{} },
			)
		default:
			update(locator, func(row *store.Row) { row.PurgeAt = due })
			if _, err := storage.PurgeRows(ctx, testNow, store.SweepBatch); err != nil {
				t.Fatal(err)
			}
			createRow()
		}
	}
	for _, locator := range result.locators {
		if choice.IntN(2) == 0 {
			update(locator, func(row *store.Row) { row.PurgeAt = due })
		}
	}
	return result
}

func (c *churned) requireNoCopy(t *testing.T, storage *sqlite.Store, path string) {
	t.Helper()
	current := c.written.current(t, storage, c.locators, c.challenges)
	for _, name := range []string{path, path + "-wal"} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if stale := c.written.staleIn(data, current); len(stale) > 0 {
			t.Errorf("%s still holds %d deleted rows or older row versions: %v", filepath.Base(name), len(stale), stale)
		}
	}
}

const (
	churnRows  = 800
	churnSteps = 3200
	traceLen   = 16
)

var traceTags = []string{"LOC:", "RWR:", "MWR:", "REC:", "BWR:", "REF-", "CHL-"}

// traces writes values whose first bytes are a tag and random bytes, so a scan of a file finds every copy of them.
type traces struct {
	random  *mathrand.ChaCha8
	written map[string]bool
}

func newTraces() *traces {
	return &traces{random: mathrand.NewChaCha8([32]byte{1}), written: map[string]bool{}}
}

func (tr *traces) value(tag string, length int) []byte {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	value := make([]byte, length)
	tr.random.Read(value)
	if strings.HasSuffix(tag, "-") {
		for i := range value {
			value[i] = alphabet[value[i]%byte(len(alphabet))]
		}
	}
	copy(value, tag)
	tr.written[string(value[:traceLen])] = true
	return value
}

func (tr *traces) row() store.Row {
	return store.Row{
		Locator:       seal.Locator(tr.value("LOC:", seal.LocatorLen)),
		ReadWrap:      tr.value("RWR:", seal.SealedWrapLen),
		ManageWrap:    tr.value("MWR:", seal.SealedWrapLen),
		Record:        tr.value("REC:", seal.RecordBlock),
		BlobReference: string(tr.value("REF-", 22)),
		BlobWrap:      tr.value("BWR:", seal.BlobWrapLen),
		BlobPurgeAt:   testNow.Add(1000 * time.Hour),
		PurgeAt:       testNow.Add(1000 * time.Hour),
	}
}

func (*traces) current(
	t *testing.T,
	storage *sqlite.Store,
	locators []seal.Locator,
	challenges []string,
) map[string]bool {
	t.Helper()
	current := map[string]bool{}
	for _, locator := range locators {
		row, err := storage.Load(t.Context(), locator)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range [][]byte{row.Locator[:], row.ReadWrap, row.ManageWrap, row.Record, []byte(row.BlobReference), row.BlobWrap} {
			if len(value) >= traceLen {
				current[string(value[:traceLen])] = true
			}
		}
	}
	for _, key := range challenges {
		count, err := storage.RateGet(t.Context(), key, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		if count.Count > 0 {
			current[key[:traceLen]] = true
		}
	}
	return current
}

func (tr *traces) staleIn(data []byte, current map[string]bool) []string {
	var stale []string
	for _, tag := range traceTags {
		for offset := 0; ; {
			at := bytes.Index(data[offset:], []byte(tag))
			if at < 0 {
				break
			}
			start := offset + at
			offset = start + len(tag)
			if start+traceLen > len(data) {
				break
			}
			if trace := string(data[start : start+traceLen]); tr.written[trace] && !current[trace] {
				stale = append(stale, fmt.Sprintf("%q at %d", trace[:len(tag)], start))
			}
		}
	}
	return stale
}

// TestOpenStartsTheLogOver covers the reset at open: the headers of the log and its index count every write since the
// log was created, so Open zeroes both in place while it holds the only connection, after a checkpoint that keeps
// every committed row.
func TestOpenStartsTheLogOver(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "nylzo.db")
	first, err := sqlite.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	records := make([]*store.Record, 30)
	for i := range records {
		records[i] = newRecord(1, false)
		create(t, first, records[i])
	}
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	counted := binary.BigEndian.Uint32(readLog(t, path)[12:16])
	if counted < uint32(len(records)) {
		t.Fatalf("the test needs a log whose header counted the earlier writes, not %d", counted)
	}
	holder := openRaw(t, path)
	if _, err = holder.Exec(`INSERT INTO rate_counters VALUES ('held', 0, 1, 0, 0)`); err != nil {
		t.Fatal(err)
	}
	held := readLog(t, path)[:32]
	skipped := openStore(t, path)
	if !skipped.LogResetSkipped() || !bytes.Equal(readLog(t, path)[:32], held) {
		t.Fatal("Open reset the log while another connection had the database open")
	}
	if err = errors.Join(holder.Close(), skipped.Close()); err != nil {
		t.Fatal(err)
	}
	before := fileInfos(t, path+"-wal", path+"-shm")
	reopened := openStore(t, path)
	zeroed := !slices.ContainsFunc(readLog(t, path), func(value byte) bool { return value != 0 })
	if reopened.LogResetSkipped() || !zeroed {
		t.Fatal("Open left the log as it was with no other connection")
	}
	for i, info := range fileInfos(t, path+"-wal", path+"-shm") {
		if !os.SameFile(before[i], info) {
			t.Fatalf("the reset replaced %s instead of overwriting it in place", info.Name())
		}
	}
	create(t, reopened, newRecord(1, false))
	if restarted := binary.BigEndian.Uint32(readLog(t, path)[12:16]); restarted >= counted {
		t.Fatalf("the log header still counts the writes before the open: %d, before it %d", restarted, counted)
	}
	for _, record := range records {
		if _, err = reopened.Load(t.Context(), record.Row.Locator); err != nil {
			t.Fatalf("the reset lost a committed row: %v", err)
		}
	}
	count, err := reopened.RateGet(t.Context(), "held", time.UnixMilli(0))
	if err != nil || count.Count != 1 {
		t.Fatalf("the reset lost a row that another connection committed: %+v %v", count, err)
	}
}

// TestOpenCheckpointsTheLogBeforeItsReset copies a database whose last commit lives only in the log, as a crash leaves
// it, and opens the copy.
func TestOpenCheckpointsTheLogBeforeItsReset(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "nylzo.db")
	openStore(t, path).Close()
	writer := openRaw(t, path, "_pragma=wal_autocheckpoint(0)")
	if _, err := writer.Exec(`INSERT INTO rate_counters VALUES ('crash', 0, 7, 0, 0)`); err != nil {
		t.Fatal(err)
	}
	copied := filepath.Join(t.TempDir(), "nylzo.db")
	for _, suffix := range []string{"", "-wal"} {
		data, err := os.ReadFile(path + suffix)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(copied+suffix, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	reopened := openStore(t, copied)
	if reopened.LogResetSkipped() {
		t.Fatal("Open skipped the reset of a copy that nothing else opened")
	}
	if count, err := reopened.RateGet(t.Context(), "crash", time.UnixMilli(0)); err != nil || count.Count != 7 {
		t.Fatalf("the reset lost a commit that lived only in the log: %+v %v", count, err)
	}
}

func readLog(t *testing.T, path string) []byte {
	t.Helper()
	wal, err := os.ReadFile(path + "-wal")
	if err != nil || len(wal) < 32 {
		t.Fatalf("read the log: %d bytes, %v", len(wal), err)
	}
	return wal
}

func fileInfos(t *testing.T, paths ...string) []os.FileInfo {
	t.Helper()
	infos := make([]os.FileInfo, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		infos = append(infos, info)
	}
	return infos
}

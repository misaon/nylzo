//go:build !windows

package disk

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/store"
)

var errFault = errors.New("fault")

func failingStat(*os.File) (os.FileInfo, error) { return nil, errFault }

func newInternal(t *testing.T) *Blobs {
	t.Helper()
	directory := t.TempDir()
	blobs, err := New(directory, WithPool(Pool{Size: 2, Batch: 1}))
	if err != nil {
		t.Fatal(err)
	}
	return blobs
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func chmod(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0o700) })
}

func requireUnprivileged(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root reads and writes whatever the mode")
	}
}

func removePool(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, temporaryDirectory))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if err = os.Remove(filepath.Join(root, temporaryDirectory, entry.Name())); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPoolForAssumesWhatTheOperatorLeftOpen(t *testing.T) {
	t.Parallel()
	if PoolFor(0, 0, 0) != PoolFor(assumedDailyCreates, assumedSweepInterval, assumedMaxTTL) {
		t.Fatal("the pool without a cap, interval or TTL must follow the assumed ones")
	}
}

type bareInfo struct{ os.FileInfo }

func (bareInfo) Sys() any { return nil }

func TestAFileWithoutLinkCountCountsAsItsOnlyName(t *testing.T) {
	t.Parallel()
	if linkCount(bareInfo{}) != 1 {
		t.Fatal("a file whose link count is unknown must be shredded, never only unlinked")
	}
}

func TestAnUploadSkipsAPoolFileThatIsNoFreshEmptyFile(t *testing.T) {
	t.Parallel()
	blobs := newInternal(t)
	root := blobs.root
	removePool(t, root)
	temporary := filepath.Join(root, temporaryDirectory)
	if err := syscall.Mkfifo(filepath.Join(temporary, poolPrefix+"fifo"), 0o600); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(temporary, poolPrefix+"full"), "data")
	write(t, filepath.Join(temporary, poolPrefix+"linked"), "")
	if err := os.Link(filepath.Join(temporary, poolPrefix+"linked"), filepath.Join(root, "6c696e6b6564")); err != nil {
		t.Fatal(err)
	}
	blobs.knowPool([]string{poolPrefix + "fifo"})
	if _, err := blobs.takeRandom(); !errors.Is(err, store.ErrPoolEmpty) {
		t.Fatalf("the default report: %v", err)
	}
	var reported []error
	blobs.OnPoolFailure(func(err error) { reported = append(reported, err) })
	blobs.knowPool([]string{poolPrefix + "missing", poolPrefix + "fifo"})
	if _, err := blobs.takeRandom(); !errors.Is(err, store.ErrPoolEmpty) {
		t.Fatalf("a pool of files no upload may take: %v", err)
	}
	if len(reported) != 1 || !errors.Is(reported[0], syscall.ENXIO) {
		t.Fatalf("only the file it could not open is reported: %v", reported)
	}
	names, err := blobs.poolFiles()
	if err != nil || len(names) != 2 {
		t.Fatalf("a skipped file must stay where it is: %v %v", names, err)
	}
}

func TestAClaimOfAFileThatChangedMeanwhileIsTaken(t *testing.T) {
	t.Parallel()
	blobs := newInternal(t)
	root := blobs.root
	var reported []error
	blobs.OnPoolFailure(func(err error) { reported = append(reported, err) })
	pooled := filepath.Join(root, temporaryDirectory, poolPrefix+"claimed")
	write(t, pooled, "")
	file, err := os.OpenFile(pooled, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = blobs.claimOpened(file, pooled, failingStat); !errors.Is(err, errTaken) ||
		!slices.Equal(reported, []error{errFault}) {
		t.Fatalf("a file it cannot stat: %v %v", err, reported)
	}
	file, err = os.OpenFile(pooled, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(pooled, pooled+"-elsewhere"); err != nil {
		t.Fatal(err)
	}
	if _, err = blobs.claimOpened(file, pooled, (*os.File).Stat); !errors.Is(err, errTaken) {
		t.Fatalf("a file another claim renamed: %v", err)
	}
}

func TestAnOpenThatCannotSizeTheBlobClosesIt(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "blob")
	write(t, path, "content")
	if file, _, err := openSized(path, failingStat); file != nil || !errors.Is(err, errFault) {
		t.Fatalf("a blob it cannot stat: %v", err)
	}
}

func TestAGivenBackFileThatWentLeavesThePoolAlone(t *testing.T) {
	t.Parallel()
	blobs := newInternal(t)
	root := blobs.root
	before := slices.Clone(blobs.poolNames)
	blobs.giveBack(&upload{path: filepath.Join(root, temporaryDirectory, uploadPrefix+"gone")})
	if !slices.Equal(blobs.poolNames, before) {
		t.Fatalf("a file that went joined the pool: %v", blobs.poolNames)
	}
}

func TestDiscardShredsDataAndLeavesWhatWentOrCannotBeRead(t *testing.T) {
	t.Parallel()
	blobs := newInternal(t)
	root := blobs.root
	temporary := filepath.Join(root, temporaryDirectory)
	write(t, filepath.Join(temporary, poolPrefix+"full"), "data")
	if err := blobs.discard(poolPrefix + "full"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(temporary, poolPrefix+"full")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a pool file with data stayed: %v", err)
	}
	if err := blobs.discard(poolPrefix + "missing"); err != nil {
		t.Fatalf("a file that went: %v", err)
	}
	write(t, filepath.Join(temporary, poolPrefix+"file"), "")
	if err := blobs.discard(poolPrefix + "file/below"); !errors.Is(err, syscall.ENOTDIR) {
		t.Fatalf("a name below a file: %v", err)
	}
}

func TestStaleTemporaryFilesThatShareAFileAreOnlyUnlinked(t *testing.T) {
	t.Parallel()
	blobs := newInternal(t)
	root := blobs.root
	temporary := filepath.Join(root, temporaryDirectory)
	stale := filepath.Join(temporary, uploadPrefix+"stale")
	published := filepath.Join(root, "7075626c6973686564")
	write(t, stale, "published content")
	if err := os.Link(stale, published); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	index := slices.IndexFunc(entries, func(entry os.DirEntry) bool { return entry.Name() == filepath.Base(published) })
	entry := entries[index]
	later := time.Now().Add(time.Hour)
	if removed, removeErr := removeStale(temporary, entry, later); removed || removeErr != nil {
		t.Fatalf("a shared name that went from the directory: %v %v", removed, removeErr)
	}
	purged, err := blobs.PurgeTemporary(t.Context(), later)
	if err != nil || purged != 1 {
		t.Fatalf("purge: %d %v", purged, err)
	}
	if content, readErr := os.ReadFile(published); string(content) != "published content" || readErr != nil {
		t.Fatalf("the unlink reached the published blob: %q %v", content, readErr)
	}
	if err = os.Remove(published); err != nil {
		t.Fatal(err)
	}
	if removed, removeErr := removeStale(root, entry, later); removed || removeErr != nil {
		t.Fatalf("an entry that went since the listing: %v %v", removed, removeErr)
	}
	if err = restampFile(published, entry); err != nil {
		t.Fatalf("a restamp of an entry that went: %v", err)
	}
	if blob, listed, listErr := listedBlob(entry); listed || listErr != nil {
		t.Fatalf("a listing of an entry that went: %+v %v", blob, listErr)
	}
}

func TestADirectoryTheProcessCannotUseFailsItsStep(t *testing.T) {
	t.Parallel()
	requireUnprivileged(t)
	for name, testCase := range map[string]struct {
		mode  os.FileMode
		empty bool
		step  func(ctx context.Context, blobs *Blobs) error
	}{
		"a pool directory it cannot list": {0o300, false, func(ctx context.Context, blobs *Blobs) error {
			_, err := blobs.PurgeTemporary(ctx, time.Now())
			return errors.Join(err, blobs.Replenish(ctx), blobs.fillEmptyPool())
		}},
		"a pool directory it cannot write": {0o500, false, func(ctx context.Context, blobs *Blobs) error {
			return blobs.Replenish(ctx)
		}},
		"an empty pool directory it cannot write": {0o500, true, func(_ context.Context, blobs *Blobs) error {
			return blobs.fillEmptyPool()
		}},
	} {
		blobs := newInternal(t)
		root := blobs.root
		if testCase.empty {
			removePool(t, root)
		}
		chmod(t, filepath.Join(root, temporaryDirectory), testCase.mode)
		if err := testCase.step(t.Context(), blobs); !errors.Is(err, fs.ErrPermission) {
			t.Fatalf("%s: %v", name, err)
		}
		if len(blobs.poolNames) != 0 {
			t.Fatalf("%s: the pool kept names it may no longer have: %v", name, blobs.poolNames)
		}
	}
}

func TestAnEmptyPoolThatCannotBeFilledIsReported(t *testing.T) {
	t.Parallel()
	blobs := newInternal(t)
	root := blobs.root
	if err := os.RemoveAll(filepath.Join(root, temporaryDirectory)); err != nil {
		t.Fatal(err)
	}
	blobs.knowPool(nil)
	if _, err := blobs.takeRandom(); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a claim without a pool directory: %v", err)
	}
	reported := make(chan error, 1)
	blobs.OnPoolFailure(func(err error) { reported <- err })
	blobs.refilling.Store(true)
	blobs.refillInBackground()
	blobs.refilling.Store(false)
	blobs.refillInBackground()
	select {
	case err := <-reported:
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("the refill reported %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the failed refill was not reported")
	}
	if len(reported) != 0 {
		t.Fatal("a refill started while another ran")
	}
}

func TestACancelledRequestReadsAndWritesNothing(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "blob")
	write(t, path, "content")
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gone, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = (&blobReader{file: file, ctx: gone}).Read(make([]byte, 1)); !errors.Is(err, context.Canceled) {
		t.Fatalf("read: %v", err)
	}
	if _, err = slotWriter(gone, file).Write([]byte("x")); !errors.Is(err, context.Canceled) {
		t.Fatalf("write: %v", err)
	}
	if content, _ := os.ReadFile(path); string(content) != "content" {
		t.Fatalf("a cancelled write changed the file: %q", content)
	}
}

func TestStoringAndListingRefuseWhatTheyCannotUse(t *testing.T) {
	t.Parallel()
	blobs := newInternal(t)
	root := blobs.root
	long := strings.Repeat("r", maxReferenceLen+1)
	if _, err := blobs.PutExpiring(
		t.Context(),
		long,
		strings.NewReader(""),
		0,
		time.Now(),
	); !errors.Is(
		err,
		ErrReference,
	) {
		t.Fatalf("put: %v", err)
	}
	if _, _, err := blobs.Open(t.Context(), ""); !errors.Is(err, ErrReference) {
		t.Fatalf("open: %v", err)
	}
	if err := blobs.Delete(t.Context(), long); !errors.Is(err, ErrReference) {
		t.Fatalf("delete: %v", err)
	}
	write(t, filepath.Join(root, "not-hex"), "")
	if listed, err := blobs.List(t.Context()); err != nil || len(listed) != 0 {
		t.Fatalf("a name no reference encodes was listed: %v %v", listed, err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if _, err := blobs.List(t.Context()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("list without the directory: %v", err)
	}
	if err := blobs.Restamp(t.Context(), time.Now()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("restamp without the directory: %v", err)
	}
}

func TestEntriesThatCannotBeReadFailTheirStep(t *testing.T) {
	t.Parallel()
	requireUnprivileged(t)
	blobs := newInternal(t)
	root := blobs.root
	write(t, filepath.Join(root, "6162"), "blob")
	write(t, filepath.Join(root, temporaryDirectory, uploadPrefix+"stale"), "stale")
	chmod(t, filepath.Join(root, temporaryDirectory), 0o400)
	if _, err := blobs.PurgeTemporary(t.Context(), time.Now().Add(time.Hour)); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("purge: %v", err)
	}
	chmod(t, root, 0o400)
	if _, err := blobs.List(t.Context()); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("list: %v", err)
	}
	if err := blobs.Restamp(t.Context(), time.Now()); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("restamp: %v", err)
	}
}

func TestOpeningABlobTheProcessCannotReadFails(t *testing.T) {
	t.Parallel()
	requireUnprivileged(t)
	blobs := newInternal(t)
	root := blobs.root
	path := filepath.Join(root, "6162")
	write(t, path, "blob")
	chmod(t, path, 0)
	if _, _, err := blobs.Open(t.Context(), "ab"); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("open: %v", err)
	}
}

func TestEraseAllStopsAtWhatItCannotErase(t *testing.T) {
	t.Parallel()
	if err := EraseAll(t.TempDir()); err != nil {
		t.Fatalf("a data directory without blobs: %v", err)
	}
	requireUnprivileged(t)
	directory := t.TempDir()
	_, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(directory, Directory)
	chmod(t, filepath.Join(root, temporaryDirectory), 0o300)
	if err = EraseAll(directory); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("a directory it cannot list: %v", err)
	}
	chmod(t, filepath.Join(root, temporaryDirectory), 0o700)
	path := filepath.Join(root, "6162")
	write(t, path, "blob")
	chmod(t, path, 0o400)
	if err = EraseAll(directory); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("a blob it cannot overwrite: %v", err)
	}
}

func TestNewRefusesADirectoryItCannotUse(t *testing.T) {
	t.Parallel()
	file := filepath.Join(t.TempDir(), "file")
	write(t, file, "")
	if _, err := New(file); !errors.Is(err, syscall.ENOTDIR) {
		t.Fatalf("a data directory that is a file: %v", err)
	}
	requireUnprivileged(t)
	for _, mode := range []os.FileMode{0o500, 0o300} {
		directory := t.TempDir()
		temporary := filepath.Join(directory, Directory, temporaryDirectory)
		if err := os.MkdirAll(temporary, 0o700); err != nil {
			t.Fatal(err)
		}
		chmod(t, temporary, mode)
		if _, err := New(directory); !errors.Is(err, fs.ErrPermission) {
			t.Fatalf("a pool directory with mode %o: %v", mode, err)
		}
	}
}

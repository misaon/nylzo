package disk_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/iolimit"
	"github.com/misaon/nylzo/internal/store"
	"github.com/misaon/nylzo/internal/store/disk"
)

func hexName(reference string) string {
	return hex.EncodeToString([]byte(reference))
}

func poolFiles(t *testing.T, directory string) []os.FileInfo {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(directory, disk.Directory, "tmp"))
	if err != nil {
		t.Fatal(err)
	}
	var files []os.FileInfo
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "pool-") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			t.Fatal(infoErr)
		}
		files = append(files, info)
	}
	return files
}

func blobFile(t *testing.T, directory, reference string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(filepath.Join(directory, disk.Directory, hexName(reference)))
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func among(file os.FileInfo, files []os.FileInfo) bool {
	return slices.ContainsFunc(files, func(other os.FileInfo) bool { return os.SameFile(file, other) })
}

func TestUploadsWriteIntoFilesThePoolCreatedBefore(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	blobs := newBlobs(t, directory)
	before := poolFiles(t, directory)
	references := []string{"first", "second", "third"}
	for _, reference := range references {
		if _, err := blobs.PutExpiring(
			t.Context(),
			reference,
			strings.NewReader(reference),
			int64(len(reference)),
			time.Hour,
		); err != nil {
			t.Fatal(err)
		}
	}
	for _, reference := range references {
		if !among(blobFile(t, directory, reference), before) {
			t.Fatalf("%s was written into a file created for its upload, whose birth time dates the upload", reference)
		}
	}
	if left := poolFiles(t, directory); len(left) != len(before)-len(references) {
		t.Fatalf("each upload must take one pool file: %d of %d left", len(left), len(before))
	}
}

func TestAFailedUploadGivesItsFileBackToThePool(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	blobs := newBlobs(t, directory)
	before := poolFiles(t, directory)
	if _, err := blobs.PutExpiring(t.Context(), "short", strings.NewReader("abc"), 10, time.Hour); err == nil {
		t.Fatal("an upload shorter than its size must fail")
	}
	after := poolFiles(t, directory)
	if len(after) != len(before) {
		t.Fatalf("a failed upload took a pool file for good: %d of %d left", len(after), len(before))
	}
	for _, file := range after {
		original := before[slices.IndexFunc(before, func(other os.FileInfo) bool { return os.SameFile(file, other) })]
		if file.Size() != 0 || !file.ModTime().Equal(original.ModTime()) {
			t.Fatalf("a returned pool file must be empty with its pool time: %d bytes, %v", file.Size(), file.ModTime())
		}
	}
}

func TestAnEmptyPoolRefusesTheUploadAndRefills(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	blobs, err := disk.New(directory, disk.WithPool(disk.Pool{Size: 2, Batch: 1}))
	if err != nil {
		t.Fatal(err)
	}
	for _, reference := range []string{"first", "second"} {
		if _, err = blobs.PutExpiring(t.Context(), reference, strings.NewReader("x"), 1, time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = blobs.PutExpiring(t.Context(), "third", strings.NewReader("x"), 1, time.Hour); !errors.Is(
		err,
		store.ErrPoolEmpty,
	) {
		t.Fatalf("an upload that finds no pool file must be refused, not given a new file: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(directory, disk.Directory, hexName("third"))); !os.IsNotExist(statErr) {
		t.Fatalf("the refused upload stored a blob: %v", statErr)
	}
	refilled := poolFiles(t, directory)
	for deadline := time.Now().Add(5 * time.Second); len(refilled) != 2 && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
		refilled = poolFiles(t, directory)
	}
	if len(refilled) != 2 {
		t.Fatalf("the refusal must start a refill of the whole pool: %d files", len(refilled))
	}
	if _, err = blobs.PutExpiring(t.Context(), "third", strings.NewReader("x"), 1, time.Hour); err != nil ||
		!among(blobFile(t, directory, "third"), refilled) {
		t.Fatalf("the retry must take a refilled file: %v", err)
	}
}

// TestEverySweepCreatesTheSameNumberOfPoolFiles covers that the files a sweep creates do not count the uploads.
func TestEverySweepCreatesTheSameNumberOfPoolFiles(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	pool := disk.Pool{Size: 10, Batch: 3}
	blobs, err := disk.New(directory, disk.WithPool(pool))
	if err != nil {
		t.Fatal(err)
	}
	for round, uploads := range []int{5, 0, 4} {
		for i := range uploads {
			reference := fmt.Sprintf("round-%d-%d", round, i)
			if _, err = blobs.PutExpiring(t.Context(), reference, strings.NewReader("x"), 1, time.Hour); err != nil {
				t.Fatal(err)
			}
		}
		before := poolFiles(t, directory)
		if err = blobs.Replenish(t.Context()); err != nil {
			t.Fatal(err)
		}
		after := poolFiles(t, directory)
		created := 0
		for _, file := range after {
			if !among(file, before) {
				created++
			}
		}
		if len(after) != min(pool.Size, len(before)+pool.Batch) ||
			(len(before)+pool.Batch <= pool.Size && created != pool.Batch) {
			t.Fatalf("after %d uploads a sweep created %d files and kept %d", uploads, created, len(after))
		}
	}
}

// gatedReader blocks its first read until the test releases it, after the upload has claimed its file.
type gatedReader struct {
	entered chan<- struct{}
	release <-chan struct{}
	content io.Reader
	opened  bool
}

func (g *gatedReader) Read(buffer []byte) (int, error) {
	if !g.opened {
		g.opened = true
		close(g.entered)
		<-g.release
	}
	return g.content.Read(buffer)
}

func TestASweepDuringAnUploadLeavesItsFileAlone(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	blobs := newBlobs(t, directory)
	ageAll(t, filepath.Join(directory, disk.Directory, "tmp"), time.Hour)
	content := bytes.Repeat([]byte{0x5a}, 3*4096)
	entered, release := make(chan struct{}), make(chan struct{})
	reader := &gatedReader{entered: entered, release: release, content: bytes.NewReader(content)}
	uploaded := make(chan error, 1)
	go func() {
		_, err := blobs.PutExpiring(t.Context(), "uploading", reader, int64(len(content)), time.Hour)
		uploaded <- err
	}()
	<-entered
	sweeper, err := disk.New(directory, disk.WithPool(disk.Pool{}))
	if err != nil {
		t.Fatal(err)
	}
	if err = sweeper.Replenish(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = sweeper.PurgeTemporary(t.Context(), time.Now().Add(-store.BlobGrace)); err != nil {
		t.Fatal(err)
	}
	if left := poolFiles(t, directory); len(left) != 0 {
		t.Fatalf("a sweep with no pool must remove every pool file: %d left", len(left))
	}
	close(release)
	if err = <-uploaded; err != nil {
		t.Fatalf("the sweep took the file of an upload in progress: %v", err)
	}
	stored, _, err := blobs.Open(t.Context(), "uploading")
	if err != nil {
		t.Fatal(err)
	}
	defer stored.Close()
	if read, readErr := io.ReadAll(stored); readErr != nil || !bytes.Equal(read, content) {
		t.Fatalf("the upload lost content to the sweep: %v", readErr)
	}
}

// TestPutWaitsForAFreeFileSlotNoLongerThanItsRequest releases the slots after a while, so a Put that does not wait
// fails the test instead of hanging in its cleanup.
func TestPutWaitsForAFreeFileSlotNoLongerThanItsRequest(t *testing.T) {
	directory := t.TempDir()
	blobs := newBlobs(t, directory)
	for range iolimit.Slots {
		iolimit.Hold()
	}
	waiting, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	put := make(chan error, 1)
	go func() {
		_, err := blobs.PutExpiring(waiting, "waiting", strings.NewReader("x"), 1, time.Hour)
		put <- err
	}()
	var err error
	select {
	case err = <-put:
	case <-time.After(5 * time.Second):
	}
	for range iolimit.Slots {
		iolimit.Release()
	}
	if err == nil {
		err = <-put
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a Put must wait for a file slot and give up with its request: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(directory, disk.Directory, hexName("waiting"))); !os.IsNotExist(statErr) {
		t.Fatalf("the abandoned upload stored a blob: %v", statErr)
	}
}

type cancelAfterLink struct {
	context.Context

	final string
}

func (c cancelAfterLink) Err() error {
	if _, err := os.Stat(c.final); err == nil {
		return context.Canceled
	}
	return nil
}

func TestABlobWhoseDirectorySyncFailsIsShredded(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	blobs := newBlobs(t, directory)
	final := filepath.Join(directory, disk.Directory, hexName("unsynced"))
	ctx := cancelAfterLink{Context: context.Background(), final: final}
	if _, err := blobs.PutExpiring(ctx, "unsynced", strings.NewReader("x"), 1, time.Hour); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("a publish whose directory sync failed must fail: %v", err)
	}
	if _, err := os.Stat(final); !os.IsNotExist(err) {
		t.Fatalf("the blob survived its failed directory sync: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(directory, disk.Directory, "tmp"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "blob-") {
			t.Fatalf("the upload name %s stayed", entry.Name())
		}
	}
}

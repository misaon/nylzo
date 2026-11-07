package storetest

import (
	"errors"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/misaon/nylzo/internal/core"
	"github.com/misaon/nylzo/internal/store"
)

func (f *fixture) blobsPutOpenDelete() {
	t, ctx := f.t, f.t.Context()
	if err := f.blobs.Delete(ctx, core.NewID().String()); err != nil {
		t.Fatalf("deleting a missing object must succeed: %v", err)
	}
	if err := f.put("short", "short", 10); err == nil || f.blobExists("short") {
		t.Fatalf("a short body must leave no object: %v", err)
	}
	if err := f.put("y", "hello", 5); err != nil {
		t.Fatal(err)
	}
	if err := f.put("y", "other", 5); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("a second Put of an existing ref must not replace it: %v", err)
	}
	if content := f.read("y", 5); content != "hello" {
		t.Fatalf("content %q", content)
	}
	upper, lower := "AbCdEfGhIjKlMnOpQrStUw", "abcdefghijklmnopqrstuw"
	for _, reference := range []string{upper, lower, "../escape"} {
		if err := f.put(reference, reference[:1], 1); err != nil {
			t.Fatalf("ref %q: %v", reference, err)
		}
	}
	if err := f.blobs.Delete(ctx, lower); err != nil {
		t.Fatal(err)
	}
	if content := f.read(upper, 1); content != "A" {
		t.Fatalf("refs that differ in case share storage: %q", content)
	}
	listed, err := f.blobs.List(ctx)
	references := make([]string, 0, len(listed))
	for _, info := range listed {
		references = append(references, info.Reference)
	}
	slices.Sort(references)
	if err != nil || !slices.Equal(references, []string{"../escape", upper, "y"}) {
		t.Fatalf("list: %v %v", references, err)
	}
}

func (f *fixture) put(reference, content string, size int64) error {
	_, err := f.blobs.PutExpiring(f.t.Context(), reference, strings.NewReader(content), size, time.Hour)
	return err
}

func (f *fixture) read(reference string, size int64) string {
	f.t.Helper()
	content, length, err := f.blobs.Open(f.t.Context(), reference)
	if err != nil || length != size {
		f.t.Fatalf("open %q: %d %v", reference, length, err)
	}
	defer content.Close()
	data, err := io.ReadAll(content)
	if err != nil {
		f.t.Fatal(err)
	}
	return string(data)
}

func (f *fixture) blobsDeleteBatch() {
	t, ctx := f.t, f.t.Context()
	first, second, kept := core.NewID().String(), core.NewID().String(), core.NewID().String()
	for _, reference := range []string{first, second, kept} {
		if err := f.put(reference, "x", 1); err != nil {
			t.Fatal(err)
		}
	}
	errs := f.blobs.DeleteBatch(ctx, []string{first, core.NewID().String(), "", second})
	if len(errs) != 4 || errs[0] != nil || errs[1] != nil || errs[2] == nil || errs[3] != nil {
		t.Fatalf("one error per ref, a missing object counts as deleted and an invalid ref fails alone: %v", errs)
	}
	if f.blobExists(first) || f.blobExists(second) || !f.blobExists(kept) {
		t.Fatal("the batch deleted the wrong objects")
	}
	if errs = f.blobs.DeleteBatch(ctx, nil); len(errs) != 0 {
		t.Fatalf("an empty batch: %v", errs)
	}
}

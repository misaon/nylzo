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
	ctx := f.t.Context()
	err := f.blobs.Delete(ctx, core.NewID().String())
	f.fatalIf(err != nil, "deleting a missing object must succeed: %v", err)
	err = f.put("short", "short", 10)
	f.fatalIf(err == nil || f.blobExists("short"), "a short body must leave no object: %v", err)
	f.must(f.put("y", "hello", 5))
	err = f.put("y", "other", 5)
	f.fatalIf(!errors.Is(err, store.ErrConflict), "a second Put of an existing ref must not replace it: %v", err)
	content := f.read("y", 5)
	f.fatalIf(content != "hello", "content %q", content)
	upper, lower := "AbCdEfGhIjKlMnOpQrStUw", "abcdefghijklmnopqrstuw"
	for _, reference := range []string{upper, lower, "../escape"} {
		err = f.put(reference, reference[:1], 1)
		f.fatalIf(err != nil, "ref %q: %v", reference, err)
	}
	f.must(f.blobs.Delete(ctx, lower))
	content = f.read(upper, 1)
	f.fatalIf(content != "A", "refs that differ in case share storage: %q", content)
	listed, err := f.blobs.List(ctx)
	references := make([]string, 0, len(listed))
	for _, info := range listed {
		references = append(references, info.Reference)
	}
	slices.Sort(references)
	f.fatalIf(
		err != nil || !slices.Equal(references, []string{"../escape", upper, "y"}),
		"list: %v %v",
		references,
		err,
	)
}

func (f *fixture) put(reference, content string, size int64) error {
	_, err := f.blobs.PutExpiring(f.t.Context(), reference, strings.NewReader(content), size, time.Now().Add(time.Hour))
	return err
}

func (f *fixture) read(reference string, size int64) string {
	f.t.Helper()
	content, length, err := f.blobs.Open(f.t.Context(), reference)
	f.fatalIf(err != nil || length != size, "open %q: %d %v", reference, length, err)
	defer content.Close()
	data, err := io.ReadAll(content)
	f.must(err)
	return string(data)
}

func (f *fixture) blobsDeleteBatch() {
	ctx := f.t.Context()
	first, second, kept := core.NewID().String(), core.NewID().String(), core.NewID().String()
	for _, reference := range []string{first, second, kept} {
		f.must(f.put(reference, "x", 1))
	}
	errs := f.blobs.DeleteBatch(ctx, []string{first, core.NewID().String(), "", second})
	f.fatalIf(len(errs) != 4 || errs[0] != nil || errs[1] != nil || errs[2] == nil || errs[3] != nil,
		"one error per ref, a missing object counts as deleted and an invalid ref fails alone: %v", errs)
	f.fatalIf(f.blobExists(first) || f.blobExists(second) || !f.blobExists(kept), "the batch deleted the wrong objects")
	errs = f.blobs.DeleteBatch(ctx, nil)
	f.fatalIf(len(errs) != 0, "an empty batch: %v", errs)
}

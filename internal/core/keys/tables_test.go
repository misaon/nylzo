package keys

import (
	"testing"
	"testing/fstest"
)

func expectPanic(t *testing.T, name string, run func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: the program went on", name)
		}
	}()
	run()
}

func TestADamagedEmbeddedTableStopsTheProgram(t *testing.T) {
	t.Parallel()
	expectPanic(t, "a code point that is not hexadecimal", func() { parseAssignedRanges("0041 zz\n") })
	expectPanic(t, "a missing word list", func() { loadWordLists(fstest.MapFS{}) })
	expectPanic(t, "a word list that is too short", func() {
		loadWordLists(fstest.MapFS{"words/en.txt": {Data: []byte("one\ntwo\n")}})
	})
}

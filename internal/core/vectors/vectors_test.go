package vectors

import (
	"bytes"
	"os"
	"testing"
)

func TestCommittedVectorsMatchGenerator(t *testing.T) {
	want, err := os.ReadFile("../../../spec/vectors.json")
	if err != nil {
		t.Fatalf("spec/vectors.json missing; run `%s`: %v", Generator, err)
	}
	f, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf(
			"spec/vectors.json is stale; regenerate with `%s` and update web/core tests if the protocol changed",
			Generator,
		)
	}
}

package vectors

import (
	"bytes"
	"errors"
	"os"
	"testing"
)

func TestCommittedVectorsMatchGenerator(t *testing.T) {
	want, err := os.ReadFile("../../spec/vectors.json")
	if err != nil {
		t.Fatalf("spec/vectors.json missing; run `%s`: %v", Generator, err)
	}
	if !bytes.Equal(Marshal(Generate()), want) {
		t.Fatalf(
			"spec/vectors.json is stale; regenerate with `%s` and update web/core tests if the protocol changed",
			Generator,
		)
	}
}

func TestTheGeneratorStopsAtACaseThatDisagreesWithTheCore(t *testing.T) {
	errDisagrees := errors.New("disagrees")
	for name, generate := range map[string]func(){
		"an error":                            func() { must(0, errDisagrees) },
		"a broken agreement":                  func() { require(false, errDisagrees) },
		"a free block outside the ciphertext": func() { exponent(-1) },
	} {
		func() {
			defer func() {
				if stopped, _ := recover().(error); !errors.Is(stopped, errDisagrees) &&
					!errors.Is(stopped, errFreeBlockOutside) {
					t.Errorf("%s: recovered %v", name, stopped)
				}
			}()
			generate()
		}()
	}
}

func TestASingularGHASHSystemHasNoSolution(t *testing.T) {
	if _, err := solve([2][2]fieldElement{}, [2]fieldElement{}); !errors.Is(err, errSingular) {
		t.Fatalf("got %v, want errSingular", err)
	}
}

func TestARejectedCaseNamesAnAcceptedObjectAndAnUnknownError(t *testing.T) {
	errUnknown := errors.New("unknown")
	if accepted, unknown := errorName(nil), errorName(errUnknown); accepted != "accepted" || unknown != "unknown" {
		t.Fatalf("named %q and %q", accepted, unknown)
	}
}

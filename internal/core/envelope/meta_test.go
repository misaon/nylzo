package envelope_test

import (
	"bytes"
	"crypto/fips140"
	"errors"
	"flag"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/misaon/nylzo/internal/core/envelope"
)

const fipsOnlyChild = "NYLZO_TEST_ENVELOPE_FIPS_ONLY"

func TestDecodeMetaRefusesFieldsOfTheWrongType(t *testing.T) {
	t.Parallel()
	for _, meta := range []string{
		`{"v":1,"text":5}`,
		`{"v":1,"file":5}`,
		`{"v":1,"file":{"name":"a","size":1,"mime":5}}`,
	} {
		if _, err := envelope.DecodeMeta([]byte(meta)); !errors.Is(err, envelope.ErrMeta) {
			t.Errorf("%s: want ErrMeta, got %v", meta, err)
		}
	}
}

func TestEncodeMetaRefusesATextWhoseEscapesExceedTheLimit(t *testing.T) {
	t.Parallel()
	quotes := strings.Repeat(`"`, envelope.MaxTextBytes)
	if _, err := envelope.EncodeMeta(envelope.Meta{Text: &quotes}); !errors.Is(err, envelope.ErrMetaTooLarge) ||
		!errors.Is(err, envelope.ErrMeta) {
		t.Fatalf("a text that escapes to twice its length: want ErrMeta, got %v", err)
	}
}

func TestCompressionKeepsTheOriginalWhenTheCompressedMetaWouldExceedTheLimit(t *testing.T) {
	t.Parallel()
	file := bytes.Repeat([]byte{'A'}, 256*1024)
	quotes := strings.Repeat(`"`, 50_000)
	meta := envelope.Meta{Text: &quotes, File: &envelope.FileMeta{Name: "f", Size: int64(len(file))}}
	encoded, err := envelope.EncodeMeta(meta)
	if err != nil {
		t.Fatal(err)
	}
	text := quotes + strings.Repeat("a", envelope.MaxMetaBytes-10-len(encoded))
	meta.Text = &text
	sample := seal(t, "", nil)
	payload := envelope.Payload{Meta: meta, File: file}
	plain, err := envelope.Seal(&sample.content, sample.header, sample.id, sample.policy, payload, false)
	if err != nil {
		t.Fatal(err)
	}
	asked, err := envelope.Seal(&sample.content, sample.header, sample.id, sample.policy, payload, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(asked) != len(plain) {
		t.Fatalf(
			"compressed to %d bytes although the compressed meta exceeds the limit; plain is %d",
			len(asked),
			len(plain),
		)
	}
	if _, opened, openErr := sample.open(asked); openErr != nil || !bytes.Equal(opened.File, file) {
		t.Fatalf("the uncompressed object does not open: %v", openErr)
	}
}

// FIPS 140-only mode forbids GCM with a caller's IV, so the envelope neither seals nor opens there.
func TestTheEnvelopeRefusesFIPS140OnlyMode(t *testing.T) {
	if os.Getenv(fipsOnlyChild) == "1" {
		var sample sealed
		fips140.WithoutEnforcement(func() { sample = seal(t, "fips", nil) })
		if object, err := envelope.Seal(
			&sample.content, sample.header, sample.id, sample.policy, sample.payload, false,
		); err == nil || object != nil {
			t.Fatal("sealed in FIPS 140-only mode")
		}
		if _, _, err := sample.open(sample.object); err == nil || errors.Is(err, envelope.ErrIntegrity) {
			t.Fatalf("opened in FIPS 140-only mode or blamed the object: %v", err)
		}
		os.Exit(0)
	}
	t.Parallel()
	child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestTheEnvelopeRefusesFIPS140OnlyMode$")
	child.Env = append(os.Environ(), fipsOnlyChild+"=1", "GODEBUG=fips140=only")
	if directory := flag.Lookup("test.gocoverdir"); directory != nil && directory.Value.String() != "" {
		child.Env = append(child.Env, "GOCOVERDIR="+directory.Value.String())
	}
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, output)
	}
}

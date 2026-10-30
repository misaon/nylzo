package core_test

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/misaon/nylzo/internal/core"
)

func TestAttachmentCompression(t *testing.T) {
	t.Parallel()
	for _, size := range []int{128 * 1024, core.MaxFileBytes} {
		file := bytes.Repeat([]byte{'A'}, size)
		sample := seal(t, "this text never enters the compressor", file)
		object, err := core.SealWithCompression(&sample.key, sample.header, sample.id, sample.policy, sample.payload)
		if err != nil {
			t.Fatal(err)
		}
		if len(object) != core.PadFile || !core.MayHoldFile(int64(len(object))) {
			t.Fatalf("want one padded attachment block, got %d", len(object))
		}
		_, payload, err := core.Open(&sample.key, sample.id, sample.policy, core.KDFNone, object)
		if err != nil || !bytes.Equal(payload.File, file) || *payload.Meta.Text != *sample.payload.Meta.Text ||
			*payload.Meta.File != *sample.payload.Meta.File {
			t.Fatalf("compressed round trip: %v", err)
		}
		plaintext, err := sample.aead(t).Open(nil, sample.header.IV[:], object[core.HeaderLen:],
			core.AAD(sample.header, sample.id, sample.policy))
		if err != nil || !bytes.Contains(plaintext, []byte(`"encoding":"gzip"`)) ||
			!bytes.Contains(plaintext, []byte(*sample.payload.Meta.Text)) {
			t.Fatalf("compression must leave metadata separate: %v", err)
		}
		_, _, err = core.Open(&sample.key, sample.id, sample.policy, core.KDFNone, flipped(object, core.HeaderLen+100))
		if !errors.Is(err, core.ErrIntegrity) {
			t.Fatalf("authenticate before decompressing: %v", err)
		}
	}
}

func TestCompressionKeepsLegacyEncodingWhenItCannotSaveSpace(t *testing.T) {
	t.Parallel()
	random := make([]byte, 256*1024)
	rand.Read(random)
	for _, file := range [][]byte{nil, {}, bytes.Repeat([]byte{'A'}, 100*1024), random} {
		sample := seal(t, "text", file)
		object, err := core.SealWithCompression(&sample.key, sample.header, sample.id, sample.policy, sample.payload)
		if err != nil || !bytes.Equal(object, sample.object) {
			t.Fatalf("unchanged legacy encoding for %d bytes: %v", len(file), err)
		}
	}
}

func TestCompressionRequiresTenPercentPaddedSavings(t *testing.T) {
	t.Parallel()
	file := make([]byte, 1024*1024)
	rand.Read(file[32*1024:])
	sample := seal(t, "text", file)
	object, err := core.SealWithCompression(&sample.key, sample.header, sample.id, sample.policy, sample.payload)
	if err != nil || !bytes.Equal(object, sample.object) {
		t.Fatalf("a single block saved on a large file is insufficient: %v", err)
	}
}

func gzipFile(tb testing.TB, file []byte) []byte {
	tb.Helper()
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(file); err != nil {
		tb.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		tb.Fatal(err)
	}
	return buffer.Bytes()
}

func compressedObject(tb testing.TB, sample *sealed, meta string, file []byte) []byte {
	tb.Helper()
	plaintext := binary.BigEndian.AppendUint32(nil, uint32(len(meta)))
	plaintext = append(plaintext, []byte(meta)...)
	plaintext = append(plaintext, file...)
	return sample.sealPlaintext(sample.aead(tb), plaintext)
}

func FuzzCompressedAttachment(f *testing.F) {
	sample := seal(f, "", nil)
	file := bytes.Repeat([]byte{'A'}, 128*1024)
	f.Add(gzipFile(f, file), uint32(len(file)))
	f.Add([]byte{0x1f, 0x8b}, uint32(1024))
	f.Fuzz(func(t *testing.T, compressed []byte, size uint32) {
		if len(compressed) > core.MaxFileBytes {
			t.Skip()
		}
		meta, _ := json.Marshal(map[string]any{"v": 2, "file": map[string]any{
			"name": "f", "size": size, "encoding": "gzip", "encoded_size": len(compressed),
		}})
		object := compressedObject(t, &sample, string(meta), compressed)
		_, payload, err := core.Open(&sample.key, sample.id, sample.policy, core.KDFNone, object)
		if err == nil && (len(payload.File) > core.MaxFileBytes || uint64(len(payload.File)) != uint64(size)) {
			t.Fatal("accepted decompression beyond the declared or protocol limit")
		}
	})
}

func TestCompressionInteroperabilityVectors(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../spec/compression-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors struct {
		Pattern     string `json:"pattern"`
		Repetitions int    `json:"repetitions"`
		Frames      []struct {
			Name string `json:"name"`
			Hex  string `json:"gzip_hex"`
		} `json:"frames"`
	}
	if err = json.Unmarshal(data, &vectors); err != nil {
		t.Fatal(err)
	}
	file := bytes.Repeat([]byte(vectors.Pattern), vectors.Repetitions)
	sample := seal(t, "separate text", file)
	for _, frame := range vectors.Frames {
		t.Run(frame.Name, func(t *testing.T) {
			t.Parallel()
			compressed := decodeHex(t, frame.Hex)
			meta, _ := json.Marshal(map[string]any{"v": 2, "file": map[string]any{
				"name": "rows.csv", "size": len(file), "encoding": "gzip", "encoded_size": len(compressed),
			}})
			object := compressedObject(t, &sample, string(meta), compressed)
			_, opened, openErr := core.Open(&sample.key, sample.id, sample.policy, core.KDFNone, object)
			if openErr != nil || !bytes.Equal(opened.File, file) {
				t.Fatalf("cross-encoder frame: %v", openErr)
			}
		})
	}
}

func TestMalformedCompressedAttachments(t *testing.T) {
	t.Parallel()
	file := bytes.Repeat([]byte{'A'}, 256*1024)
	compressed := gzipFile(t, file)
	badChecksum := bytes.Clone(compressed)
	badChecksum[len(badChecksum)-8] ^= 1
	sample := seal(t, "", nil)
	for name, test := range map[string]struct {
		file     []byte
		size     int
		encoding string
	}{
		"checksum":        {badChecksum, len(file), "gzip"},
		"truncated":       {compressed[:len(compressed)-1], len(file), "gzip"},
		"trailing bytes":  {append(bytes.Clone(compressed), 1), len(file), "gzip"},
		"multiple frames": {append(bytes.Clone(compressed), compressed...), len(file) * 2, "gzip"},
		"expansion bomb":  {compressed, 1024, "gzip"},
		"short output":    {compressed, len(file) + 1, "gzip"},
		"unknown codec":   {compressed, len(file), "zstd"},
		"over limit":      {compressed, core.MaxFileBytes + 1, "gzip"},
		"negative size":   {compressed, -1, "gzip"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			meta, _ := json.Marshal(map[string]any{"v": 2, "file": map[string]any{
				"name": "f", "size": test.size, "encoding": test.encoding, "encoded_size": len(test.file),
			}})
			object := compressedObject(t, &sample, string(meta), test.file)
			_, _, err := core.Open(&sample.key, sample.id, sample.policy, core.KDFNone, object)
			if err == nil {
				t.Fatal("accepted a malformed compressed attachment")
			}
		})
	}
}

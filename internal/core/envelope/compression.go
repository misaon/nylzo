package envelope

import (
	"bytes"
	"compress/gzip"
	"io"
)

const (
	gzipHeaderLen     = 10
	gzipFlagsOffset   = 3
	gzipReservedFlags = 0xe0
)

type encodedAttachment struct {
	meta      []byte
	file      []byte
	objectLen int
}

// compressAttachment gives a fresh compression context to this file alone and reports objectLen 0 when it keeps the original.
func compressAttachment(meta Meta, metadata, file []byte) encodedAttachment {
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	_, writeErr := writer.Write(file)
	closeErr := writer.Close()
	compressed := buffer.Bytes()
	if writeErr != nil || closeErr != nil || len(compressed) >= len(file) {
		clear(compressed)
		return encodedAttachment{}
	}
	encodedMeta, err := encodeStoredMeta(meta, int64(len(compressed)))
	if err != nil {
		clear(compressed)
		return encodedAttachment{}
	}
	overhead := PrefixLen + MetaLenPrefix + TagLen
	original := padded(overhead + len(metadata) + len(file))
	needed := padded(overhead + len(encodedMeta) + len(compressed))
	objectLen := CompressedLength(original, needed)
	if objectLen == 0 {
		clear(compressed)
		clear(encodedMeta)
		return encodedAttachment{}
	}
	return encodedAttachment{meta: encodedMeta, file: compressed, objectLen: objectLen}
}

// CompressedLength is the smallest halving class of the original length that holds needed bytes, or 0 when
// none is shorter than the original (spec section 3.2), so the length shows only a factor of two.
func CompressedLength(original, needed int) int {
	chosen := 0
	for divisor := 2; ; divisor *= 2 {
		class := padded((original + divisor - 1) / divisor)
		if class < needed || class >= original {
			return chosen
		}
		chosen = class
		if class == Pad {
			return chosen
		}
	}
}

// Authenticate the envelope before calling this. The claimed size never grants more than the protocol limit.
func decompressAttachment(compressed []byte, size int64) ([]byte, error) {
	if size < 0 || size > MaxFileBytes {
		return nil, ErrPlaintext
	}
	if len(compressed) < gzipHeaderLen || compressed[gzipFlagsOffset]&gzipReservedFlags != 0 {
		return nil, ErrPlaintext
	}
	source := bytes.NewReader(compressed)
	reader, err := gzip.NewReader(source)
	if err != nil {
		return nil, ErrPlaintext
	}
	defer reader.Close()
	reader.Multistream(false)
	file, err := io.ReadAll(io.LimitReader(reader, size+1))
	if err != nil || int64(len(file)) != size || source.Len() != 0 {
		clear(file)
		return nil, ErrPlaintext
	}
	return file, nil
}

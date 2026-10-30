package core

import (
	"bytes"
	"compress/gzip"
	"io"
)

const minCompressBytes = 128 * 1024

type encodedAttachment struct {
	meta []byte
	file []byte
}

// Each attachment has a fresh compression context, separate from text and metadata.
func compressAttachment(meta Meta, metadata, file []byte) encodedAttachment {
	if len(file) < minCompressBytes {
		return encodedAttachment{metadata, file}
	}
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	_, writeErr := writer.Write(file)
	closeErr := writer.Close()
	compressed := buffer.Bytes()
	if writeErr != nil || closeErr != nil || len(compressed) >= len(file) {
		clear(compressed)
		return encodedAttachment{metadata, file}
	}
	encodedMeta, err := encodeStoredMeta(meta, int64(len(compressed)))
	originalSize := PaddedLen(HeaderLen+MetaLenPrefix+len(metadata)+len(file)+TagLen, true)
	encodedSize := PaddedLen(HeaderLen+MetaLenPrefix+len(encodedMeta)+len(compressed)+TagLen, true)
	if err != nil || originalSize-encodedSize < PadFile || encodedSize*10 > originalSize*9 {
		clear(compressed)
		clear(encodedMeta)
		return encodedAttachment{metadata, file}
	}
	return encodedAttachment{encodedMeta, compressed}
}

// Authenticate the envelope before calling this. The claimed size never grants more than the protocol limit.
func decompressAttachment(compressed []byte, size int64) ([]byte, error) {
	if size < 0 || size > MaxFileBytes {
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

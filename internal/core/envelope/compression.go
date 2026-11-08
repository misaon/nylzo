package envelope

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
)

const (
	gzipHeaderLen     = 10
	gzipFlagsOffset   = 3
	gzipReservedFlags = 0xe0
)

var errNotShorter = errors.New("envelope: the compressed file is not shorter")

// shorterBuffer refuses to grow past its capacity, so compression stops once it cannot help and no regrown copy of
// the compressed plaintext stays behind.
type shorterBuffer struct {
	data []byte
}

func (b *shorterBuffer) Write(chunk []byte) (int, error) {
	if len(chunk) > cap(b.data)-len(b.data) {
		return 0, errNotShorter
	}
	b.data = append(b.data, chunk...)
	return len(chunk), nil
}

type encodedAttachment struct {
	meta      []byte
	file      []byte
	objectLen int
}

// compressAttachment gives a fresh compression context to this file alone and reports objectLen 0 when it keeps the original.
func compressAttachment(meta Meta, metadata, file []byte) encodedAttachment {
	buffer := shorterBuffer{data: make([]byte, 0, max(0, len(file)-1))}
	writer := gzip.NewWriter(&buffer)
	_, writeErr := writer.Write(file)
	closeErr := writer.Close()
	compressed := buffer.data
	if writeErr != nil || closeErr != nil {
		clear(compressed[:cap(compressed)])
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
	file, ended := readToEnd(reader, size)
	if !ended || source.Len() != 0 {
		clear(file)
		return nil, ErrPlaintext
	}
	return file, nil
}

// readToEnd reads into one allocation of size+1 bytes and reports whether the stream ended cleanly after exactly size
// bytes; a truncated stream ends with io.ErrUnexpectedEOF instead, and a longer one fills the extra byte.
func readToEnd(reader io.Reader, size int64) ([]byte, bool) {
	file := make([]byte, size+1)
	read := 0
	var err error
	for err == nil && read < len(file) {
		var chunk int
		chunk, err = reader.Read(file[read:])
		read += chunk
	}
	return file[:read], errors.Is(err, io.EOF) && int64(read) == size
}

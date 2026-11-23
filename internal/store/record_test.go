package store

import (
	"bytes"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/misaon/nylzo/internal/seal"
)

const sizeOffset = 27

func TestARecordRefusesFieldsNoMessageHas(t *testing.T) {
	t.Parallel()
	valid := encodeMessage(newTestRecord().Message)
	for name, damage := range map[string]func(data []byte) []byte{
		"another format": func(data []byte) []byte { data[0]++; return data },
		"a cut record":   func(data []byte) []byte { return data[:len(data)-1] },
		"no opens":       func(data []byte) []byte { return encodeMessage(&Message{}) },
		"more opens left than given": func([]byte) []byte {
			return encodeMessage(&Message{OpensMax: 1, OpensRemaining: 2})
		},
	} {
		if _, err := decodeMessage(damage(bytes.Clone(valid))); !errors.Is(err, ErrCorrupt) {
			t.Errorf("%s: %v", name, err)
		}
	}
	record := newTestRecord()
	record.Row.Record = seal.Record(record.Key, record.Row.Locator, encodeMessage(&Message{}))
	if _, err := OpenRecord(&record.Row, record.Key); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("a sealed record without opens: %v", err)
	}
}

func TestAValueTheRecordCannotHoldBecomesItsNearestBound(t *testing.T) {
	t.Parallel()
	message := newTestRecord().Message
	message.CreatedAt = time.Unix(-1, 0)
	message.Size = -1
	message.FailCountTotal = math.MaxInt32
	message.Ending = "unknown"
	message.Leases = []Lease{{MAC: make([]byte, macLen), ExpiresAt: testMoment, Downloads: math.MaxInt32}}
	decoded, err := decodeMessage(encodeMessage(message))
	if err != nil {
		t.Fatal(err)
	}
	if !decoded.CreatedAt.IsZero() || decoded.Size != 0 || decoded.FailCountTotal != math.MaxUint16 ||
		decoded.Ending != "" || decoded.Leases[0].Downloads != math.MaxUint8 {
		t.Fatalf("out of range values: %+v", decoded)
	}
	data := encodeMessage(newTestRecord().Message)
	copy(data[sizeOffset:], bytes.Repeat([]byte{0xff}, uint64Bytes))
	if decoded, err = decodeMessage(data); err != nil || decoded.Size != math.MaxInt64 {
		t.Fatalf("a size past the largest int64: %v", err)
	}
}

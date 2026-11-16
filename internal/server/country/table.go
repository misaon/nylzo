package country

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

// countries.bin: "NZCC", version 1, then an IPv4 and an IPv6 section. A section is a uvarint count n, n uvarint
// differences between ascending range starts (the first from 0; IPv6 starts are the upper 64 bits of the address)
// and n two-byte country codes, 00 00 for unknown. A range lasts until the next start.
const (
	tableHeader = "NZCC\x01"
	codeLength  = len(Code{})
)

var errTable = errors.New("country: the embedded table is damaged")

type tableReader struct {
	data []byte
}

func (r *tableReader) uvarint() (uint64, error) {
	value, length := binary.Uvarint(r.data)
	if length <= 0 {
		return 0, errTable
	}
	r.data = r.data[length:]
	return value, nil
}

func decodeTable(compressed []byte) (*Table, error) {
	archive, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errTable, err)
	}
	data, err := io.ReadAll(archive)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errTable, err)
	}
	return parseTable(data)
}

func parseTable(data []byte) (*Table, error) {
	rest, ok := bytes.CutPrefix(data, []byte(tableHeader))
	if !ok {
		return nil, errTable
	}
	reader := &tableReader{data: rest}
	ipv4Starts, ipv4Countries, err := readSection[uint32](reader, math.MaxUint32)
	if err != nil {
		return nil, err
	}
	ipv6Starts, ipv6Countries, err := readSection[uint64](reader, math.MaxUint64)
	if err != nil {
		return nil, err
	}
	if len(reader.data) != 0 {
		return nil, errTable
	}
	return &Table{
		ipv4Starts:    ipv4Starts,
		ipv4Countries: ipv4Countries,
		ipv6Starts:    ipv6Starts,
		ipv6Countries: ipv6Countries,
	}, nil
}

func readSection[Start uint32 | uint64](reader *tableReader, largest uint64) ([]Start, []Code, error) {
	count, err := reader.uvarint()
	if err != nil {
		return nil, nil, err
	}
	if count == 0 || count > uint64(len(reader.data)/(codeLength+1)) {
		return nil, nil, errTable
	}
	starts := make([]Start, count)
	var start uint64
	for i := range starts {
		difference, differenceErr := reader.uvarint()
		if differenceErr != nil {
			return nil, nil, differenceErr
		}
		if (i > 0 && difference == 0) || difference > largest-start {
			return nil, nil, errTable
		}
		start += difference
		starts[i] = Start(start)
	}
	countries, err := readCountries(reader, len(starts))
	if err != nil {
		return nil, nil, err
	}
	return starts, countries, nil
}

func readCountries(reader *tableReader, count int) ([]Code, error) {
	if len(reader.data) < count*codeLength {
		return nil, errTable
	}
	countries := make([]Code, count)
	for i := range countries {
		country := Code(reader.data[i*codeLength:])
		if country.Known() && (!isUpper(country[0]) || !isUpper(country[1])) {
			return nil, errTable
		}
		countries[i] = country
	}
	reader.data = reader.data[count*codeLength:]
	return countries, nil
}

func isUpper(letter byte) bool {
	return letter >= 'A' && letter <= 'Z'
}

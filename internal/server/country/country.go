package country

import (
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/netip"
	"slices"
	"sync"
)

// Code is an ISO 3166-1 alpha-2 country code; the zero value means unknown.
type Code [2]byte

func (c Code) String() string {
	if !c.Known() {
		return ""
	}
	return string(c[:])
}

func (c Code) Known() bool {
	return c != Code{}
}

type Source struct {
	Name           string `json:"name"`
	Month          string `json:"month"`
	Attribution    string `json:"attribution"`
	AttributionURL string `json:"attribution_url"`
}

type Table struct {
	ipv4Starts    []uint32
	ipv4Countries []Code
	ipv6Starts    []uint64
	ipv6Countries []Code
	source        Source
}

var (
	//go:embed data/countries.bin.gz
	embeddedTable []byte
	//go:embed data/source.json
	embeddedSource []byte

	globalUnicastIPv6 = netip.MustParsePrefix("2000::/3")
	errSource         = errors.New("country: source.json lacks the name, month or attribution")
	load              = sync.OnceValues(func() (*Table, error) { return decode(embeddedTable, embeddedSource) })
)

func Load() (*Table, error) {
	return load()
}

func (t *Table) Source() Source {
	return t.source
}

func (t *Table) Lookup(address netip.Addr) Code {
	address = address.Unmap().WithZone("")
	if !address.IsGlobalUnicast() || address.IsPrivate() {
		return Code{}
	}
	if address.Is4() {
		bytes := address.As4()
		return find(t.ipv4Starts, t.ipv4Countries, binary.BigEndian.Uint32(bytes[:]))
	}
	if !globalUnicastIPv6.Contains(address) {
		return Code{}
	}
	bytes := address.As16()
	return find(t.ipv6Starts, t.ipv6Countries, binary.BigEndian.Uint64(bytes[:8]))
}

func find[Start uint32 | uint64](starts []Start, countries []Code, key Start) Code {
	position, found := slices.BinarySearch(starts, key)
	if found {
		return countries[position]
	}
	if position == 0 {
		return Code{}
	}
	return countries[position-1]
}

func decode(compressedTable, sourceDocument []byte) (*Table, error) {
	table, err := decodeTable(compressedTable)
	if err != nil {
		return nil, err
	}
	if sourceErr := json.Unmarshal(sourceDocument, &table.source); sourceErr != nil {
		return nil, sourceErr
	}
	if table.source.Name == "" || table.source.Month == "" || table.source.Attribution == "" ||
		table.source.AttributionURL == "" {
		return nil, errSource
	}
	return table, nil
}

package country

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"net/netip"
	"testing"
)

func loadTable(tb testing.TB) *Table {
	tb.Helper()
	table, err := Load()
	if err != nil {
		tb.Fatal(err)
	}
	return table
}

func TestLookupFollowsTheData(t *testing.T) {
	table := loadTable(t)
	for address, want := range map[string]string{
		"77.75.71.255":                           "FR",
		"77.75.72.0":                             "CZ",
		"77.75.79.255":                           "CZ",
		"77.75.80.0":                             "DK",
		"8.8.8.8":                                "US",
		"1.1.1.1":                                "AU",
		"2a02:598::1":                            "CZ",
		"2a02:598:ffff:ffff:ffff:ffff:ffff:ffff": "CZ",
		"2a02:599::":                             "NL",
		"2001:4860:4860::8888":                   "CA",
		"::ffff:77.75.72.1":                      "CZ",
		"192.88.99.1":                            "",
		"2001:db8::1":                            "",
		"10.0.0.1":                               "",
		"192.168.1.1":                            "",
		"127.0.0.1":                              "",
		"0.0.0.0":                                "",
		"255.255.255.255":                        "",
		"::1":                                    "",
		"::":                                     "",
		"fd00::1":                                "",
		"fe80::1%en0":                            "",
		"fec0::1":                                "",
		"ff02::1":                                "",
		"::ffff:10.0.0.1":                        "",
		"2a02:598:ffff:ffff:ffff:ffff:ffff:fffe%1": "CZ",
	} {
		if got := table.Lookup(netip.MustParseAddr(address)).String(); got != want {
			t.Errorf("Lookup(%s) = %q, want %q", address, got, want)
		}
	}
	if table.Lookup(netip.Addr{}).Known() {
		t.Error("the zero address has a country")
	}
}

func TestLookupDoesNotAllocate(t *testing.T) {
	table := loadTable(t)
	addresses := []netip.Addr{
		netip.MustParseAddr("8.8.8.8"),
		netip.MustParseAddr("2a02:598::1"),
		netip.MustParseAddr("::ffff:77.75.72.1"),
	}
	allocations := testing.AllocsPerRun(100, func() {
		for _, address := range addresses {
			table.Lookup(address)
		}
	})
	if allocations != 0 {
		t.Errorf("Lookup allocates %v times", allocations)
	}
}

func TestSourceNamesTheAttribution(t *testing.T) {
	source := loadTable(t).Source()
	if source.Attribution != "IP Geolocation by DB-IP" || source.AttributionURL != "https://db-ip.com" {
		t.Errorf("source = %+v", source)
	}
}

func TestDamagedTableIsRefused(t *testing.T) {
	archive, err := gzip.NewReader(bytes.NewReader(embeddedTable))
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(archive)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = parseTable(data); err != nil {
		t.Fatal(err)
	}
	damaged := map[string][]byte{
		"empty":           nil,
		"header only":     data[:len(tableHeader)],
		"cut in the IPv4": data[:len(data)/4],
		"cut in the IPv6": data[:len(data)*3/4],
		"one byte short":  data[:len(data)-1],
		"trailing byte":   append(bytes.Clone(data), 0),
		"other version":   append([]byte("NZCC\x02"), data[len(tableHeader):]...),
		"cut in a start":  append([]byte(tableHeader), 1, 0x80, 0x80, 0x80),
		"lowercase code":  append([]byte(tableHeader), 1, 0, 'c', 'z'),
		"repeated start":  append([]byte(tableHeader), 2, 0, 0, 'C', 'Z', 'S', 'K'),
	}
	for name, table := range damaged {
		if _, err = parseTable(table); !errors.Is(err, errTable) {
			t.Errorf("%s: err = %v, want errTable", name, err)
		}
	}
	for name, compressed := range map[string][]byte{
		"truncated gzip": embeddedTable[:len(embeddedTable)/2],
		"flipped byte":   flipByte(embeddedTable, len(embeddedTable)/2),
		"not gzip":       data,
	} {
		if _, err = decodeTable(compressed); !errors.Is(err, errTable) {
			t.Errorf("%s: err = %v, want errTable", name, err)
		}
	}
}

func TestDecodeRefusesADamagedTableOrSource(t *testing.T) {
	t.Parallel()
	if _, err := decode(embeddedTable[:10], embeddedSource); !errors.Is(err, errTable) {
		t.Errorf("a damaged table: %v", err)
	}
	if _, err := decode(embeddedTable, []byte("{")); err == nil {
		t.Error("a source that is not JSON was accepted")
	}
	if _, err := decode(embeddedTable, []byte(`{"name":"RIR","month":"2026-11"}`)); !errors.Is(err, errSource) {
		t.Errorf("a source without its attribution: %v", err)
	}
}

func TestAnAddressBeforeTheFirstRangeHasNoCountry(t *testing.T) {
	t.Parallel()
	if found := find([]uint32{10, 20}, []Code{{'C', 'Z'}, {'S', 'K'}}, 5); found.Known() {
		t.Fatalf("an address before the first range: %s", found)
	}
}

func flipByte(data []byte, position int) []byte {
	flipped := bytes.Clone(data)
	flipped[position] ^= 0xff
	return flipped
}

func BenchmarkLoad(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		if _, err := decode(embeddedTable, embeddedSource); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLookup(b *testing.B) {
	table := loadTable(b)
	address := netip.MustParseAddr("2a02:598::1")
	for b.Loop() {
		table.Lookup(address)
	}
}

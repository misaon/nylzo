package hostusage

import (
	"errors"
	"os"
	"testing"
	"testing/fstest"
)

func TestProcReaderFromFixtures(t *testing.T) {
	root := fstest.MapFS{
		"stat":    {Data: fixture(t, "stat_first")},
		"meminfo": {Data: fixture(t, "meminfo")},
		"loadavg": {Data: fixture(t, "loadavg")},
	}
	reader := newProcReader(root, 4)
	first, err := reader.read()
	if err != nil || first.Processor >= 0 || first.Load != 0.5 || first.MemoryTotal == 0 {
		t.Fatalf("first reading %+v, %v", first, err)
	}
	root["stat"] = &fstest.MapFile{Data: fixture(t, "stat_second")}
	if second, secondErr := reader.read(); secondErr != nil || second.Processor != 0.5 {
		t.Fatalf("second reading %+v, %v", second, secondErr)
	}
	malformed := newProcReader(fstest.MapFS{
		"stat":    {Data: []byte("intr 1 2 3 4 5 6\n")},
		"meminfo": {Data: []byte("MemTotal: 5 kB\n")},
		"loadavg": {Data: []byte("abc\n")},
	}, 4)
	if reading, malformedErr := malformed.read(); !errors.Is(malformedErr, errMalformed) ||
		reading != unknownReading() {
		t.Fatalf("malformed files gave %+v, %v", reading, malformedErr)
	}
	broken := newProcReader(os.DirFS(t.TempDir()), 4)
	if reading, brokenErr := broken.read(); brokenErr == nil || reading.MemoryTotal != 0 || reading.Processor >= 0 {
		t.Fatalf("unreadable files gave %+v, %v", reading, brokenErr)
	}
}

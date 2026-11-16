package hostusage

import (
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
	broken := newProcReader(os.DirFS(t.TempDir()), 4)
	if reading, brokenErr := broken.read(); brokenErr == nil || reading.MemoryTotal != 0 || reading.Processor >= 0 {
		t.Fatalf("unreadable files gave %+v, %v", reading, brokenErr)
	}
}

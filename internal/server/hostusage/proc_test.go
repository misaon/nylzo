package hostusage

import (
	"os"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestProcessorShareBetweenTwoReadings(t *testing.T) {
	first, err := parseProcessorTicks(fixture(t, "stat_first"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := parseProcessorTicks(fixture(t, "stat_second"))
	if err != nil {
		t.Fatal(err)
	}
	if share := processorShare(first, second); share != 0.5 {
		t.Fatalf("share = %v, want 0.5", share)
	}
	if share := processorShare(second, first); share >= 0 {
		t.Fatalf("a counter that went backwards gave %v, want unknown", share)
	}
	if share := processorShare(first, first); share >= 0 {
		t.Fatalf("no elapsed ticks gave %v, want unknown", share)
	}
}

func TestMemoryAndLoadFromFixtures(t *testing.T) {
	memory, err := parseMemory(fixture(t, "meminfo"))
	if err != nil {
		t.Fatal(err)
	}
	const kibibytes = 1024
	if memory.Total != 16000000*kibibytes || memory.Used != 12000000*kibibytes {
		t.Fatalf("used %d of %d", memory.Used, memory.Total)
	}
	load, err := parseLoad(fixture(t, "loadavg"), 4)
	if err != nil || load != 0.5 {
		t.Fatalf("load = %v, %v", load, err)
	}
}

func TestMalformedInputIsUnknownWithoutPanic(t *testing.T) {
	for _, input := range []string{"", "\n", "cpu", "cpu a b c d e", "intr 1 2 3 4 5 6", "cpu 1 2 3 4 -5"} {
		if _, err := parseProcessorTicks([]byte(input)); err == nil {
			t.Errorf("processor %q parsed", input)
		}
	}
	malformedMemory := []string{
		"", "MemTotal: x kB\nMemAvailable: 1 kB", "MemTotal: 5 kB", "MemTotal: 0 kB\nMemAvailable: 0 kB",
		"MemTotal: 18446744073709551615 kB\nMemAvailable: 1 kB",
	}
	for _, input := range malformedMemory {
		if _, err := parseMemory([]byte(input)); err == nil {
			t.Errorf("memory %q parsed", input)
		}
	}
	for _, input := range []string{"", "NaN 1 1", "-1 0 0", "Inf 0 0", "abc"} {
		if _, err := parseLoad([]byte(input), 2); err == nil {
			t.Errorf("load %q parsed", input)
		}
	}
	if _, err := parseLoad([]byte("1.0"), 0); err == nil {
		t.Error("load without cores parsed")
	}
}

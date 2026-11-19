package harden

import "testing"

// TestProtectOnMacOS checks that a protected process writes no core file, sets PT_DENY_ATTACH without error and swaps
// only to storage that macOS encrypts.
func TestProtectOnMacOS(t *testing.T) {
	_, report := startChild(t, "full")
	if report.Facts["core_soft"] != 0 || report.Facts["core_hard"] != 0 {
		t.Errorf("the core file limit stayed %d/%d", report.Facts["core_soft"], report.Facts["core_hard"])
	}
	if report.Warning != "" {
		t.Errorf("macOS encrypts swap, yet the child warned: %s", report.Warning)
	}
}

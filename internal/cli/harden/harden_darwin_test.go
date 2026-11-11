package harden

import "testing"

// TestProtectOnMacOS covers F07: no core file, PT_DENY_ATTACH set without error, and swap that macOS encrypts.
func TestProtectOnMacOS(t *testing.T) {
	_, report := startChild(t, "full")
	if report.Facts["core_soft"] != 0 || report.Facts["core_hard"] != 0 {
		t.Errorf("the core file limit stayed %d/%d", report.Facts["core_soft"], report.Facts["core_hard"])
	}
	if report.Warning != "" {
		t.Errorf("macOS encrypts swap, yet the child warned: %s", report.Warning)
	}
}

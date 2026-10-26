package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestReleaseMetadataOverridesVCS(t *testing.T) {
	information := &debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: "wrong"},
		{Key: "vcs.time", Value: "wrong"},
		{Key: "vcs.modified", Value: "true"},
	}}
	got := resolve("1.2.3", "release-commit", "2026-10-26T09:00:00Z", "false", information)
	want := Info{Version: "1.2.3", Commit: "release-commit", Date: "2026-10-26T09:00:00Z"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestDevelopmentMetadataDoesNotImpersonateARelease(t *testing.T) {
	information := &debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: "0123456789abcdef0123456789abcdef01234567"},
		{Key: "vcs.time", Value: "2026-10-26T09:00:00Z"},
		{Key: "vcs.modified", Value: "true"},
	}}
	got := resolve("dev", "", "", "", information)
	if got.Version != "0.0.0-dev.g0123456789ab.dirty" || !got.Dirty || got.Commit != information.Settings[0].Value {
		t.Fatalf("development metadata: %+v", got)
	}
	information.Main.Version = "v1.2.3"
	if installed := resolve("dev", "", "", "", information); installed.Version != "1.2.3" {
		t.Fatalf("installed module: %+v", installed)
	}
	if unknown := resolve("", "", "", "", nil); unknown.Version != "dev" {
		t.Fatalf("unknown source: %+v", unknown)
	}
}

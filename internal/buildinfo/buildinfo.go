package buildinfo

import (
	"cmp"
	"runtime/debug"
	"strings"
)

const development = "dev"

type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
	Dirty   bool   `json:"dirty"`
}

func Read(version, commit, date, dirty string) Info {
	information, _ := debug.ReadBuildInfo()
	return resolve(version, commit, date, dirty, information)
}

func resolve(version, commit, date, dirty string, information *debug.BuildInfo) Info {
	if information == nil {
		information = &debug.BuildInfo{}
	}
	result := Info{
		Commit: cmp.Or(commit, setting(information, "vcs.revision")),
		Date:   cmp.Or(date, setting(information, "vcs.time")),
		Dirty:  cmp.Or(dirty, setting(information, "vcs.modified")) == "true",
	}
	result.Version = versionOf(version, information.Main.Version, result.Commit, result.Dirty)
	return result
}

func versionOf(override, module, commit string, dirty bool) string {
	if override != "" && override != development {
		return override
	}
	if module != "" && module != "(devel)" {
		return strings.TrimPrefix(module, "v")
	}
	if commit == "" {
		return development
	}
	version := "0.0.0-dev.g" + commit[:min(12, len(commit))]
	if dirty {
		version += ".dirty"
	}
	return version
}

func setting(information *debug.BuildInfo, name string) string {
	for _, setting := range information.Settings {
		if setting.Key == name {
			return setting.Value
		}
	}
	return ""
}

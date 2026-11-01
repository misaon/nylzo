//go:build !darwin && !windows

package local

func systemOpener(target string) (string, []string) {
	return "xdg-open", []string{target}
}

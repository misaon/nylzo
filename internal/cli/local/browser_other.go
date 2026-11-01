//go:build !darwin && !windows

package local

func systemOpener(target string) []string {
	return []string{"xdg-open", target}
}

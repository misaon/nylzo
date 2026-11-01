//go:build !darwin

package local

func systemResolvesLocalhost() bool {
	return true
}

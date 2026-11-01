//go:build windows

package datadir

// Secure does nothing on Windows, where the server refuses to start (D54).
func Secure(Layout) (int, error) {
	return 0, nil
}

func Check(Layout) error {
	return nil
}

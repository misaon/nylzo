//go:build !linux

package cli

func hardenProcess() error {
	return nil
}

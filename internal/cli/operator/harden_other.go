//go:build !linux

package operator

func hardenProcess() error {
	return nil
}

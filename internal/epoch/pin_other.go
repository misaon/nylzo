//go:build !linux

package epoch

func pinFile(string) ([]byte, error) {
	return nil, nil
}

func unpinFile([]byte) {}

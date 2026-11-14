//go:build !linux && !darwin

package harden

func ownFacts() map[string]uint64 {
	return nil
}

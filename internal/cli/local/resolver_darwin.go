package local

import (
	"strconv"
	"strings"
	"syscall"
)

const firstMacOSResolvingLocalhost = 26

// Safari leaves names under localhost to macOS, which keeps them on this computer only from macOS 26.
func systemResolvesLocalhost() bool {
	version, err := syscall.Sysctl("kern.osproductversion")
	if err != nil {
		return false
	}
	major, _, _ := strings.Cut(version, ".")
	number, err := strconv.Atoi(major)
	return err == nil && number >= firstMacOSResolvingLocalhost
}

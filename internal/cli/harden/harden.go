// Package harden protects a nylzo process before it reads a secret: no core file, no debugger of the same user and,
// where the system allows it, no page in swap.
package harden

import "errors"

// Level says how much a command protects its process; the zero value protects the most.
type Level int

const (
	// Full keeps the memory out of core files, away from debuggers and, where it can, out of swap.
	Full Level = iota
	// ProcessOnly keeps the memory out of core files and away from debuggers; serve locks its memory itself.
	ProcessOnly
	// Off leaves the process as it is, for a command that holds no secret.
	Off
)

var errTraced = errors.New("a debugger or tracer is attached to this process, so it reads no secret")

// Protect applies the level before the command reads anything secret. The warning names swap that Linux or macOS
// reports as unencrypted while the memory is not locked; Windows cannot tell, so it never warns.
func Protect(level Level) (string, error) {
	if level == Off {
		return "", nil
	}
	if err := disableDumps(); err != nil {
		return "", err
	}
	if level == ProcessOnly {
		return "", nil
	}
	return keepFromSwap(), nil
}

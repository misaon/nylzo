//go:build !windows

package disk

import "syscall"

// poolOpenFlags never follow a link and never block on a FIFO, so a claim only writes into a file the pool created.
const poolOpenFlags = syscall.O_NOFOLLOW | syscall.O_NONBLOCK

// Package privatefile creates new files and directories that only the user of the process can open (F04).
package privatefile

import "errors"

var ErrNotPrivate = errors.New("privatefile: the file system there does not keep files private")

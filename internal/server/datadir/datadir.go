// Package datadir keeps the server's files private to the user that runs it (spec section 13, D99).
package datadir

import "errors"

var (
	ErrForeignOwner = errors.New("datadir: an entry belongs to another user; fix the owner before starting")
	ErrSymlink      = errors.New("datadir: an entry is a symbolic link; replace it with the file or directory")
)

// Layout names the parts of the data directory that hold server data.
type Layout struct {
	Directory string
	Files     []string
	Trees     []string
}

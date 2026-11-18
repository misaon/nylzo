//go:build !windows

package cli

import (
	"github.com/misaon/nylzo/internal/store"
	"github.com/misaon/nylzo/internal/store/disk"
)

func testBlobs(directory string) (store.Blobs, error) { //nolint:ireturn // the blob store differs by OS
	return disk.New(directory)
}

package epoch

import "os"

// Windows runs only development servers, one process per data directory.
func lock(*os.File) error {
	return nil
}

func unlock(*os.File) {}

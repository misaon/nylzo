package datadir

// Hold does nothing on Windows, where the server refuses to start (D54).
func Hold(string) (func(), error) {
	return func() {}, nil
}

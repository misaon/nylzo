// Package debugenv finds environment settings that make Go's HTTP/2 code or quic-go record request headers.
package debugenv

import "strings"

// HTTP2HeaderDump names the GODEBUG setting that makes net/http's HTTP/2 code print every header, or returns "".
// It matches the substrings that net/http itself looks for.
func HTTP2HeaderDump(getenv func(string) string) string {
	godebug := getenv("GODEBUG")
	for _, setting := range []string{"http2debug=1", "http2debug=2"} {
		if strings.Contains(godebug, setting) {
			return "GODEBUG=" + setting
		}
	}
	return ""
}

// HeaderDump also names QLOGDIR, which makes quic-go write HTTP/3 headers to files, for a process that serves HTTP/3.
func HeaderDump(getenv func(string) string) string {
	if setting := HTTP2HeaderDump(getenv); setting != "" {
		return setting
	}
	if getenv("QLOGDIR") != "" {
		return "QLOGDIR"
	}
	return ""
}

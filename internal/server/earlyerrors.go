package server

import (
	"bytes"
	"net"
	"net/http"
	"slices"

	"github.com/misaon/nylzo/internal/server/webclient"
)

// netHTTPErrorHeaders is what net/http writes after the status line of an error it answers before any handler runs.
const netHTTPErrorHeaders = "\r\nContent-Type: text/plain; charset=utf-8\r\nConnection: close\r\n\r\n"

// SecureEarlyErrors gives the 400, 431 and 501 that net/http writes before routing the security headers of every
// other response (spec §9).
func SecureEarlyErrors(listener net.Listener) net.Listener {
	header := http.Header{}
	webclient.SetSecurityHeaders(header, false)
	var block bytes.Buffer
	for _, name := range slices.Sorted(func(yield func(string) bool) {
		for name := range header {
			if !yield(name) {
				return
			}
		}
	}) {
		block.WriteString(name + ": " + header.Get(name) + "\r\n")
	}
	return &earlyErrorListener{Listener: listener, headers: block.Bytes()}
}

type earlyErrorListener struct {
	net.Listener

	headers []byte
}

func (l *earlyErrorListener) Accept() (net.Conn, error) {
	connection, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &earlyErrorConnection{Conn: connection, headers: l.headers}, nil
}

type earlyErrorConnection struct {
	net.Conn

	headers []byte
}

// Write matches only a status line followed at once by the error headers, which no routed response has: net/http
// sorts the headers of those and puts Connection before Content-Type.
func (c *earlyErrorConnection) Write(data []byte) (int, error) {
	at := bytes.Index(data, []byte(netHTTPErrorHeaders))
	if !bytes.HasPrefix(data, []byte("HTTP/1.1 ")) || at < 0 || bytes.Contains(data[:at], []byte("\r\n")) {
		return c.Conn.Write(data)
	}
	statusLineEnd := at + len("\r\n")
	if _, err := c.Conn.Write(slices.Concat(data[:statusLineEnd], c.headers, data[statusLineEnd:])); err != nil {
		return 0, err
	}
	return len(data), nil
}

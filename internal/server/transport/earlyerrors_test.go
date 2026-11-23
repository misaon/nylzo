package transport

import (
	"net"
	"testing"
)

func TestAnEarlyErrorReportsAFailedWrite(t *testing.T) {
	t.Parallel()
	server, client := net.Pipe()
	client.Close()
	connection := &earlyErrorConnection{Conn: server, headers: []byte("X-Frame-Options: DENY\r\n")}
	written, err := connection.Write([]byte("HTTP/1.1 400 Bad Request" + netHTTPErrorHeaders + "400 Bad Request"))
	if err == nil || written != 0 {
		t.Fatalf("a write to a closed connection gave %d, %v", written, err)
	}
}

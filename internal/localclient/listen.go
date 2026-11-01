package localclient

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
)

const (
	loopbackIPv4   = "127.0.0.1"
	loopbackIPv6   = "::1"
	listenAttempts = 32
)

var errNoFreePort = errors.New("localclient: found no port that is free on both 127.0.0.1 and [::1]")

type Listeners struct {
	port      int
	listeners []net.Listener
}

// Listen holds one random port on both loopback addresses, so no other program answers the session's name there.
func Listen(ctx context.Context) (*Listeners, error) {
	var config net.ListenConfig
	probe, err := config.Listen(ctx, "tcp6", net.JoinHostPort(loopbackIPv6, "0"))
	withIPv6 := err == nil
	if withIPv6 {
		probe.Close()
	}
	for range listenAttempts {
		ipv4, listenErr := config.Listen(ctx, "tcp4", net.JoinHostPort(loopbackIPv4, "0"))
		if listenErr != nil {
			return nil, listenErr
		}
		port, ok := listenPort(ipv4)
		if !ok {
			ipv4.Close()
			return nil, errNoFreePort
		}
		if !withIPv6 {
			return &Listeners{port: port, listeners: []net.Listener{ipv4}}, nil
		}
		ipv6, sameErr := config.Listen(ctx, "tcp6", net.JoinHostPort(loopbackIPv6, strconv.Itoa(port)))
		if sameErr == nil {
			return &Listeners{port: port, listeners: []net.Listener{ipv4, ipv6}}, nil
		}
		ipv4.Close()
	}
	return nil, errNoFreePort
}

func listenPort(listener net.Listener) (int, bool) {
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok || address.Port == 0 {
		return 0, false
	}
	return address.Port, true
}

func (l *Listeners) Port() int {
	return l.port
}

func (l *Listeners) Serve(httpServer *http.Server) error {
	failed := make(chan error, len(l.listeners))
	for _, listener := range l.listeners {
		go func() { failed <- httpServer.Serve(listener) }()
	}
	return <-failed
}

func (l *Listeners) Close() {
	for _, listener := range l.listeners {
		listener.Close()
	}
}

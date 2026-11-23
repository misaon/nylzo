package localclient

import (
	"context"
	"errors"
	"net"
	"syscall"
	"testing"
)

type unixAddressListener struct {
	net.Listener
}

func (unixAddressListener) Addr() net.Addr { return &net.UnixAddr{Name: "socket", Net: "unix"} }

type loopback struct {
	ipv6      bool
	ipv4Fails bool
	unix      bool
	opened    []net.Listener
}

func (l *loopback) open(ctx context.Context, network, address string) (net.Listener, error) {
	switch {
	case network == "tcp6" && (!l.ipv6 || address != "[::1]:0"):
		return nil, syscall.EADDRNOTAVAIL
	case network == "tcp4" && l.ipv4Fails:
		return nil, syscall.EADDRINUSE
	}
	var config net.ListenConfig
	listener, err := config.Listen(ctx, "tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	l.opened = append(l.opened, listener)
	if l.unix {
		return unixAddressListener{listener}, nil
	}
	return listener, nil
}

func (l *loopback) closeAll() {
	for _, listener := range l.opened {
		listener.Close()
	}
}

func TestListenHoldsTheIPv4PortAloneWithoutIPv6(t *testing.T) {
	t.Parallel()
	system := &loopback{}
	defer system.closeAll()
	listeners, err := listenWith(t.Context(), system.open)
	if err != nil || len(listeners.listeners) != 1 || listeners.Port() == 0 {
		t.Fatalf("listen without IPv6: %+v, %v", listeners, err)
	}
}

func TestListenRefusesWhenItCannotHoldAPort(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		system *loopback
		want   error
	}{
		"IPv4 refuses":                 {&loopback{ipv4Fails: true}, syscall.EADDRINUSE},
		"the port is not a TCP port":   {&loopback{unix: true}, errNoFreePort},
		"IPv6 never has the same port": {&loopback{ipv6: true}, errNoFreePort},
	} {
		listeners, err := listenWith(t.Context(), test.system.open)
		if !errors.Is(err, test.want) || listeners != nil {
			t.Errorf("%s: %+v, %v", name, listeners, err)
		}
		for _, listener := range test.system.opened {
			if _, acceptErr := listener.Accept(); !errors.Is(acceptErr, net.ErrClosed) {
				t.Errorf("%s: a refused listen left %s open", name, listener.Addr())
			}
		}
		test.system.closeAll()
	}
}

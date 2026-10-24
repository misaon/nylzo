package main

import (
	"crypto/tls"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func server(t *testing.T, curves []tls.CurveID) (addr, ca string) {
	t.Helper()
	ts := httptest.NewUnstartedServer(http.NotFoundHandler())
	ts.TLS = &tls.Config{CurvePreferences: curves}
	ts.StartTLS()
	t.Cleanup(ts.Close)
	ca = filepath.Join(t.TempDir(), "root.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ts.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	return ts.Listener.Addr().String(), ca
}

func TestCheckAcceptsAHybridKeyExchange(t *testing.T) {
	t.Parallel()
	addr, ca := server(t, []tls.CurveID{tls.X25519MLKEM768, tls.X25519})
	if err := check(addr, ca); err != nil {
		t.Fatal(err)
	}
}

func TestCheckRefusesAClassicalKeyExchange(t *testing.T) {
	t.Parallel()
	addr, ca := server(t, []tls.CurveID{tls.X25519})
	if err := check(addr, ca); err == nil {
		t.Fatal("a server without X25519MLKEM768 passed")
	}
}

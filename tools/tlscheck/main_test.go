package main

import (
	"crypto/tls"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func server(t *testing.T, curves []tls.CurveID) (addr, ca string) {
	t.Helper()
	ts := httptest.NewUnstartedServer(http.NotFoundHandler())
	ts.TLS = &tls.Config{CurvePreferences: curves}
	ts.StartTLS()
	t.Cleanup(ts.Close)
	ca = filepath.Join(t.TempDir(), "root.pem")
	if err := os.WriteFile(
		ca,
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ts.Certificate().Raw}),
		0o600,
	); err != nil {
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
	err := check(addr, ca)
	if err == nil || !strings.Contains(err.Error(), "negotiated X25519 instead") {
		t.Fatalf("a server without X25519MLKEM768 must be refused after the handshake, got %v", err)
	}
}

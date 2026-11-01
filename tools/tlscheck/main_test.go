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

type testServer struct {
	address  string
	rootFile string
}

func startServer(t *testing.T, curves []tls.CurveID, minimum ...uint16) testServer {
	t.Helper()
	server := httptest.NewUnstartedServer(http.NotFoundHandler())
	server.TLS = &tls.Config{CurvePreferences: curves}
	if len(minimum) > 0 {
		server.TLS.MinVersion = minimum[0]
	}
	server.StartTLS()
	t.Cleanup(server.Close)
	rootFile := filepath.Join(t.TempDir(), "root.pem")
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(rootFile, certificate, 0o600); err != nil {
		t.Fatal(err)
	}
	return testServer{address: server.Listener.Addr().String(), rootFile: rootFile}
}

func TestCheckAcceptsAHybridKeyExchange(t *testing.T) {
	t.Parallel()
	server := startServer(t, []tls.CurveID{tls.X25519MLKEM768, tls.X25519})
	if err := check(server.address, server.rootFile); err != nil {
		t.Fatal(err)
	}
}

func TestCheckRefusesAClassicalKeyExchange(t *testing.T) {
	t.Parallel()
	server := startServer(t, []tls.CurveID{tls.X25519})
	err := check(server.address, server.rootFile)
	if err == nil || !strings.Contains(err.Error(), "negotiated X25519 instead") {
		t.Fatalf("a server without X25519MLKEM768 must be refused after the handshake, got %v", err)
	}
}

func TestOnlyRefusesAServerThatStillAcceptsClassicalHandshakes(t *testing.T) {
	t.Parallel()
	for name, server := range map[string]testServer{
		"X25519 fallback": startServer(t, []tls.CurveID{tls.X25519MLKEM768, tls.X25519}, tls.VersionTLS13),
		"TLS 1.2":         startServer(t, []tls.CurveID{tls.X25519MLKEM768, tls.CurveP256}),
	} {
		if err := refusesClassical(server.address, server.rootFile); err == nil {
			t.Errorf("%s: a server that accepts classical handshakes passed", name)
		}
	}
	strict := startServer(t, []tls.CurveID{tls.X25519MLKEM768}, tls.VersionTLS13)
	if err := refusesClassical(strict.address, strict.rootFile); err != nil {
		t.Fatal(err)
	}
}

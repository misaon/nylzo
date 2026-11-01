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

func TestChecksRejectMissingOrInvalidRootCertificatesAndAddresses(t *testing.T) {
	t.Parallel()
	server := startServer(t, []tls.CurveID{tls.X25519MLKEM768})
	invalid := filepath.Join(t.TempDir(), "invalid.pem")
	if err := os.WriteFile(invalid, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, probe := range map[string]func(string, string) error{"check": check, "only": refusesClassical} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, test := range []struct{ address, root, want string }{
				{server.address, invalid + ".missing", "no such file"},
				{server.address, invalid, "no certificate"},
				{"not-a-host-port", server.rootFile, "missing port"},
			} {
				if err := probe(test.address, test.root); err == nil || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("probe(%q, %q) = %v, want %q", test.address, test.root, err, test.want)
				}
			}
		})
	}
}

func TestCheckRejectsAnUntrustedServer(t *testing.T) {
	t.Parallel()
	server := startServer(t, []tls.CurveID{tls.X25519MLKEM768})
	// httptest shares a static certificate, so use a valid certificate that cannot authenticate this address.
	if err := check("localhost:"+strings.Split(server.address, ":")[1], server.rootFile); err == nil {
		t.Fatal("accepted a certificate for a different hostname")
	}
}

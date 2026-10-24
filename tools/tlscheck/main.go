package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"time"
)

func main() {
	addr := flag.String("addr", "localhost:443", "host:port of the TLS server")
	ca := flag.String("ca", "", "PEM file with the root certificate to trust")
	flag.Parse()
	if err := check(*addr, *ca); err != nil {
		fmt.Fprintln(os.Stderr, "tlscheck:", err)
		os.Exit(1)
	}
	fmt.Println("tlscheck: TLS 1.3 with X25519MLKEM768")
}

func check(addr, ca string) error {
	pem, err := os.ReadFile(ca)
	if err != nil {
		return err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		return errors.New("no certificate in the root file")
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{
		RootCAs:          roots,
		ServerName:       host,
		MinVersion:       tls.VersionTLS13,
		CurvePreferences: []tls.CurveID{tls.X25519MLKEM768},
	})
	if err != nil {
		return fmt.Errorf("handshake offering only X25519MLKEM768: %w", err)
	}
	defer conn.Close()
	if state := conn.ConnectionState(); state.CurveID != tls.X25519MLKEM768 {
		return fmt.Errorf("negotiated %s instead of X25519MLKEM768", state.CurveID)
	}
	return nil
}

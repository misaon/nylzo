package main

import (
	"context"
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
	address := flag.String("addr", "localhost:443", "host:port of the TLS server")
	rootFile := flag.String("ca", "", "PEM file with the root certificate to trust")
	flag.Parse()
	if err := check(*address, *rootFile); err != nil {
		fmt.Fprintln(os.Stderr, "tlscheck:", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stdout, "tlscheck: TLS 1.3 with X25519MLKEM768")
}

func check(address, rootFile string) error {
	pem, err := os.ReadFile(rootFile)
	if err != nil {
		return err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		return errors.New("no certificate in the root file")
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dialer := &tls.Dialer{Config: &tls.Config{
		RootCAs:          roots,
		ServerName:       host,
		MinVersion:       tls.VersionTLS13,
		CurvePreferences: []tls.CurveID{tls.X25519MLKEM768, tls.X25519},
	}}
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("handshake offering X25519MLKEM768 and X25519: %w", err)
	}
	defer connection.Close()
	tlsConnection, ok := connection.(*tls.Conn)
	if !ok {
		return errors.New("the dialer returned a connection without TLS")
	}
	if curve := tlsConnection.ConnectionState().CurveID; curve != tls.X25519MLKEM768 {
		return fmt.Errorf("negotiated %s instead of X25519MLKEM768", curve)
	}
	return nil
}

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
	only := flag.Bool("only", false, "also require that TLS 1.2 and classical key exchanges are refused")
	flag.Parse()
	err := check(*address, *rootFile)
	if err == nil && *only {
		err = refusesClassical(*address, *rootFile)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "tlscheck:", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stdout, "tlscheck: TLS 1.3 with X25519MLKEM768")
}

func refusesClassical(address, rootFile string) error {
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
	for name, config := range map[string]*tls.Config{
		"TLS 1.2":    {MaxVersion: tls.VersionTLS12},
		"X25519":     {MinVersion: tls.VersionTLS13, CurvePreferences: []tls.CurveID{tls.X25519}},
		"ECDH P-256": {MinVersion: tls.VersionTLS13, CurvePreferences: []tls.CurveID{tls.CurveP256}},
	} {
		config.RootCAs, config.ServerName = roots, host
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		connection, err := (&tls.Dialer{Config: config}).DialContext(ctx, "tcp", address)
		cancel()
		if err == nil {
			_ = connection.Close()
			return fmt.Errorf("the server accepted a handshake with %s", name)
		}
	}
	return nil
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

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"time"
)

const dialTimeout = 10 * time.Second

var (
	errNoRootCertificate = errors.New("no certificate in the root file")
	errClassicalAccepted = errors.New("the server accepted a handshake with")
	errWrongCurve        = errors.New("instead of X25519MLKEM768")
)

var exit = os.Exit

func main() {
	address := flag.String("addr", "localhost:443", "host:port of the TLS server")
	rootFile := flag.String("ca", "", "PEM file with the root certificate to trust")
	only := flag.Bool("only", false, "also require that TLS 1.2 and classical key exchanges are refused")
	flag.Parse()
	exit(run(*address, *rootFile, *only, os.Stdout, os.Stderr))
}

func run(address, rootFile string, only bool, stdout, stderr io.Writer) int {
	err := check(address, rootFile)
	if err == nil && only {
		err = refusesClassical(address, rootFile)
	}
	if err != nil {
		fmt.Fprintln(stderr, "tlscheck:", err)
		return 1
	}
	fmt.Fprintln(stdout, "tlscheck: TLS 1.3 with X25519MLKEM768")
	return 0
}

func refusesClassical(address, rootFile string) error {
	pem, err := os.ReadFile(rootFile)
	if err != nil {
		return err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		return errNoRootCertificate
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
		ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
		var connection net.Conn
		connection, err = (&tls.Dialer{Config: config}).DialContext(ctx, "tcp", address)
		cancel()
		if err == nil {
			_ = connection.Close()
			return fmt.Errorf("%w %s", errClassicalAccepted, name)
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
		return errNoRootCertificate
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()
	raw, err := new(net.Dialer).DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("handshake offering X25519MLKEM768 and X25519: %w", err)
	}
	connection := tls.Client(raw, &tls.Config{
		RootCAs:          roots,
		ServerName:       host,
		MinVersion:       tls.VersionTLS13,
		CurvePreferences: []tls.CurveID{tls.X25519MLKEM768, tls.X25519},
	})
	defer connection.Close()
	if err = connection.HandshakeContext(ctx); err != nil {
		return fmt.Errorf("handshake offering X25519MLKEM768 and X25519: %w", err)
	}
	if curve := connection.ConnectionState().CurveID; curve != tls.X25519MLKEM768 {
		return fmt.Errorf("negotiated %s %w", curve, errWrongCurve)
	}
	return nil
}

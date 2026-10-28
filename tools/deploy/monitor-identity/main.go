// Command monitor-identity creates a private identity for read-only host monitoring.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

type credentials struct {
	certificate []byte
	key         []byte
}

func identity(now time.Time) (credentials, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return credentials{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return credentials{}, err
	}
	certificate := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "Nylzo read-only host monitor"},
		NotBefore:    now.Add(-5 * time.Minute), NotAfter: now.Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		return credentials{}, err
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return credentials{}, err
	}
	return credentials{
		certificate: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		key:         pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}),
	}, nil
}

func create(destination string) error {
	destination = filepath.Clean(destination)
	parent, err := os.OpenRoot(filepath.Dir(destination))
	if err != nil {
		return err
	}
	defer parent.Close()
	information, err := parent.Stat(".")
	if err != nil {
		return err
	}
	if information.Mode().Perm()&0o077 != 0 {
		return errors.New("the identity needs a private parent directory")
	}
	name := filepath.Base(destination)
	if directoryErr := parent.Mkdir(name, 0o700); directoryErr != nil {
		return directoryErr
	}
	root, err := parent.OpenRoot(name)
	if err != nil {
		return err
	}
	defer root.Close()
	issued, err := identity(time.Now())
	if err != nil {
		return err
	}
	defer clear(issued.key)
	for filename, contents := range map[string][]byte{"client.pem": issued.certificate, "client-key.pem": issued.key} {
		file, fileErr := root.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if fileErr != nil {
			return fileErr
		}
		_, writeErr := file.Write(contents)
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "Usage: go run ./tools/deploy/monitor-identity PRIVATE_DIRECTORY")
		os.Exit(2)
	}
	if err := create(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "Monitor identity creation failed; existing identities are never overwritten.")
		os.Exit(1)
	}
	fmt.Fprintln(
		os.Stdout,
		"Created a one-year client identity. Keep client-key.pem private; only client.pem belongs in host configuration.",
	)
}

package main

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIdentityIsAnExpiringClientOnlyLeaf(t *testing.T) {
	now := time.Now()
	identity, err := identity(now)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(identity.certificate)
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	keyBlock, _ := pem.Decode(identity.key)
	key, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if !certificate.PublicKey.(*ecdsa.PublicKey).Equal(&key.(*ecdsa.PrivateKey).PublicKey) || certificate.IsCA {
		t.Fatal("identity is not a matching, client-only leaf")
	}
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	options := x509.VerifyOptions{
		Roots:       roots,
		CurrentTime: now,
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	if _, verificationErr := certificate.Verify(options); verificationErr != nil {
		t.Fatal(verificationErr)
	}
	options.KeyUsages = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	if _, verificationErr := certificate.Verify(options); verificationErr == nil {
		t.Fatal("monitor identity authenticated a server")
	}
	options.KeyUsages = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	options.CurrentTime = now.Add(366 * 24 * time.Hour)
	if _, verificationErr := certificate.Verify(options); verificationErr == nil {
		t.Fatal("expired monitor identity was accepted")
	}
}

func TestCreationIsPrivateAndRefusesOverwrite(t *testing.T) {
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(parent, "identity")
	if err := create(directory); err == nil {
		t.Fatal("public parent directory was accepted")
	}
	if err := os.Chmod(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := create(directory); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "client.pem", "client-key.pem"} {
		info, err := os.Stat(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Fatal("identity is readable outside its owner")
		}
	}
	if err := create(directory); !errors.Is(err, os.ErrExist) {
		t.Fatal("existing identity was not protected")
	}
}

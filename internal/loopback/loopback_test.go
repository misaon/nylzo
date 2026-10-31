package loopback_test

import (
	"testing"

	"github.com/misaon/nylzo/internal/loopback"
)

func TestOnlyLocalhostAndLoopbackAddressesCount(t *testing.T) {
	t.Parallel()
	for host, want := range map[string]bool{
		"localhost":         true,
		"LocalHost":         true,
		"127.0.0.1":         true,
		"127.0.0.2":         true,
		"127.255.255.255":   true,
		"::1":               true,
		"0:0:0:0:0:0:0:1":   true,
		"":                  false,
		"localhost.":        false,
		"app.localhost":     false,
		"localhost.example": false,
		"localhoſt":         false,
		"[::1]":             false,
		"::1%lo0":           false,
		"::ffff:127.0.0.1":  false,
		"::ffff:7f00:2":     false,
		"::":                false,
		"0.0.0.0":           false,
		"126.255.255.255":   false,
		"128.0.0.1":         false,
		"10.0.0.1":          false,
		"fe80::1":           false,
		"127.1":             false,
		"0177.0.0.1":        false,
		"2130706433":        false,
	} {
		if got := loopback.Host(host); got != want {
			t.Errorf("Host(%q) = %t, want %t", host, got, want)
		}
	}
}

package operatoraddress_test

import (
	"strings"
	"testing"

	"github.com/misaon/nylzo/internal/server/operatoraddress"
)

func TestCheckPublicURL(t *testing.T) {
	t.Parallel()
	for _, valid := range []string{
		"https://nylzo.example", "https://nylzo.example:8443", "https://xn--nylzo-9ua.example", "https://192.0.2.1",
		"https://[2001:db8::1]:443", "http://localhost:8080", "http://127.0.0.1:8096", "http://[::1]:8080",
		"http://localhost",
	} {
		if err := operatoraddress.CheckPublicURL(valid); err != nil {
			t.Errorf("%q: %v", valid, err)
		}
	}
	for _, invalid := range []string{
		"", "nylzo.example", "//nylzo.example", "https://", "https://nylzo.example/", "https://nylzo.example/cs/",
		"https://Nylzo.example", "HTTPS://nylzo.example", "http://nylzo.example", "http://localhost.evil.example",
		"ftp://nylzo.example", "https://nylzo.example:", "https://nylzo.example:0", "https://nylzo.example:65536",
		"https://nylzo.example:08443", "https://user@nylzo.example", "https://nylzo.example?x=1",
		"https://nylzo.example?", "https://nylzo.example#top", "https://[fe80::1%25en0]", "https://nylzo_example.com",
		"https://-nylzo.example", "https://nylzo-.example", `https://ny"lzo.example`, "https://ny<lzo.example",
		"https://nylzo.example\n", "https://" + strings.Repeat("a", 64) + ".example", "https:nylzo.example",
	} {
		if err := operatoraddress.CheckPublicURL(invalid); err == nil {
			t.Errorf("%q passed", invalid)
		}
	}
}

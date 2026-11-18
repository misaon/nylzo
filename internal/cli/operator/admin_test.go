package operator

import (
	"testing"

	"github.com/misaon/nylzo/internal/server/config"
)

// Without NYLZO_PUBLIC_URL the enrollment link names this host as localhost, the one host the admin area accepts then,
// also when the server listens on every interface or only on a port.
func TestTheEnrollmentLinkWithoutAPublicURLPointsAtLocalhost(t *testing.T) {
	t.Parallel()
	for listen, want := range map[string]string{
		":8080":               "http://localhost:8080/admin",
		"0.0.0.0:8080":        "http://localhost:8080/admin",
		"127.0.0.1:8080":      "http://localhost:8080/admin",
		"[::]:8080":           "http://localhost:8080/admin",
		"[::1]:8080":          "http://localhost:8080/admin",
		"127.0.0.2:8080":      "http://127.0.0.2:8080/admin",
		"10.0.0.2:8080":       "http://10.0.0.2:8080/admin",
		"[fe80::1%eth0]:8080": "http://[fe80::1%25eth0]:8080/admin",
	} {
		if got := adminAddress(&config.Server{Listen: listen}); got != want {
			t.Errorf("listen %s: %s, want %s", listen, got, want)
		}
	}
}

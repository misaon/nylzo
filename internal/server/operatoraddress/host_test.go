package operatoraddress

import (
	"errors"
	"testing"
)

func TestCheckHostRefusesAnAddressWithAZone(t *testing.T) {
	t.Parallel()
	if err := checkHost("fe80::1%en0"); !errors.Is(err, errAddressZone) {
		t.Fatalf("an address with a zone: %v", err)
	}
}

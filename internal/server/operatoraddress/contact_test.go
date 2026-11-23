package operatoraddress_test

import (
	"testing"

	"github.com/misaon/nylzo/internal/server/operatoraddress"
)

func TestOperatorContactRefusesAContactTheTransparencyPageCouldNotLink(t *testing.T) {
	t.Parallel()
	contact, err := operatoraddress.OperatorContact([]byte(`{"operator":{"contact":"javascript:alert(1)"}}`))
	if err == nil || contact != "" {
		t.Fatalf("a script contact gave %q, %v", contact, err)
	}
	contact, err = operatoraddress.OperatorContact([]byte(`{"operator":{"contact":"abuse@example.org"}}`))
	if err != nil || contact != "abuse@example.org" {
		t.Fatalf("an email contact gave %q, %v", contact, err)
	}
}

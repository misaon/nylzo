package command

import (
	"bytes"
	"testing"
)

func TestFailureMessagesAreEscaped(t *testing.T) {
	t.Parallel()
	var stderr bytes.Buffer
	Failf(&Environment{Stderr: &stderr}, "%s", "spoof\x1b]0;x\x07\r\u202enylzo: saved")
	if got := stderr.String(); got != `nylzo: spoof\u001b]0;x\u0007\u000d\u202enylzo: saved`+"\n" {
		t.Fatalf("control and format characters must reach stderr escaped: %q", got)
	}
}

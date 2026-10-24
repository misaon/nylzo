package core_test

import (
	"strings"
	"testing"

	"github.com/misaon/nylzo/internal/core"
)

func TestReasonTooLongCountsTheRedactedReasonLikeTheWebClient(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		reason string
		long   bool
	}{
		{"empty", "", false},
		{"short", "phishing", false},
		{"long words", strings.Repeat("ab ", 400), true},
		{"a long code becomes short", strings.Repeat("a", 3000), false},
		{"fragments grow", strings.Repeat("# ", 150), true},
		{"fragments that still fit", strings.Repeat("# ", 50), false},
		{"html characters are not escaped", strings.Repeat("<", 1000), false},
		{"just over the limit", strings.Repeat("ab ", 333) + "ab", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := core.ReasonTooLong(tc.reason); got != tc.long {
				t.Fatalf("ReasonTooLong = %v, want %v", got, tc.long)
			}
		})
	}
}

package policy

import (
	"strings"
	"testing"
)

func TestRedactPII(t *testing.T) {
	input := "Email me at sam@example.com or +1 (555) 123-9876 and use 4242 4242 4242 4242."
	out, changed := RedactPII(input)
	if !changed {
		t.Fatalf("changed = false, want true")
	}
	for _, marker := range []string{"[REDACTED_EMAIL]", "[REDACTED_PHONE]", "[REDACTED_CARD]"} {
		if !strings.Contains(out, marker) {
			t.Fatalf("output missing marker %q: %q", marker, out)
		}
	}
}

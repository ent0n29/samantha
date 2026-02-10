package policy

import "regexp"

var (
	emailPattern = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
	phonePattern = regexp.MustCompile(`\+?[0-9][0-9\-() ]{7,}[0-9]`)
	cardPattern  = regexp.MustCompile(`\b(?:\d[ -]*?){13,19}\b`)
)

// RedactPII masks common high-risk PII patterns.
func RedactPII(input string) (redacted string, changed bool) {
	out := input

	next := emailPattern.ReplaceAllString(out, "[REDACTED_EMAIL]")
	changed = changed || next != out
	out = next

	// Run card redaction before phone to avoid card numbers being classified as phone.
	next = cardPattern.ReplaceAllString(out, "[REDACTED_CARD]")
	changed = changed || next != out
	out = next

	next = phonePattern.ReplaceAllString(out, "[REDACTED_PHONE]")
	changed = changed || next != out
	out = next

	return out, changed
}

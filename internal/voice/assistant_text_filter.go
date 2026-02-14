package voice

import (
	"regexp"
	"strings"
	"unicode"
)

const leadFillerProbeMaxCanonicalLen = 96

var (
	assistantLeadAckPhrases = []string{
		"sure",
		"okay",
		"ok",
		"alright",
		"all right",
		"got it",
		"absolutely",
		"yes",
		"yep",
		"yeah",
		"certainly",
		"of course",
		"right",
		"well",
		"hmm",
		"mmhm",
		"mm hmm",
		"mm hmmm",
	}
	assistantLeadFillerPhrases = []string{
		"give me a second while i think",
		"give me a second to think",
		"give me a second",
		"give me just a second",
		"give me just a second to think",
		"just a sec",
		"one sec",
		"just a second",
		"one second",
		"give me a moment",
		"give me just a moment",
		"just a moment",
		"one moment",
		"hold on",
		"hang on",
		"let me think for a second",
		"let me think for a moment",
		"let me think",
		"while i think",
	}
	assistantLeadAckRe    = regexp.MustCompile(`(?is)^\s*(?:sure|okay|ok|alright|all right|got it|absolutely|yes|yep|yeah|certainly|of course|right|well|hmm|mmhm|mm\s*hmm+)(?:(?:\s*[\p{P}]+\s*)+|\s+$|$)`)
	assistantLeadFillerRe = regexp.MustCompile(`(?is)^\s*(?:give me(?: just)? a (?:second|sec)(?: while i think| to think)?|just a (?:second|sec)|one (?:second|sec)(?: while i think)?|give me(?: just)? a moment(?: while i think| to think)?|just a moment|one moment|hold on|hang on|let me think(?: for a (?:second|moment))?|while i think)(?:(?:\s*[\p{P}]+\s*)+|\s+$|$)`)
)

type leadResponseFilter struct {
	committed bool
	buffer    string
}

func newLeadResponseFilter() *leadResponseFilter {
	return &leadResponseFilter{}
}

func (f *leadResponseFilter) Consume(delta string) string {
	if delta == "" {
		return ""
	}
	if f.committed {
		return delta
	}

	f.buffer += delta
	canon := canonicalizeForLeadFiller(f.buffer)
	if shouldHoldLeadBuffer(canon) && len(canon) < leadFillerProbeMaxCanonicalLen {
		return ""
	}

	f.buffer = stripAssistantLeadPreamble(f.buffer)
	canon = canonicalizeForLeadFiller(f.buffer)
	if canon == "" {
		return ""
	}
	if shouldHoldLeadBuffer(canon) && len(canon) < leadFillerProbeMaxCanonicalLen {
		return ""
	}

	f.committed = true
	out := f.buffer
	f.buffer = ""
	return out
}

func (f *leadResponseFilter) Finalize(fallback string) string {
	if strings.TrimSpace(fallback) != "" {
		return strings.TrimSpace(stripAssistantLeadPreamble(fallback))
	}
	if f.committed {
		return strings.TrimSpace(f.buffer)
	}
	return strings.TrimSpace(stripAssistantLeadPreamble(f.buffer))
}

func stripAssistantLeadFiller(raw string) string {
	out := raw
	for i := 0; i < 4; i++ {
		next := assistantLeadFillerRe.ReplaceAllString(out, "")
		if next == out {
			return out
		}
		out = next
	}
	return out
}

func stripAssistantLeadPreamble(raw string) string {
	out := raw
	for i := 0; i < 4; i++ {
		next := stripAssistantLeadFiller(out)
		if stripped, changed := stripAssistantLeadAckThenFiller(next); changed {
			next = stripped
		}
		if next == out {
			return out
		}
		out = next
	}
	return out
}

func stripAssistantLeadAckThenFiller(raw string) (string, bool) {
	m := assistantLeadAckRe.FindStringIndex(raw)
	if len(m) != 2 || m[0] != 0 {
		return raw, false
	}
	rest := raw[m[1]:]
	stripped := stripAssistantLeadFiller(rest)
	if stripped == rest {
		return raw, false
	}
	return stripped, true
}

func canonicalizeForLeadFiller(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))

	prevSpace := true
	for _, r := range strings.ToLower(raw) {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			b.WriteRune(r)
			prevSpace = false
		case unicode.IsSpace(r) || unicode.IsPunct(r):
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
		default:
			// Ignore symbols/emoji for matching.
		}
	}
	return strings.TrimSpace(b.String())
}

func shouldHoldLeadBuffer(canon string) bool {
	canon = strings.TrimSpace(canon)
	if canon == "" {
		return false
	}
	if isAssistantLeadFillerPrefix(canon) || isAssistantLeadAckPrefix(canon) {
		return true
	}
	rest, ok := trimAssistantLeadAckCanonical(canon)
	if !ok {
		return false
	}
	if rest == "" {
		return true
	}
	return isAssistantLeadFillerPrefix(rest)
}

func isAssistantLeadFillerPrefix(canon string) bool {
	canon = strings.TrimSpace(canon)
	if canon == "" {
		return false
	}
	for _, phrase := range assistantLeadFillerPhrases {
		if strings.HasPrefix(phrase, canon) {
			return true
		}
	}
	return false
}

func isAssistantLeadAckPrefix(canon string) bool {
	canon = strings.TrimSpace(canon)
	if canon == "" {
		return false
	}
	for _, phrase := range assistantLeadAckPhrases {
		if strings.HasPrefix(phrase, canon) {
			return true
		}
	}
	return false
}

func trimAssistantLeadAckCanonical(canon string) (string, bool) {
	canon = strings.TrimSpace(canon)
	if canon == "" {
		return "", false
	}
	for _, phrase := range assistantLeadAckPhrases {
		if canon == phrase {
			return "", true
		}
		if strings.HasPrefix(canon, phrase+" ") {
			return strings.TrimSpace(canon[len(phrase):]), true
		}
	}
	return "", false
}

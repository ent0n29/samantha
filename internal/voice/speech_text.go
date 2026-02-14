package voice

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	speechURLPattern          = regexp.MustCompile(`https?://\S+`)
	speechFencedCodePattern   = regexp.MustCompile("(?s)```.*?```")
	speechInlineCodePattern   = regexp.MustCompile("`[^`]*`")
	speechMarkdownLinkPattern = regexp.MustCompile(`\[(.*?)\]\((.*?)\)`)
)

type speechSanitizer struct {
	inFence bool
}

func newSpeechSanitizer() *speechSanitizer { return &speechSanitizer{} }

// SanitizeDelta removes code-fence blocks across streamed deltas before applying
// sanitizeSpeechText. This prevents TTS from reading backticks, symbols, and code.
func (s *speechSanitizer) SanitizeDelta(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	if s == nil {
		return sanitizeSpeechText(raw)
	}

	var b strings.Builder
	b.Grow(len(raw))

	for i := 0; i < len(raw); i++ {
		if raw[i] != '`' {
			if !s.inFence {
				b.WriteByte(raw[i])
			}
			continue
		}

		j := i
		for j < len(raw) && raw[j] == '`' {
			j++
		}
		count := j - i
		i = j - 1

		if count >= 3 {
			s.inFence = !s.inFence
			continue
		}

		// Drop single/double backticks (inline code markers).
	}

	return sanitizeSpeechText(b.String())
}

// sanitizeSpeechText removes markup/symbol noise from model text so TTS sounds conversational.
func sanitizeSpeechText(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	raw = speechFencedCodePattern.ReplaceAllString(raw, " ")
	raw = speechInlineCodePattern.ReplaceAllString(raw, " ")
	raw = speechMarkdownLinkPattern.ReplaceAllString(raw, "$1")
	raw = speechURLPattern.ReplaceAllString(raw, " ")

	raw = strings.NewReplacer(
		"*", " ",
		"_", " ",
		"\\", " ",
		"/", " ",
		"|", " ",
		"#", " ",
		"~", " ",
		"<", " ",
		">", " ",
	).Replace(raw)

	var b strings.Builder
	b.Grow(len(raw))
	prevSpace := true

	for _, r := range raw {
		switch {
		case r == '\u200d' || r == '\ufe0f' || r == '\u20e3':
			continue
		case r == '\n' || r == '\r' || r == '\t' || unicode.IsSpace(r):
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
		case unicode.IsControl(r):
			continue
		case unicode.In(r, unicode.So, unicode.Sm, unicode.Sk):
			// Drops emoji and symbol-heavy glyphs that sound unnatural when spoken.
			continue
		case isSpeechSafePunctuation(r):
			b.WriteRune(r)
			prevSpace = false
		case unicode.IsPunct(r):
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
		default:
			b.WriteRune(r)
			prevSpace = false
		}
	}

	return strings.TrimSpace(b.String())
}

// bridgeSpeechDelta restores an intentional leading space removed by sanitizeSpeechText
// when a streamed text delta begins with whitespace and continues a word sequence.
func bridgeSpeechDelta(rawDelta, sanitized string, alreadySent bool) string {
	if !alreadySent || sanitized == "" {
		return sanitized
	}
	firstRaw, ok := firstRune(rawDelta)
	if !ok || !unicode.IsSpace(firstRaw) {
		return sanitized
	}
	firstClean, ok := firstRune(sanitized)
	if !ok {
		return sanitized
	}
	if unicode.IsLetter(firstClean) || unicode.IsDigit(firstClean) {
		return " " + sanitized
	}
	return sanitized
}

func firstRune(s string) (rune, bool) {
	for _, r := range s {
		return r, true
	}
	return 0, false
}

func isSpeechSafePunctuation(r rune) bool {
	switch r {
	// Keep only punctuation that sounds natural when spoken. Many TTS engines
	// will literally say "colon" / "open parenthesis" etc, which breaks flow.
	case '.', ',', '!', '?', '\'', '"', '-':
		return true
	default:
		return false
	}
}

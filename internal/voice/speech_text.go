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

func isSpeechSafePunctuation(r rune) bool {
	switch r {
	case '.', ',', '!', '?', ':', ';', '\'', '"', '-', '(', ')':
		return true
	default:
		return false
	}
}

package voice

import "testing"

func TestSanitizeSpeechText_StripsEmojiAndSymbolNoise(t *testing.T) {
	in := "Hello 😊 / test *bold* (paren): ok"
	got := sanitizeSpeechText(in)
	want := "Hello test bold paren ok"
	if got != want {
		t.Fatalf("sanitizeSpeechText() = %q, want %q", got, want)
	}
}

func TestSanitizeSpeechText_StripsURLsAndMarkdownLinks(t *testing.T) {
	in := "See [this link](https://example.com/path?q=1) and also https://example.com/other."
	got := sanitizeSpeechText(in)
	want := "See this link and also"
	if got != want {
		t.Fatalf("sanitizeSpeechText() = %q, want %q", got, want)
	}
}

func TestSpeechSanitizer_StripsFencedCodeAcrossDeltas(t *testing.T) {
	s := newSpeechSanitizer()
	a := s.SanitizeDelta("Here is the command:\n```bash\nrm -rf /tmp\n")
	b := s.SanitizeDelta("```\nDone.\n")
	got := a
	if got != "" && b != "" {
		got = got + " " + b
	} else {
		got = got + b
	}
	got = sanitizeSpeechText(got)
	want := "Here is the command Done."
	if got != want {
		t.Fatalf("speechSanitizer combined = %q, want %q", got, want)
	}
}

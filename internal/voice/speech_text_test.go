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

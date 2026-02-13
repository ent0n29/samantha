package voice

import "testing"

func TestSanitizeSpeechText(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "drops emoji and markdown markers",
			in:   "Sure 😊 **let's** do this / now.",
			want: "Sure let's do this now.",
		},
		{
			name: "keeps markdown link label and removes url",
			in:   "Read [the docs](https://example.com/docs) first.",
			want: "Read the docs first.",
		},
		{
			name: "removes code blocks and inline code",
			in:   "```bash\nnpm run dev\n```\nThen run `make test` ✅",
			want: "Then run",
		},
		{
			name: "normalizes odd punctuation spacing",
			in:   "Hello***world///again",
			want: "Hello world again",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeSpeechText(tc.in)
			if got != tc.want {
				t.Fatalf("sanitizeSpeechText(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestBridgeSpeechDelta(t *testing.T) {
	cases := []struct {
		name        string
		rawDelta    string
		sanitized   string
		alreadySent bool
		want        string
	}{
		{
			name:        "prepends space for continuing word chunk",
			rawDelta:    " world",
			sanitized:   "world",
			alreadySent: true,
			want:        " world",
		},
		{
			name:        "no prepend on first chunk",
			rawDelta:    " world",
			sanitized:   "world",
			alreadySent: false,
			want:        "world",
		},
		{
			name:        "no prepend when sanitized starts with punctuation",
			rawDelta:    " !",
			sanitized:   "!",
			alreadySent: true,
			want:        "!",
		},
		{
			name:        "no prepend when raw delta has no leading whitespace",
			rawDelta:    "world",
			sanitized:   "world",
			alreadySent: true,
			want:        "world",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := bridgeSpeechDelta(tc.rawDelta, tc.sanitized, tc.alreadySent)
			if got != tc.want {
				t.Fatalf("bridgeSpeechDelta(%q, %q, %v) = %q, want %q", tc.rawDelta, tc.sanitized, tc.alreadySent, got, tc.want)
			}
		})
	}
}

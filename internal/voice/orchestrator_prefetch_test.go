package voice

import (
	"testing"
	"time"
)

func TestCanonicalIsProgressiveContinuation(t *testing.T) {
	cases := []struct {
		name string
		prev string
		next string
		want bool
	}{
		{
			name: "extends prior text",
			prev: "build api",
			next: "build api endpoint",
			want: true,
		},
		{
			name: "small rollback",
			prev: "build api endpoint",
			next: "build api endpoi",
			want: true,
		},
		{
			name: "large rollback",
			prev: "build api endpoint with tests",
			next: "build api",
			want: false,
		},
		{
			name: "different phrase",
			prev: "build api endpoint",
			next: "draft architecture doc",
			want: false,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := canonicalIsProgressiveContinuation(tc.prev, tc.next); got != tc.want {
				t.Fatalf("canonicalIsProgressiveContinuation(%q, %q) = %v, want %v", tc.prev, tc.next, got, tc.want)
			}
		})
	}
}

func TestShouldStartBrainPrefetchEarly(t *testing.T) {
	cases := []struct {
		name        string
		partialText string
		canonical   string
		utteranceMs int
		want        bool
	}{
		{
			name:        "long canonical starts early",
			partialText: "build api and add tests for auth middleware",
			canonical:   "build api and add tests for auth middleware",
			utteranceMs: 800,
			want:        true,
		},
		{
			name:        "terminal cue starts early",
			partialText: "ship this now.",
			canonical:   "ship this now",
			utteranceMs: 900,
			want:        true,
		},
		{
			name:        "four-word instruction starts early",
			partialText: "refactor this module today",
			canonical:   "refactor this module today",
			utteranceMs: 900,
			want:        true,
		},
		{
			name:        "age-based starts early",
			partialText: "we should compare both approaches",
			canonical:   "we should compare both approaches",
			utteranceMs: 2300,
			want:        true,
		},
		{
			name:        "short partial waits",
			partialText: "build",
			canonical:   "build",
			utteranceMs: 700,
			want:        false,
		},
		{
			name:        "two-word partial can start early",
			partialText: "build this",
			canonical:   "build this",
			utteranceMs: 700,
			want:        true,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldStartBrainPrefetchEarly(tc.partialText, tc.canonical, time.Duration(tc.utteranceMs)*time.Millisecond); got != tc.want {
				t.Fatalf("shouldStartBrainPrefetchEarly(%q, %q, %dms) = %v, want %v", tc.partialText, tc.canonical, tc.utteranceMs, got, tc.want)
			}
		})
	}
}

func TestBrainPrefetchCanonicalCompatible(t *testing.T) {
	cases := []struct {
		name      string
		prefetch  string
		committed string
		want      bool
	}{
		{
			name:      "exact match",
			prefetch:  "build api endpoint with auth",
			committed: "build api endpoint with auth",
			want:      true,
		},
		{
			name:      "progressive continuation match",
			prefetch:  "build api endpoint",
			committed: "build api endpoint with auth and tests",
			want:      true,
		},
		{
			name:      "tiny trailing correction still matches",
			prefetch:  "build api endpoint with auth middleware",
			committed: "build api endpoint with auth middlewares",
			want:      true,
		},
		{
			name:      "small unrelated tail rewrite does not match",
			prefetch:  "build api endpoint with auth middleware",
			committed: "build api endpoint for markdown parser",
			want:      false,
		},
		{
			name:      "short text is too risky for fuzzy match",
			prefetch:  "build api now",
			committed: "build api tomorrow",
			want:      false,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := brainPrefetchCanonicalCompatible(tc.prefetch, tc.committed); got != tc.want {
				t.Fatalf("brainPrefetchCanonicalCompatible(%q, %q) = %v, want %v", tc.prefetch, tc.committed, got, tc.want)
			}
		})
	}
}

func TestShouldKeepBrainPrefetchInFlight(t *testing.T) {
	cases := []struct {
		name     string
		inFlight string
		incoming string
		want     bool
	}{
		{
			name:     "exact canonical",
			inFlight: "build api endpoint",
			incoming: "build api endpoint",
			want:     true,
		},
		{
			name:     "incoming extends in-flight",
			inFlight: "build api",
			incoming: "build api endpoint with auth",
			want:     true,
		},
		{
			name:     "single-word prefix is too weak",
			inFlight: "build",
			incoming: "build api endpoint with auth",
			want:     false,
		},
		{
			name:     "incoming rewrites topic",
			inFlight: "build api endpoint",
			incoming: "design architecture diagram",
			want:     false,
		},
		{
			name:     "incoming rollback should restart",
			inFlight: "build api endpoint with auth middleware",
			incoming: "build api endpoint",
			want:     false,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldKeepBrainPrefetchInFlight(tc.inFlight, tc.incoming); got != tc.want {
				t.Fatalf("shouldKeepBrainPrefetchInFlight(%q, %q) = %v, want %v", tc.inFlight, tc.incoming, got, tc.want)
			}
		})
	}
}

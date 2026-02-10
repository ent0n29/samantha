package reliability

import (
	"testing"
	"time"
)

func TestIsRetryableHTTPStatus(t *testing.T) {
	cases := []struct {
		code int
		want bool
	}{
		{200, false},
		{400, false},
		{429, true},
		{500, true},
		{503, true},
	}
	for _, tc := range cases {
		got := IsRetryableHTTPStatus(tc.code)
		if got != tc.want {
			t.Fatalf("IsRetryableHTTPStatus(%d) = %v, want %v", tc.code, got, tc.want)
		}
	}
}

func TestExponentialBackoffCap(t *testing.T) {
	base := 100 * time.Millisecond
	capDur := 700 * time.Millisecond
	if got := ExponentialBackoff(0, base, capDur); got != base {
		t.Fatalf("attempt 0 = %v, want %v", got, base)
	}
	if got := ExponentialBackoff(10, base, capDur); got != capDur {
		t.Fatalf("attempt 10 = %v, want %v", got, capDur)
	}
}

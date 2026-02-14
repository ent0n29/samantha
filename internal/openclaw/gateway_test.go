package openclaw

import "testing"

func TestConnectSignatureStringV2(t *testing.T) {
	got := connectSignatureString(
		"dev1",
		"cli",
		"cli",
		"operator",
		[]string{"operator.read", "operator.write"},
		123,
		"tok",
		"nonce-1",
	)
	want := "v2|dev1|cli|cli|operator|operator.read,operator.write|123|tok|nonce-1"
	if got != want {
		t.Fatalf("connectSignatureString() = %q, want %q", got, want)
	}
}

func TestConnectSignatureStringV1(t *testing.T) {
	got := connectSignatureString(
		"dev1",
		"cli",
		"cli",
		"operator",
		[]string{"a", "b"},
		456,
		"",
		"",
	)
	want := "v1|dev1|cli|cli|operator|a,b|456|"
	if got != want {
		t.Fatalf("connectSignatureString() = %q, want %q", got, want)
	}
}

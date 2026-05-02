package apikey

import "testing"

func TestDisplayKeyPrefixPrefersHint(t *testing.T) {
	prefix := DisplayKeyPrefix(Key{
		KeyHint: "sk-abcd...wxyz",
		KeyHash: "1234567890abcdef",
	})
	if prefix != "sk-abcd...wxyz" {
		t.Fatalf("expected hint to be used, got %q", prefix)
	}
}

func TestParseExpiresAtRejectsInvalidFormat(t *testing.T) {
	value := "2026/04/02"
	_, _, err := parseExpiresAt(&value)
	if err != ErrInvalidExpiresAt {
		t.Fatalf("expected ErrInvalidExpiresAt, got %v", err)
	}
}

func TestParseExpiresAtClearsWhenEmpty(t *testing.T) {
	value := ""
	expiresAt, hasExpiresAt, err := parseExpiresAt(&value)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !hasExpiresAt {
		t.Fatal("expected expires_at to be marked for update")
	}
	if expiresAt != nil {
		t.Fatalf("expected nil expires_at, got %v", expiresAt)
	}
}

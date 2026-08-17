package audit

import "testing"

func TestSanitizeError(t *testing.T) {
	msg := "password=secret123 extra"
	got := sanitizeError("login", &msg)
	if got == nil || *got != "password=[redacted] extra" {
		t.Fatalf("got %#v", got)
	}
	got = sanitizeError("password_change", &msg)
	if got == nil || *got != "[redacted]" {
		t.Fatalf("got %#v", got)
	}
	if sanitizeError("login", nil) != nil {
		t.Fatal("nil")
	}
}

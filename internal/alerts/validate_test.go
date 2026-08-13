package alerts

import "testing"

func TestValidChannel(t *testing.T) {
	if err := validChannel("email", map[string]any{}); err == nil {
		t.Fatal("expected error")
	}
	if err := validChannel("webhook", map[string]any{"url": "https://example"}); err != nil {
		t.Fatal(err)
	}
	if err := validRuleType("nope"); err == nil {
		t.Fatal("expected error")
	}
}

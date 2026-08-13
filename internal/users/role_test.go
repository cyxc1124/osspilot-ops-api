package users

import "testing"

func TestNormalizeRole(t *testing.T) {
	got, err := normalizeRole([]string{"ops_operator", "platform_admin"})
	if err != nil || got != roleAdmin {
		t.Fatalf("got %q err %v", got, err)
	}
	got, err = normalizeRole([]string{"ops_operator"})
	if err != nil || got != roleOperator {
		t.Fatalf("got %q err %v", got, err)
	}
	got, err = normalizeRole(nil)
	if err != nil || got != "" {
		t.Fatalf("got %q err %v", got, err)
	}
	if _, err := normalizeRole([]string{"nope"}); err == nil {
		t.Fatal("expected error")
	}
}

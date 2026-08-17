package main

import "testing"

func TestValidatePassword(t *testing.T) {
	if _, err := validatePassword(""); err == nil {
		t.Fatal("empty")
	}
	if _, err := validatePassword("short"); err == nil {
		t.Fatal("short")
	}
	got, err := validatePassword("longenough")
	if err != nil || got != "longenough" {
		t.Fatalf("%q %v", got, err)
	}
}

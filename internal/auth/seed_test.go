package auth

import "testing"

// Keep in sync with migrations/00001_ops_users.sql
const seedAdminHash = "$2a$10$6apLvtRj9fP/MibiTA.VOexIIIPUtW5oeTiZA1BAD3tbSWZUEqPm2"

func TestSeedAdminHash(t *testing.T) {
	if !CheckPassword("admin", seedAdminHash) {
		t.Fatal("seed hash must verify password admin")
	}
}

func TestCheckCreatePassword(t *testing.T) {
	if got := CheckCreatePassword("short", "", true); got == "" {
		t.Fatal("short password")
	}
	if got := CheckCreatePassword("longenough", "", true); got != "" {
		t.Fatal(got)
	}
	if got := CheckCreatePassword("longenough", "otherpass", false); got != "passwords do not match" {
		t.Fatal(got)
	}
	if got := CheckCreatePassword("longenough", "longenough", false); got != "" {
		t.Fatal(got)
	}
}

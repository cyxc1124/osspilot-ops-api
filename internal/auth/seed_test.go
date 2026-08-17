package auth

import "testing"

// Keep in sync with migrations/00001_ops_users.sql
const seedAdminHash = "$2a$10$6apLvtRj9fP/MibiTA.VOexIIIPUtW5oeTiZA1BAD3tbSWZUEqPm2"

func TestSeedAdminHash(t *testing.T) {
	if !CheckPassword("admin", seedAdminHash) {
		t.Fatal("seed hash must verify password admin")
	}
}

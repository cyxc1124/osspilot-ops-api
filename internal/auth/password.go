package auth

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(b), nil
}

func CheckPassword(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func CheckCreatePassword(password, confirm string, mustChange bool) string {
	if len(password) < 8 || len(password) > 128 {
		return "password must be 8-128 characters"
	}
	if !mustChange && password != confirm {
		return "passwords do not match"
	}
	return ""
}

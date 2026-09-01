package auth

import (
	"strings"
	"testing"
)

func TestPasswordManagerHashesAndVerifiesWithoutExposingPlaintext(t *testing.T) {
	manager, err := NewPasswordManager()
	if err != nil {
		t.Fatalf("NewPasswordManager() error = %v", err)
	}
	password := "correct horse battery staple"
	hash, err := manager.Hash(password)
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	if hash == password || strings.Contains(hash, password) {
		t.Fatal("bcrypt hash exposed plaintext password")
	}
	if !manager.Verify(hash, password) {
		t.Fatal("Verify() rejected the correct password")
	}
	if manager.Verify(hash, "incorrect password") {
		t.Fatal("Verify() accepted an incorrect password")
	}
}

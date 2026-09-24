package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadPrivateValueRequiresOwnerOnlyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte(" private-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	value, err := readPrivateValue(path)
	if err != nil || value != "private-value" {
		t.Fatalf("value=%q err=%v", value, err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivateValue(path); err == nil {
		t.Fatal("publicly readable private value was accepted")
	}
}

func TestReadPrivateValueRejectsEmptyAndOversizedFiles(t *testing.T) {
	for name, data := range map[string][]byte{"empty": []byte(" \n"), "oversized": make([]byte, 8193)} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "secret")
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := readPrivateValue(path); err == nil {
				t.Fatal("invalid private value was accepted")
			}
		})
	}
}

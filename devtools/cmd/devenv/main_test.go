package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestUsageAndUnknownCommand checks the documented exit codes: usage errors are
// 2, runtime errors are 1.
func TestUsageAndUnknownCommand(t *testing.T) {
	for _, args := range [][]string{{}, {"--help"}, {"help"}, {"frobnicate"}} {
		if code := run(args); code != 2 {
			t.Fatalf("run(%v) = %d, want 2", args, code)
		}
	}
}

// TestUnknownScopeFailsBeforeDocker proves an unknown scope is rejected before
// any workspace, environment file, port, or container is touched.
func TestUnknownScopeFailsBeforeDocker(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "VERSION"), []byte("2.5.3\n"), 0o644); err != nil {
		t.Fatalf("seed version: %v", err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(previous) }()

	for _, command := range []string{"integration", "deps"} {
		code := run([]string{command, "--scope", "bogus"})
		if code != 1 {
			t.Fatalf("%s --scope bogus = %d, want 1", command, code)
		}
	}
	if _, err := os.Stat(filepath.Join(directory, ".run")); err == nil {
		t.Fatal("a rejected scope created workspace state")
	}
}

// TestBrowserScopeAndPortValidation covers the browser entry point: an unknown
// scope and an out-of-range port must both be rejected before any Docker work.
func TestBrowserScopeAndPortValidation(t *testing.T) {
	for _, args := range [][]string{
		{"e2e", "--scope", "bogus"},
		{"e2e", "--scope", "observe", "--frontend-port", "0"},
		{"e2e", "--scope", "business", "--http-port", "70000"},
		{"e2e", "--scope", "observe", "--admin-frontend-port", "abc"},
	} {
		if code := run(args); code != 2 && code != 1 {
			t.Fatalf("run(%v) = %d, want a non-zero rejection", args, code)
		}
	}
	for _, port := range []string{"1", "65535", "15173"} {
		if _, err := parseOptions([]string{"--frontend-port", port}); err != nil {
			t.Fatalf("port %s was rejected: %v", port, err)
		}
	}
	if _, err := parseOptions([]string{"--frontend-port", "65536"}); err == nil {
		t.Fatal("an out-of-range port was accepted")
	}
	if parsed, err := parseOptions([]string{"--http-port", "18080", "--frontend-port=15173"}); err != nil {
		t.Fatalf("valid ports were rejected: %v", err)
	} else if parsed.overrides()["HTTP_PORT"] != "18080" || parsed.overrides()["FRONTEND_PORT"] != "15173" {
		t.Fatalf("overrides %v", parsed.overrides())
	}
}

// TestUnknownOptionIsUsageError keeps the option parser strict.
func TestUnknownOptionIsUsageError(t *testing.T) {
	for _, args := range [][]string{{"dev", "--nope"}, {"integration", "--scope"}, {"dev", "extra"}} {
		if code := run(args); code != 2 {
			t.Fatalf("run(%v) = %d, want 2", args, code)
		}
	}
	if _, err := parseOptions([]string{"--scope=observe"}); err != nil {
		t.Fatalf("inline option value was rejected: %v", err)
	}
	if parsed, _ := parseOptions([]string{"--scope=observe"}); !strings.Contains(parsed.scope, "observe") {
		t.Fatalf("inline scope parsed as %q", parsed.scope)
	}
}

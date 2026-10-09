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

package frontenddeps

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Ray-ymq/GoPulse/devtools/internal/workspace"
)

func TestInputDigestIncludesBothPackageInputsAndRuntime(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"test"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "package-lock.json"), []byte(`{"lockfileVersion":3}`), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := InputDigest(root, "frontend", "v24.20.0", "11.19.0")
	if err != nil {
		t.Fatal(err)
	}
	if changed, _ := InputDigest(root, "frontend", "v24.20.1", "11.19.0"); changed == first {
		t.Fatal("node runtime changes must invalidate the digest")
	}
	if err := os.WriteFile(filepath.Join(root, "package-lock.json"), []byte(`{"lockfileVersion":4}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if changed, _ := InputDigest(root, "frontend", "v24.20.0", "11.19.0"); changed == first {
		t.Fatal("lockfile changes must invalidate the digest")
	}
}

func TestEnsureFailureDoesNotWriteSuccessMarker(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "frontend"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"package.json", "package-lock.json"} {
		if err := os.WriteFile(filepath.Join(root, "frontend", name), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	failed := errors.New("controlled npm failure")
	previousPath, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(previousPath) }()
	// This test uses the real node/npm version probes but never reaches npm.
	err = EnsureWithRunner(root, "frontend", func([]string, string, map[string]string) error { return failed })
	if !errors.Is(err, failed) {
		t.Fatalf("EnsureWithRunner error = %v, want controlled failure", err)
	}
	ws, err := workspace.For(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ws.PrivateRoot(), "frontenddeps", "frontend.json")); !os.IsNotExist(err) {
		t.Fatalf("failure wrote a success marker: %v", err)
	}
}

func TestUnknownApplicationIsRejected(t *testing.T) {
	if err := EnsureWithRunner(t.TempDir(), "web", func([]string, string, map[string]string) error { return nil }); err == nil {
		t.Fatal("unknown application was accepted")
	}
}

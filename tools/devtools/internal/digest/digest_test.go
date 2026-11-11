package digest

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDigestIsDeterministicAndChangeSensitive(t *testing.T) {
	root := t.TempDir()
	write(t, root, "backend/business/main.go", "package main\n")
	write(t, root, "backend/business/nested/file.go", "package nested\n")

	first, err := Paths(root, []string{filepath.Join(root, "backend/business")})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Paths(root, []string{"backend/business"})
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("the same tree must digest identically: %s != %s", first, second)
	}

	// A path change is a content change: build inputs are addressed by path too.
	if err := os.Rename(filepath.Join(root, "backend/business", "nested", "file.go"), filepath.Join(root, "backend/business", "nested", "renamed.go")); err != nil {
		t.Fatal(err)
	}
	renamed, err := Paths(root, []string{"backend/business"})
	if err != nil {
		t.Fatal(err)
	}
	if renamed == first {
		t.Error("renaming a file must change the digest")
	}

	write(t, root, "backend/business/main.go", "package main // changed\n")
	changed, err := Paths(root, []string{"backend/business"})
	if err != nil {
		t.Fatal(err)
	}
	if changed == renamed {
		t.Error("changing file content must change the digest")
	}
}

func TestDigestIgnoresGitDirectory(t *testing.T) {
	root := t.TempDir()
	write(t, root, "backend/business/main.go", "package main\n")
	before, err := Paths(root, []string{"backend/business"})
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "backend/business/.git/objects/aa/bb", "object")
	after, err := Paths(root, []string{"backend/business"})
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Error("a .git directory must not contribute to a source digest")
	}
}

func TestSourceDigestCoversLocalReplaceModulesAndScope(t *testing.T) {
	root := t.TempDir()
	write(t, root, "backend/business/go.mod", "module example\n\nreplace github.com/Ray-ymq/GoPulse/runtimekit => ../runtimekit\n")
	write(t, root, "backend/runtimekit/metrics.go", "package runtimekit\n")

	development, err := Source(root, false)
	if err != nil {
		t.Fatal(err)
	}

	// The replaced helper's regression: a locally replaced module is a build input.
	write(t, root, "backend/runtimekit/metrics.go", "package runtimekit // changed\n")
	changedDevelopment, err := Source(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if changedDevelopment == development {
		t.Error("a local replace module must contribute to the source digest")
	}

}

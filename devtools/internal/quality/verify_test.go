package quality

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func verificationFixture(t *testing.T, failure string) string {
	t.Helper()
	root := t.TempDir()
	makefile := `list-go-modules:
	@echo backend devtools
list-npm-modules:
	@echo frontend admin-frontend
ci-check ci-test ci-build:
	@echo $@ MODULE=$(MODULE) >> calls
`
	if failure != "" {
		makefile += "\t@test $@ != " + failure + "\n"
	}
	if err := os.WriteFile(filepath.Join(root, "Makefile"), []byte(makefile), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestVerifyModuleUsesTheThreeCIEntryPoints(t *testing.T) {
	root := verificationFixture(t, "")
	if err := VerifyModule(root, "backend", "unused-lint-baseline"); err != nil {
		t.Fatal(err)
	}
	calls, err := os.ReadFile(filepath.Join(root, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	want := "ci-check MODULE=backend\nci-test MODULE=backend\nci-build MODULE=backend\n"
	if string(calls) != want {
		t.Fatalf("commands = %q, want %q", calls, want)
	}
}

func TestVerifyModuleStopsOnFailedCheck(t *testing.T) {
	root := verificationFixture(t, "ci-test")
	if err := VerifyModule(root, "frontend", ""); err == nil {
		t.Fatal("expected the failing test to stop verification")
	}
	calls, err := os.ReadFile(filepath.Join(root, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(calls), "ci-build") {
		t.Fatalf("build ran after failed tests: %s", calls)
	}
}

func TestVerifyModuleRejectsUnknownModule(t *testing.T) {
	root := verificationFixture(t, "")
	if err := VerifyModule(root, "../unknown", ""); err == nil {
		t.Fatal("expected an unknown module error")
	}
	if _, err := os.Stat(filepath.Join(root, "calls")); !os.IsNotExist(err) {
		t.Fatal("unknown module must not run any check")
	}
}

func TestVerifyRequiresBaseRef(t *testing.T) {
	if err := Verify(t.TempDir(), "", false); err == nil {
		t.Fatal("expected a missing BASE_REF error")
	}
}

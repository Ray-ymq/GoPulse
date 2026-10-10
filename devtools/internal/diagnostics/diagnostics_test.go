package diagnostics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ray-ymq/GoPulse/devtools/internal/workspace"
)

func TestFailureReportSurvivesCleanupAndRedactsSecrets(t *testing.T) {
	root := t.TempDir()
	ws, err := workspace.For(root)
	if err != nil {
		t.Fatal(err)
	}
	report, err := New(ws, Metadata{
		Label: "integration-business", Project: "gopulse-test", Branch: "develop/2.5.6", Revision: "abc123",
		Version: "2.5.6", Mode: "integration", Scope: "business",
		Environment:  map[string]string{"workspace_id": ws.Identity, "workspace_root": ws.Root, "go": "go1.26.7"},
		Reproduction: [][]string{{"make", "integration", "SCOPE=business"}}, Secrets: []string{"super-secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := report.RecordCommand(Command{
		Label: "controlled failure", Args: []string{"sh", "-c", "printf token=super-secret; exit 7"}, Cwd: root,
		ExitCode: 7, Stdout: "token=super-secret\n", Stderr: "password=super-secret\n",
	}); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(root, "owned.log")
	if err := os.WriteFile(log, []byte("authorization=super-secret\nuseful failure\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := report.CopyText("backend", log); err != nil {
		t.Fatal(err)
	}
	report.AddCleanupError(os.ErrPermission)
	primary := &testError{"command failed"}
	if err := report.Finalize(primary, 7); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"failure.txt", "environment.txt", "reproduce.txt"} {
		if info, err := os.Stat(filepath.Join(report.Directory(), name)); err != nil {
			t.Fatal(err)
		} else if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %o, want 600", name, info.Mode().Perm())
		}
	}
	raw, err := os.ReadFile(filepath.Join(report.Directory(), "failure.txt"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "primary_exit_code: 7") || !strings.Contains(text, "cleanup_errors:") || !strings.Contains(text, "permission denied") {
		t.Fatalf("failure report does not preserve primary and cleanup details: %s", text)
	}
	for _, name := range []string{"failure.txt", "command-01-controlled-failure.txt", "logs/backend.txt", "reproduce.txt"} {
		raw, err := os.ReadFile(filepath.Join(report.Directory(), name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "super-secret") {
			t.Fatalf("secret leaked into %s: %s", name, raw)
		}
	}
}

func TestDiagnosticRejectsSymlinkSourceAndPrivatePath(t *testing.T) {
	root := t.TempDir()
	ws, err := workspace.For(root)
	if err != nil {
		t.Fatal(err)
	}
	report, err := New(ws, Metadata{Label: "symlink"})
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target.log")
	if err := os.WriteFile(target, []byte("no"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.log")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := report.CopyText("link", link); err == nil {
		t.Fatal("symlink source was accepted")
	}
	info, err := os.Stat(report.Directory())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("report directory mode = %o, want 700", info.Mode().Perm())
	}
}

func TestCopyTreeCreatesPrivateArtifactParents(t *testing.T) {
	root := t.TempDir()
	ws, err := workspace.For(root)
	if err != nil {
		t.Fatal(err)
	}
	report, err := New(ws, Metadata{Label: "browser-failure"})
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "test-results")
	if err := os.MkdirAll(filepath.Join(source, "case"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "case", "error-context.md"), []byte("failure"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := report.CopyTree("browser-report", source); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(report.Directory(), "artifacts", "browser-report", "case", "error-context.md")
	content, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "failure" {
		t.Fatalf("copied browser report = %q, want failure", content)
	}
}

type testError struct{ message string }

func (e *testError) Error() string { return e.message }

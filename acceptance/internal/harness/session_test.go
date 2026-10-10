package harness

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ray-ymq/GoPulse/acceptance/internal/contracts"
)

func TestGeneratedProjectAndEnvironmentStayOwned(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "VERSION"), []byte("2.5.5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env.example"), []byte("FOO=bar\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v (%s)", err, output)
	}
	command := exec.Command("git", "-C", root, "-c", "user.name=Acceptance Test", "-c", "user.email=acceptance@example.invalid", "commit", "--quiet", "--allow-empty", "-m", "test")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v (%s)", err, output)
	}
	session, err := New(root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Cleanup()
	if !contracts.ValidProjectName(session.Project) || session.Token == "" {
		t.Fatalf("invalid project: %+v", session)
	}
	if raw, err := os.ReadFile(session.EnvFile); err != nil {
		t.Fatal(err)
	} else if !strings.Contains(string(raw), "PUBLISHED_HOST=127.0.0.1\n") || !strings.Contains(string(raw), "GOPULSE_VERSION=2.5.5\n") {
		t.Fatalf("acceptance environment missing contract: %s", raw)
	} else if !strings.Contains(string(raw), "MARSHALLER_API_TOKEN=marshaller-") {
		t.Fatalf("acceptance environment missing Marshaller token: %s", raw)
	}
	values, err := parseEnv(session.EnvFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(values["MARSHALLER_API_TOKEN"]) < 32 {
		t.Fatalf("Marshaller API token length = %d", len(values["MARSHALLER_API_TOKEN"]))
	}
	if info, err := os.Stat(session.EnvFile); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o600 {
		t.Fatalf("environment mode = %o", info.Mode().Perm())
	}
}

func TestAcceptanceVersionIncrement(t *testing.T) {
	if got := nextVersion("2.5.9"); got != "2.5.10" {
		t.Fatalf("next version = %s", got)
	}
}

func TestPortConflictAndCommandFailureAreVisible(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	if err := CheckPortAvailable("127.0.0.1", port); err == nil {
		t.Fatalf("occupied port %d was accepted", port)
	}
	session := &Session{Root: t.TempDir()}
	result := session.Run("sh", "-c", "exit 7")
	if result.ExitCode != 7 || len(session.Commands) != 1 || session.Commands[0].ExitCode != 7 {
		t.Fatalf("failed command was not propagated: result=%+v commands=%+v", result, session.Commands)
	}
}

func TestKeepPreservesOwnedEnvironment(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "VERSION"), []byte("2.5.5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env.example"), []byte("FOO=bar\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v (%s)", err, output)
	}
	command := exec.Command("git", "-C", root, "-c", "user.name=Acceptance Test", "-c", "user.email=acceptance@example.invalid", "commit", "--quiet", "--allow-empty", "-m", "test")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v (%s)", err, output)
	}
	session, err := New(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(session.EnvFile); err != nil {
		t.Fatalf("--keep removed the owned environment: %v", err)
	}
	if err := os.RemoveAll(session.TempDir); err != nil {
		t.Fatal(err)
	}
}

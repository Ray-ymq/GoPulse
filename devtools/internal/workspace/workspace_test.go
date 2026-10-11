package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestIdentityIsPathScopedAndStable(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	one, err := For(first)
	if err != nil {
		t.Fatal(err)
	}
	again, err := For(first)
	if err != nil {
		t.Fatal(err)
	}
	other, err := For(second)
	if err != nil {
		t.Fatal(err)
	}
	if one.Identity != again.Identity {
		t.Errorf("the same path must keep one identity: %s != %s", one.Identity, again.Identity)
	}
	if one.Identity == other.Identity {
		t.Error("different checkout paths must not share an identity")
	}
	if !regexp.MustCompile(`^[0-9a-f]{12}$`).MatchString(one.Identity) {
		t.Errorf("identity must be 12 hex characters: %s", one.Identity)
	}
	if one.PrivateRoot() != filepath.Join(first, ".run", "local", one.Identity) {
		t.Errorf("private root = %s", one.PrivateRoot())
	}
}

func TestProjectNamesAreWorkspaceAndCandidateScoped(t *testing.T) {
	root := t.TempDir()
	checkout, err := For(root)
	if err != nil {
		t.Fatal(err)
	}
	if want := "gopulse-" + checkout.Identity + "-dev"; checkout.ProjectDev() != want {
		t.Errorf("development project = %s, want %s", checkout.ProjectDev(), want)
	}
	if err := os.WriteFile(filepath.Join(root, "VERSION"), []byte("2.5.2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if want := "gopulse-" + checkout.Identity + "-test-252"; checkout.ProjectTest() != want {
		t.Errorf("test project = %s, want %s", checkout.ProjectTest(), want)
	}
	if err := os.WriteFile(filepath.Join(root, "VERSION"), []byte("not-a-version\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(checkout.ProjectTest(), "-test-current") {
		t.Errorf("an unknown candidate must fall back to the current scope: %s", checkout.ProjectTest())
	}
}

func TestLoadRejectsStateOfAnotherWorkspace(t *testing.T) {
	root := t.TempDir()
	checkout, err := For(root)
	if err != nil {
		t.Fatal(err)
	}
	foreign := &State{Schema: Schema, Status: "running", WorkspaceRoot: "/somewhere/else", WorkspaceID: "000000000000"}
	if err := SaveJSON(checkout.StatePath(), foreign); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(checkout); err == nil {
		t.Fatal("a state document of another workspace must be refused")
	} else if !strings.Contains(err.Error(), "another workspace") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStateRoundTripKeepsContractFields(t *testing.T) {
	root := t.TempDir()
	checkout, err := For(root)
	if err != nil {
		t.Fatal(err)
	}
	state := &State{
		Schema: Schema, Status: "running", WorkspaceRoot: checkout.Root, WorkspaceID: checkout.Identity,
		Mode: "dev", Stage: StageDeps, Project: checkout.ProjectDev(), Observe: false,
		Processes: map[string]ProcessRecord{"backend": {PID: 42, Birth: "123", Log: "backend.log"}},
	}
	if err := Save(checkout, state); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(checkout.StatePath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("private state mode = %v, want 0600", info.Mode().Perm())
	}
	raw, err := os.ReadFile(checkout.StatePath())
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"schema", "status", "workspace_root", "workspace_id", "mode", "stage", "observe", "project", "processes", "logs"} {
		if _, found := document[key]; !found {
			t.Errorf("state must keep the %s field", key)
		}
	}
	loaded, err := Load(checkout)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Processes["backend"].PID != 42 || loaded.Stage != StageDeps {
		t.Errorf("state round trip lost data: %+v", loaded)
	}
}

func TestLoadReturnsNothingWithoutState(t *testing.T) {
	checkout, err := For(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	state, err := Load(checkout)
	if err != nil {
		t.Fatal(err)
	}
	if state != nil {
		t.Fatalf("expected no state, got %+v", state)
	}
}

// Package workspace owns the per-checkout identity, the private runtime root,
// the private lifecycle state document, and the Compose project names.
package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Schema is the private lifecycle state schema version.
const Schema = 1

var semver = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// Workspace identifies one checkout. Two checkouts (including two git
// worktrees) never share projects, volumes, ports, or private state.
type Workspace struct {
	Root     string
	Identity string
}

// For resolves the checkout root and derives its stable identity.
func For(root string) (Workspace, error) {
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return Workspace{}, err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return Workspace{}, err
	}
	sum := sha256.Sum256([]byte(resolved))
	return Workspace{Root: resolved, Identity: hex.EncodeToString(sum[:])[:12]}, nil
}

// PrivateRoot is the ignored per-workspace runtime directory.
func (w Workspace) PrivateRoot() string { return filepath.Join(w.Root, ".run", "local", w.Identity) }

// StatePath is the private lifecycle state document.
func (w Workspace) StatePath() string { return filepath.Join(w.PrivateRoot(), "state.json") }

// IntegrationStatePath is the isolated test lifecycle state document.
func (w Workspace) IntegrationStatePath() string {
	return filepath.Join(w.PrivateRoot(), "integration-state.json")
}

// LockPath is the exclusive test-lifecycle lock file.
func (w Workspace) LockPath() string { return filepath.Join(w.PrivateRoot(), "integration.lock") }

// BinRoot holds locally built source binaries and their build metadata.
func (w Workspace) BinRoot() string { return filepath.Join(w.PrivateRoot(), "bin") }

// LogRoot holds the owned child process logs.
func (w Workspace) LogRoot() string { return filepath.Join(w.PrivateRoot(), "logs") }

// ProjectDev is the development dependency Compose project.
func (w Workspace) ProjectDev() string { return "gopulse-" + w.Identity + "-dev" }

// ProjectTest is the isolated test Compose project. A Monitor image owns a
// compile-time release catalog, so test volumes stay candidate-scoped.
func (w Workspace) ProjectTest() string {
	version := "current"
	if raw, err := os.ReadFile(filepath.Join(w.Root, "VERSION")); err == nil {
		candidate := strings.TrimSpace(string(raw))
		if semver.MatchString(candidate) {
			version = strings.ReplaceAll(candidate, ".", "")
		}
	}
	return fmt.Sprintf("gopulse-%s-test-%s", w.Identity, version)
}

// ProcessRecord is one owned child process, identified by its birth token so a
// recycled PID can never be mistaken for the process this workspace started.
type ProcessRecord struct {
	Birth     string   `json:"birth"`
	Command   []string `json:"command"`
	Cwd       string   `json:"cwd"`
	Log       string   `json:"log"`
	PID       int      `json:"pid"`
	StartedAt string   `json:"started_at"`
}

// State mirrors the private lifecycle state contract. Field order follows the
// sorted JSON the replaced helper wrote, so old and new receipts stay diffable.
type State struct {
	Branch              string                   `json:"branch"`
	ComposeDigest       string                   `json:"compose_digest"`
	ComposeFiles        []string                 `json:"compose_files"`
	EnvFile             string                   `json:"env_file"`
	EnvironmentDigest   string                   `json:"environment_digest"`
	Logs                map[string]string        `json:"logs"`
	Mode                string                   `json:"mode"`
	MonitorImage        string                   `json:"monitor_image"`
	MonitorInputDigest  string                   `json:"monitor_input_digest"`
	Observe             bool                     `json:"observe"`
	PreparationCommands [][]string               `json:"preparation_commands"`
	Processes           map[string]ProcessRecord `json:"processes"`
	Project             string                   `json:"project"`
	Revision            string                   `json:"revision"`
	Schema              int                      `json:"schema"`
	SourceDigest        string                   `json:"source_digest"`
	Stage               string                   `json:"stage"`
	StartedAt           string                   `json:"started_at"`
	Status              string                   `json:"status"`
	StoppedAt           string                   `json:"stopped_at,omitempty"`
	WorkspaceID         string                   `json:"workspace_id"`
	WorkspaceRoot       string                   `json:"workspace_root"`
}

// Stage values distinguish a dependency-only lifecycle from a running
// application lifecycle so `deps` can be continued by `dev` or `dev-observe`.
const (
	StageDeps       = "deps"
	StageDev        = "dev"
	StageDevObserve = "dev-observe"
)

// Load reads the private state and rejects a document that belongs to another
// workspace.
func Load(w Workspace) (*State, error) {
	raw, err := os.ReadFile(w.StatePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("cannot read private lifecycle state: %w", err)
	}
	var state State
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("cannot read private lifecycle state: %w", err)
	}
	if state.WorkspaceRoot != w.Root || state.WorkspaceID != w.Identity {
		return nil, fmt.Errorf("private lifecycle state belongs to another workspace")
	}
	return &state, nil
}

// SaveJSON writes an indented, sorted, 0600 private document.
func SaveJSON(path string, document any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

// Save persists the lifecycle state.
func Save(w Workspace, state *State) error { return SaveJSON(w.StatePath(), state) }

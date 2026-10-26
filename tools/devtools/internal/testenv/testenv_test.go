package testenv

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Ray-ymq/GoPulse/devtools/internal/envfile"
	"github.com/Ray-ymq/GoPulse/devtools/internal/workspace"
)

var integrationStateKeys = []string{
	"branch", "container_network", "containers", "env_file", "logs", "mode", "observe", "processes",
	"project", "revision", "schema", "scope", "source_digest", "started_at", "status", "stopped_at",
	"workspace_id", "workspace_root",
}

func TestScopeForRejectsUnknownScope(t *testing.T) {
	observe, err := scopeFor("bogus", "integration")
	if err == nil {
		t.Fatalf("bogus scope was accepted (observe=%v)", observe)
	}
	want := "unknown integration SCOPE='bogus'; expected business or observe"
	if err.Error() != want {
		t.Fatalf("message %q, want %q", err.Error(), want)
	}
	for scope, expected := range map[string]bool{ScopeBusiness: false, ScopeObserve: true} {
		got, err := scopeFor(scope, "integration")
		if err != nil || got != expected {
			t.Fatalf("scope %s resolved to %v (%v), want %v", scope, got, err, expected)
		}
	}
}

func TestReservePortsUsesEphemeralHostPorts(t *testing.T) {
	ports := reservePorts([]string{"HTTP_PORT", "MONITOR_HTTP_PORT", "MARSHALLER_HTTP_PORT"})
	if len(ports) != 3 {
		t.Fatalf("reserved ports = %v, want three dynamic ports", ports)
	}
	for name, port := range ports {
		if port < 1 || port > 65535 {
			t.Fatalf("%s received invalid dynamic port %d", name, port)
		}
		if port == 18080 || port == 19090 || port == 19093 {
			t.Fatalf("%s retained a legacy fixed integration port %d", name, port)
		}
	}
}

func TestIntegrationLockIsExclusive(t *testing.T) {
	current := testSession(t)
	release, err := current.lock()
	if err != nil {
		t.Fatalf("first lock failed: %v", err)
	}
	second := testSessionFor(t, current.root, current.ws)
	if _, err := second.lock(); err == nil {
		t.Fatal("a second lock was granted while the first was held")
	} else if err.Error() != "another integration check owns the test environment lock" {
		t.Fatalf("lock message %q", err.Error())
	}
	release()
	again, err := second.lock()
	if err != nil {
		t.Fatalf("lock was not released: %v", err)
	}
	again()
}

func TestIntegrationFailurePropagatesAndCleansUp(t *testing.T) {
	current := testSession(t)
	dependenciesStopped := 0
	current.startDependencies = func(map[string]string) error { return nil }
	current.stopDependencies = func(*workspace.State) { dependenciesStopped++ }
	failure := errors.New("integration tests failed with exit code 1")
	current.runTestSuite = func(*workspace.State, string, bool) error { return failure }

	err := runIntegration(current, ScopeBusiness, false)
	if !errors.Is(err, failure) {
		t.Fatalf("integration error %v, want the test failure to propagate", err)
	}
	if dependenciesStopped != 1 {
		t.Fatalf("dependency cleanup count=%d, want 1", dependenciesStopped)
	}
	document := readState(t, current)
	if document["status"] != "failed" {
		t.Fatalf("status %v, want failed", document["status"])
	}
	if document["stopped_at"] == "" || document["stopped_at"] == nil {
		t.Fatal("cleanup did not record stopped_at")
	}
	if current.diagnostic == nil {
		t.Fatal("failed integration did not create a diagnostic report")
	}
	failureRaw, readErr := os.ReadFile(filepath.Join(current.diagnostic.Directory(), "failure.txt"))
	if readErr != nil {
		t.Fatalf("read diagnostic failure report: %v", readErr)
	}
	if !strings.Contains(string(failureRaw), "integration tests failed with exit code 1") {
		t.Fatalf("diagnostic report lost primary failure: %s", failureRaw)
	}
	assertStateKeys(t, document)
}

func TestIntegrationSucceedsWithoutCleanupFailure(t *testing.T) {
	current := testSession(t)
	statusDuringRun := ""
	current.startDependencies = func(map[string]string) error { return nil }
	current.stopDependencies = func(*workspace.State) {}
	current.runTestSuite = func(*workspace.State, string, bool) error {
		statusDuringRun = readState(t, current)["status"].(string)
		return nil
	}
	if err := runIntegration(current, ScopeBusiness, false); err != nil {
		t.Fatalf("business integration returned %v", err)
	}
	if statusDuringRun != "running" {
		t.Fatalf("state status during the test command was %q, want running", statusDuringRun)
	}
	document := readState(t, current)
	if document["status"] != "passed" || document["observe"] != false {
		t.Fatalf("final state %v", document)
	}
	assertStateKeys(t, document)
}

func TestTestcontainersReaperMustRemainEnabled(t *testing.T) {
	t.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")
	_, err := startDependencyContainers(t.Context(), "test-run", false)
	if err == nil || !strings.Contains(err.Error(), "resource reaper is disabled") {
		t.Fatalf("disabled reaper was accepted: %v", err)
	}
}

func testSession(t *testing.T) *session {
	t.Helper()
	root := repositoryRoot(t)
	directory := t.TempDir()
	return testSessionFor(t, root, workspaceFor(t, directory))
}

func testSessionFor(t *testing.T, root string, ws workspace.Workspace) *session {
	t.Helper()
	envFile := filepath.Join(ws.PrivateRoot(), "test.env")
	return &session{
		root:       root,
		ws:         ws,
		values:     envfile.Values{"HTTP_PORT": "0", "MONITOR_API_TOKEN": "token"},
		processEnv: map[string]string{"PATH": os.Getenv("PATH")},
		envFile:    envFile,
		project:    ws.ProjectTest(),
	}
}

func workspaceFor(t *testing.T, directory string) workspace.Workspace {
	t.Helper()
	ws, err := workspace.For(directory)
	if err != nil {
		t.Fatalf("workspace.For(%s): %v", directory, err)
	}
	return ws
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "deploy", "compose.yaml")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("repository root not found")
		}
		directory = parent
	}
}

func readState(t *testing.T, current *session) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(current.ws.IntegrationStatePath())
	if err != nil {
		t.Fatalf("read integration state: %v", err)
	}
	document := map[string]any{}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("decode integration state: %v", err)
	}
	return document
}

func assertStateKeys(t *testing.T, document map[string]any) {
	t.Helper()
	keys := make([]string, 0, len(document))
	for key := range document {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	expected := append([]string{}, integrationStateKeys...)
	sort.Strings(expected)
	if strings.Join(keys, ",") != strings.Join(expected, ",") {
		t.Fatalf("state keys\n got %v\nwant %v", keys, expected)
	}
}

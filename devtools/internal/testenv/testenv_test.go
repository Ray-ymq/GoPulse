package testenv

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/Ray-ymq/GoPulse/devtools/internal/compose"
	"github.com/Ray-ymq/GoPulse/devtools/internal/envfile"
	"github.com/Ray-ymq/GoPulse/devtools/internal/workspace"
)

// integrationStateKeys is the document contract the replaced helper wrote.
var integrationStateKeys = []string{
	"branch", "compose_digest", "compose_files", "env_file", "logs", "mode", "monitor_image",
	"observe", "processes", "project", "revision", "schema", "scope", "source_digest",
	"started_at", "status", "stopped_at", "workspace_id", "workspace_root",
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

func TestIntegrationPortsAreIsolatedPerScope(t *testing.T) {
	values := envfile.Values{
		"MYSQL_PORT": "23306", "REDIS_PORT": "26379", "RABBITMQ_PORT": "25672",
		"RABBITMQ_MANAGEMENT_PORT": "25673", "ELASTICSEARCH_PORT": "29200",
		"KAFKA_PORT": "19092", "VICTORIAMETRICS_PORT": "18428",
		"OBSERVABILITY_ELASTICSEARCH_PORT": "29201", "MONITOR_HTTP_PORT": "19090",
		"ROUTER_HTTP_PORT": "19091", "MARSHALLER_HTTP_PORT": "19093",
		"REDIS_EXPORTER_HTTP_PORT": "19121", "HTTP_PORT": "18080",
	}
	business := integrationPorts(values, false)
	wantBusiness := []int{23306, 26379, 25672, 25673, 29200}
	if !slices.Equal(business, wantBusiness) {
		t.Fatalf("business ports %v, want %v", business, wantBusiness)
	}
	observe := integrationPorts(values, true)
	for _, port := range []int{19092, 18428, 29201, 19090, 19091, 19093, 19121, 18080, 19101, 19102, 19103, 19105, 19106} {
		if !slices.Contains(observe, port) {
			t.Fatalf("observe ports %v do not cover %d", observe, port)
		}
	}
	if len(observe) != len(wantBusiness)+13 {
		t.Fatalf("observe ports %v do not extend the business set exactly once", observe)
	}
}

func TestE2EPortsIncludeSourceAndBrowserPorts(t *testing.T) {
	values := envfile.Values{
		"MYSQL_PORT": "23306", "REDIS_PORT": "26379", "RABBITMQ_PORT": "25672",
		"RABBITMQ_MANAGEMENT_PORT": "25673", "ELASTICSEARCH_PORT": "29200",
		"KAFKA_PORT": "19092", "VICTORIAMETRICS_PORT": "18428",
		"OBSERVABILITY_ELASTICSEARCH_PORT": "29201", "MONITOR_HTTP_PORT": "19090",
		"ROUTER_HTTP_PORT": "19091", "MARSHALLER_HTTP_PORT": "19093",
		"REDIS_EXPORTER_HTTP_PORT": "19121", "HTTP_PORT": "18080",
		"FRONTEND_PORT": "15173", "ADMIN_FRONTEND_PORT": "15174",
	}
	business := e2ePorts(values, false)
	for _, port := range []int{18080, 15173, 19101, 19102, 19103} {
		if !slices.Contains(business, port) {
			t.Fatalf("business browser ports %v do not cover %d", business, port)
		}
	}
	if slices.Contains(business, 15174) {
		t.Fatalf("business browser ports %v must not include the admin Vite port", business)
	}
	observe := e2ePorts(values, true)
	for _, port := range []int{15174, 19090, 19091, 19093, 19121} {
		if !slices.Contains(observe, port) {
			t.Fatalf("observe browser ports %v do not cover %d", observe, port)
		}
	}
}

func TestIntegrationLockIsExclusive(t *testing.T) {
	current := testSession(t, false)
	release, err := current.lock()
	if err != nil {
		t.Fatalf("first lock failed: %v", err)
	}
	second := testSessionFor(t, current.root, current.ws)
	if _, err := second.lock(); err == nil {
		t.Fatal("a second lock was granted while the first was held")
	} else if err.Error() != "another integration or browser check owns the test environment lock" {
		t.Fatalf("lock message %q", err.Error())
	}
	release()
	again, err := second.lock()
	if err != nil {
		t.Fatalf("lock was not released: %v", err)
	}
	again()
}

// TestIntegrationFailurePropagatesAndCleansUp covers the required unit case: a
// non-zero test command makes the helper fail, records status=failed, and still
// runs the cleanup path.
func TestIntegrationFailurePropagatesAndCleansUp(t *testing.T) {
	current := testSession(t, false)
	composeUp, composeDown := 0, 0
	started := make(chan struct{})
	current.startDependencies = func(map[string]string) error {
		composeUp++
		close(started)
		return nil
	}
	current.stopDependencies = func(*workspace.State) { composeDown++ }
	failure := errors.New("observability integration tests failed with exit code 1")
	current.runTestSuite = func(*workspace.State, string, bool) error {
		<-started
		return failure
	}

	err := runIntegration(current, ScopeBusiness, false)
	if !errors.Is(err, failure) {
		t.Fatalf("integration error %v, want the test failure to propagate", err)
	}
	if composeUp != 1 || composeDown != 1 {
		t.Fatalf("compose up=%d down=%d, want 1 and 1", composeUp, composeDown)
	}
	document := readState(t, current)
	if document["status"] != "failed" {
		t.Fatalf("status %v, want failed", document["status"])
	}
	if document["stopped_at"] == "" || document["stopped_at"] == nil {
		t.Fatal("cleanup did not record stopped_at")
	}
	if document["mode"] != "integration" || document["scope"] != ScopeBusiness {
		t.Fatalf("mode/scope %v/%v", document["mode"], document["scope"])
	}
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

// TestIntegrationSucceedsWithoutCleanupFailure proves the success path keeps the
// isolation contract: the state is written before the tests run and marked
// passed afterwards.
func TestIntegrationSucceedsWithoutCleanupFailure(t *testing.T) {
	current := testSession(t, true)
	statusDuringRun := ""
	current.startDependencies = func(map[string]string) error { return nil }
	current.stopDependencies = func(*workspace.State) {}
	current.runTestSuite = func(*workspace.State, string, bool) error {
		statusDuringRun = readState(t, current)["status"].(string)
		return nil
	}
	if err := runIntegration(current, ScopeObserve, true); err != nil {
		t.Fatalf("observe integration returned %v", err)
	}
	if statusDuringRun != "running" {
		t.Fatalf("state status during the test command was %q, want running", statusDuringRun)
	}
	document := readState(t, current)
	if document["status"] != "passed" || document["observe"] != true {
		t.Fatalf("final state %v", document)
	}
	if document["monitor_image"] == "" {
		t.Fatal("an observation scope must record the Monitor image tag")
	}
	if len(document["compose_files"].([]any)) != 2 {
		t.Fatalf("observe compose files %v", document["compose_files"])
	}
}

func testSession(t *testing.T, observe bool) *session {
	t.Helper()
	root := repositoryRoot(t)
	directory := t.TempDir()
	current := testSessionFor(t, root, workspaceFor(t, directory))
	if observe {
		current.files = append(current.files, filepath.Join(root, "deploy", "compose.local-linux.yaml"))
		current.profiles = []string{"observe"}
		current.command.Files = current.files
		current.command.Profiles = current.profiles
	}
	// The unit layer must never build or inspect an image.
	current.prepareMonitor = func(*session) (string, error) {
		return "gopulse/monitor:local-0123456789abcdef", nil
	}
	return current
}

func testSessionFor(t *testing.T, root string, ws workspace.Workspace) *session {
	t.Helper()
	envFile := filepath.Join(ws.PrivateRoot(), "test.env")
	files := []string{filepath.Join(root, "deploy", "compose.local.yaml")}
	project := ws.ProjectTest()
	return &session{
		root:       root,
		ws:         ws,
		values:     envfile.Values{"HTTP_PORT": "18080", "MONITOR_HTTP_PORT": "19090", "MONITOR_API_TOKEN": "token"},
		processEnv: map[string]string{"PATH": os.Getenv("PATH")},
		envFile:    envFile,
		project:    project,
		files:      files,
		command:    compose.Command{Project: project, EnvFile: envFile, Files: files},
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

// repositoryRoot resolves the checkout that owns this module so state digests
// run against real files while the workspace stays in a temporary directory.
func repositoryRoot(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "deploy", "compose.local.yaml")); err == nil {
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

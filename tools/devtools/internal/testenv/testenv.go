// Package testenv owns the isolated test lifecycles: a Testcontainers network
// with fresh dependency containers and dynamic ports, the source test
// processes, and deterministic test account cleanup.
package testenv

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Ray-ymq/GoPulse/devtools/internal/diagnostics"
	"github.com/Ray-ymq/GoPulse/devtools/internal/digest"
	"github.com/Ray-ymq/GoPulse/devtools/internal/envfile"
	"github.com/Ray-ymq/GoPulse/devtools/internal/proc"
	"github.com/Ray-ymq/GoPulse/devtools/internal/workspace"
)

// Scope names accepted by the test entries.
const (
	ScopeBusiness = "business"
	ScopeObserve  = "observe"
)

const (
	readinessTimeout           = 180 * time.Second
	businessIntegrationTimeout = 900 * time.Second
	observeIntegrationTimeout  = 390 * time.Second
)

// Environ is the calling process environment as a map.
func Environ() map[string]string {
	values := map[string]string{}
	for _, entry := range os.Environ() {
		if key, value, found := strings.Cut(entry, "="); found {
			values[key] = value
		}
	}
	return values
}

func now() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000000+00:00") }

type session struct {
	root            string
	ws              workspace.Workspace
	values          envfile.Values
	processEnv      map[string]string
	envFile         string
	project         string
	observationRoot string
	containers      *dependencyContainers
	context         context.Context
	// The three effectful steps stay overridable so the failure and cleanup
	// contract is covered without Docker.
	startDependencies      func(map[string]string) error
	stopDependencies       func(*workspace.State)
	runTestSuite           func(*workspace.State, string, bool) error
	stopDependenciesResult func(*workspace.State) error
	diagnostic             *diagnostics.Report
	diagnosticErrors       []error
	lastCommand            proc.Result
}

// newSession merges the test-mode environment contract and writes the private
// environment file. Dependency endpoints are filled after Testcontainers has
// allocated the run's dynamic ports.
func newSession(root string, observe bool, overrides map[string]string) (*session, error) {
	ws, err := workspace.For(root)
	if err != nil {
		return nil, err
	}
	values, err := envfile.Compose(ws.Root, envfile.ModeTest, "", Environ())
	if err != nil {
		return nil, err
	}
	// Explicit entry-point values win over the merged contract and reach the
	// private environment file, the children, and the recorded state alike.
	for key, value := range overrides {
		values[key] = value
	}
	values["MONITOR_PACKAGE_DIR"] = filepath.Join(ws.Root, ".run", "packages")
	envFile, err := envfile.WritePrivate(ws.PrivateRoot(), envfile.ModeTest, values)
	if err != nil {
		return nil, err
	}
	// Owned children receive the caller environment plus the merged contract so
	// that PATH, the Go build cache, and npm configuration keep working.
	processEnv := Environ()
	for key, value := range values {
		processEnv[key] = value
	}
	project := ws.ProjectTest()
	observationRoot := filepath.Join(ws.PrivateRoot(), fmt.Sprintf("integration-observe-%d-%d", os.Getpid(), time.Now().UnixNano()))
	current := &session{
		root:            ws.Root,
		ws:              ws,
		values:          values,
		processEnv:      processEnv,
		envFile:         envFile,
		project:         project,
		observationRoot: observationRoot,
		context:         context.Background(),
	}
	current.startDependencies = func(environment map[string]string) error {
		_ = environment
		return current.startContainers(observe)
	}
	current.stopDependenciesResult = current.stopContainers
	current.runTestSuite = func(state *workspace.State, scope string, observe bool) error {
		return current.runIntegrationTests(state, scope, observe)
	}
	return current, nil
}

func (s *session) startContainers(observe bool) error {
	manager, err := startDependencyContainers(s.context, s.ws.Identity, observe)
	if err != nil {
		return err
	}
	values, err := manager.values(s.context, observe)
	if err != nil {
		_ = manager.terminate(context.Background())
		return err
	}
	s.containers = manager
	for key, value := range values {
		s.values[key] = value
		s.processEnv[key] = value
	}
	envFile, err := envfile.WritePrivate(s.ws.PrivateRoot(), envfile.ModeTest, s.values)
	if err != nil {
		_ = manager.terminate(context.Background())
		s.containers = nil
		return err
	}
	s.envFile = envFile
	return nil
}

func (s *session) stopContainers(state *workspace.State) error {
	if s.containers == nil {
		if s.stopDependencies != nil {
			s.stopDependencies(state)
		}
		return nil
	}
	if s.diagnostic != nil {
		if err := s.containers.copyLogs(context.Background(), s.diagnostic.WriteText); err != nil {
			s.diagnosticErrors = append(s.diagnosticErrors, err)
		}
	}
	err := s.containers.terminate(context.Background())
	s.containers = nil
	return err
}

// lock takes the exclusive test environment lock without waiting.
func (s *session) lock() (func(), error) {
	if err := os.MkdirAll(s.ws.PrivateRoot(), 0o755); err != nil {
		return nil, err
	}
	handle, err := os.OpenFile(s.ws.LockPath(), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(handle.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		handle.Close()
		return nil, fmt.Errorf("another integration check owns the test environment lock")
	}
	return func() {
		_ = syscall.Flock(int(handle.Fd()), syscall.LOCK_UN)
		handle.Close()
	}, nil
}

// Integration runs one isolated integration scope against real dependencies.
func Integration(root, scope string) error {
	observe, err := scopeFor(scope, "integration")
	if err != nil {
		return err
	}
	current, err := newSession(root, observe, nil)
	if err != nil {
		return err
	}
	lifecycle, err := workspace.Load(current.ws)
	if err != nil {
		return err
	}
	if lifecycle != nil && lifecycle.Status == "running" {
		return fmt.Errorf("cannot start integration while this workspace owns a development lifecycle; run make stop first")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	current.context = ctx
	return runIntegration(current, scope, observe)
}

// runIntegration owns the locked test lifecycle: an isolated Testcontainers
// dependency set, the scope test command, and cleanup that runs on failure too.
func runIntegration(current *session, scope string, observe bool) error {
	release, err := current.lock()
	if err != nil {
		return err
	}
	defer release()

	current.beginDiagnostics("integration", scope, observe)
	if err := current.startDependencies(copyEnvironment(current.processEnv)); err != nil {
		primaryExit := current.lastCommand.ExitCode
		if primaryExit == 0 {
			primaryExit = -1
		}
		cleanupErrors := current.stopForFailure(current.pendingState())
		current.collectFailureEvidence(current.pendingState())
		current.finishDiagnosticsWithExit(err, primaryExit, cleanupErrors)
		return err
	}
	state, err := current.integrationState(scope, observe)
	if err != nil {
		primaryExit := current.lastCommand.ExitCode
		if primaryExit == 0 {
			primaryExit = -1
		}
		cleanupErrors := current.stopForFailure(current.pendingState())
		current.collectFailureEvidence(current.pendingState())
		current.finishDiagnosticsWithExit(err, primaryExit, cleanupErrors)
		return err
	}
	if err := current.saveIntegrationState(state); err != nil {
		primaryExit := current.lastCommand.ExitCode
		if primaryExit == 0 {
			primaryExit = -1
		}
		cleanupErrors := current.stopForFailure(state)
		current.collectFailureEvidence(state)
		current.finishDiagnosticsWithExit(err, primaryExit, cleanupErrors)
		return err
	}

	runErr := current.runTestSuite(state, scope, observe)
	primaryExit := current.lastCommand.ExitCode
	if runErr != nil && primaryExit == 0 {
		primaryExit = -1
	}
	if runErr != nil {
		state.Status = "failed"
		current.collectFailureEvidence(state)
	} else {
		state.Status = "passed"
		fmt.Printf("[gopulse] integration scope=%s passed for test project %s\n", scope, current.project)
	}
	cleanupErrors := current.cleanup(state, observe)
	if runErr != nil {
		current.finishDiagnosticsWithExit(runErr, primaryExit, cleanupErrors)
	}
	if runErr == nil && len(cleanupErrors) > 0 {
		return fmt.Errorf("integration cleanup failed: %s", joinErrors(cleanupErrors))
	}
	return runErr
}

// scopeFor validates a test scope name and returns whether it observes.
func scopeFor(scope, entry string) (bool, error) {
	switch scope {
	case ScopeBusiness:
		return false, nil
	case ScopeObserve:
		return true, nil
	default:
		return false, fmt.Errorf("unknown %s SCOPE='%s'; expected %s or %s", entry, scope, ScopeBusiness, ScopeObserve)
	}
}

// pendingState describes containers and a network that were created but not
// recorded, so a failed startup still removes exactly what it created.
func (s *session) pendingState() *workspace.State {
	state := &workspace.State{Project: s.project, EnvFile: s.envFile}
	if s.containers != nil {
		state.ContainerNetwork = s.containers.network.Name
		state.Containers = s.containers.records(context.Background())
	}
	return state
}

func (s *session) integrationState(scope string, observe bool) (*workspace.State, error) {
	source, err := digest.Source(s.root, observe)
	if err != nil {
		return nil, err
	}
	containerNetwork := ""
	containers := map[string]string{}
	if s.containers != nil {
		containerNetwork = s.containers.network.Name
		containers = s.containers.records(context.Background())
	}
	return &workspace.State{
		Schema:           workspace.Schema,
		Status:           "running",
		WorkspaceRoot:    s.ws.Root,
		WorkspaceID:      s.ws.Identity,
		Branch:           s.git("branch", "--show-current"),
		Revision:         s.git("rev-parse", "HEAD"),
		Mode:             "integration",
		Observe:          observe,
		Project:          s.project,
		EnvFile:          s.envFile,
		SourceDigest:     source,
		ContainerNetwork: containerNetwork,
		Containers:       containers,
		StartedAt:        now(),
		Processes:        map[string]workspace.ProcessRecord{},
		Logs:             map[string]string{},
		Scope:            scope,
	}, nil
}

func (s *session) git(args ...string) string {
	value, err := proc.Capture(append([]string{"git"}, args...), s.root, s.processEnv)
	if err != nil {
		return ""
	}
	return value
}

func (s *session) stopForFailure(state *workspace.State) []error {
	if s.stopDependenciesResult != nil {
		if err := s.stopDependenciesResult(state); err != nil {
			return []error{err}
		}
		return nil
	}
	if s.stopDependencies != nil {
		s.stopDependencies(state)
	}
	return nil
}

// saveIntegrationState records the Testcontainers resources and the host
// processes that belong to this integration run. Compose lifecycle fields are
// intentionally absent from this receipt.
func (s *session) saveIntegrationState(state *workspace.State) error {
	document := map[string]any{
		"schema":            state.Schema,
		"status":            state.Status,
		"workspace_root":    state.WorkspaceRoot,
		"workspace_id":      state.WorkspaceID,
		"branch":            state.Branch,
		"revision":          state.Revision,
		"mode":              state.Mode,
		"scope":             state.Scope,
		"observe":           state.Observe,
		"project":           state.Project,
		"container_network": state.ContainerNetwork,
		"containers":        state.Containers,
		"env_file":          state.EnvFile,
		"source_digest":     state.SourceDigest,
		"started_at":        state.StartedAt,
		"processes":         state.Processes,
		"logs":              state.Logs,
	}
	if state.StoppedAt != "" {
		document["stopped_at"] = state.StoppedAt
	}
	return workspace.SaveJSON(s.ws.IntegrationStatePath(), document)
}

func copyEnvironment(source map[string]string) map[string]string {
	target := make(map[string]string, len(source))
	for key, value := range source {
		target[key] = value
	}
	return target
}

func readMetadata(path string) map[string]any {
	metadata := map[string]any{}
	raw, err := os.ReadFile(path)
	if err != nil {
		return metadata
	}
	_ = json.Unmarshal(raw, &metadata)
	return metadata
}

func (s *session) runCommand(command []string, cwd string, environment map[string]string, label string, timeout time.Duration) error {
	result, err := proc.RunCapturedContext(s.context, command, cwd, environment, label, timeout)
	s.lastCommand = result
	if s.diagnostic != nil {
		if recordErr := s.diagnostic.RecordCommand(diagnostics.Command{
			Label: label, Args: result.Command, Cwd: result.Cwd, ExitCode: result.ExitCode,
			Stdout: result.Stdout, Stderr: result.Stderr, TimedOut: result.TimedOut,
		}); recordErr != nil {
			s.diagnosticErrors = append(s.diagnosticErrors, recordErr)
		}
	}
	return err
}

func (s *session) beginDiagnostics(mode, scope string, observe bool) {
	version := ""
	if raw, err := os.ReadFile(filepath.Join(s.root, "VERSION")); err == nil {
		version = strings.TrimSpace(string(raw))
	}
	secrets := []string{}
	for key, value := range s.values {
		upper := strings.ToUpper(key)
		if value != "" && (strings.Contains(upper, "PASSWORD") || strings.Contains(upper, "TOKEN") || strings.Contains(upper, "SECRET") || strings.Contains(upper, "CREDENTIAL") || strings.Contains(upper, "COOKIE")) {
			secrets = append(secrets, value)
		}
	}
	for key, value := range s.processEnv {
		upper := strings.ToUpper(key)
		if value != "" && (strings.Contains(upper, "PASSWORD") || strings.Contains(upper, "TOKEN") || strings.Contains(upper, "SECRET") || strings.Contains(upper, "CREDENTIAL") || strings.Contains(upper, "COOKIE")) {
			secrets = append(secrets, value)
		}
	}
	reproduction := [][]string{{"make", mode, "SCOPE=" + scope}}
	environment := map[string]string{
		"workspace_id":      s.ws.Identity,
		"workspace_root":    s.ws.Root,
		"container_network": "",
		"observe":           fmt.Sprintf("%t", observe),
	}
	if s.containers != nil {
		environment["container_network"] = s.containers.network.Name
	}
	report, err := diagnostics.New(s.ws, diagnostics.Metadata{
		Label: mode + "-" + scope, Project: s.project, Branch: s.git("branch", "--show-current"),
		Revision: s.git("rev-parse", "HEAD"), Version: version, Mode: mode, Scope: scope,
		Environment: environment, Reproduction: reproduction, Secrets: secrets,
	})
	if err != nil {
		s.diagnosticErrors = append(s.diagnosticErrors, err)
		return
	}
	s.diagnostic = report
}

func (s *session) collectFailureEvidence(state *workspace.State) {
	if s.diagnostic == nil {
		return
	}
	for name, record := range state.Processes {
		if record.Log == "" {
			continue
		}
		if err := s.diagnostic.CopyText("process-"+name, record.Log); err != nil {
			s.diagnosticErrors = append(s.diagnosticErrors, err)
		}
	}
}

func (s *session) reportDiagnosticErrors() {
	for _, err := range s.diagnosticErrors {
		fmt.Fprintf(os.Stderr, "[gopulse] warning: diagnostic evidence: %v\n", err)
	}
}

func (s *session) finishDiagnostics(primary error, state *workspace.State, cleanupErrors []error) {
	if primary == nil {
		return
	}
	if state != nil {
		s.collectFailureEvidence(state)
	}
	exitCode := s.lastCommand.ExitCode
	if exitCode == 0 {
		exitCode = -1
	}
	s.finishDiagnosticsWithExit(primary, exitCode, cleanupErrors)
}

func (s *session) finishDiagnosticsWithExit(primary error, exitCode int, cleanupErrors []error) {
	if s.diagnostic == nil {
		return
	}
	for _, cleanupErr := range cleanupErrors {
		s.diagnostic.AddCleanupError(cleanupErr)
	}
	if err := s.diagnostic.Finalize(primary, exitCode); err != nil {
		s.diagnosticErrors = append(s.diagnosticErrors, err)
	}
	s.reportDiagnosticErrors()
}

func joinErrors(errors []error) string {
	parts := make([]string, 0, len(errors))
	for _, err := range errors {
		if err != nil {
			parts = append(parts, err.Error())
		}
	}
	return strings.Join(parts, "; ")
}

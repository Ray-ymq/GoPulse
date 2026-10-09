// Package devrun owns the local development lifecycle: dependency startup,
// the source development environment, the observation environment, and the
// bounded stop that preserves named volumes.
package devrun

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Ray-ymq/GoPulse/devtools/internal/compose"
	"github.com/Ray-ymq/GoPulse/devtools/internal/digest"
	"github.com/Ray-ymq/GoPulse/devtools/internal/envfile"
	"github.com/Ray-ymq/GoPulse/devtools/internal/proc"
	"github.com/Ray-ymq/GoPulse/devtools/internal/workspace"
)

// Scope names accepted by the dependency entry point.
const (
	ScopeBusiness = "business"
	ScopeObserve  = "observe"
)

const readinessTimeout = 180 * time.Second

type request struct {
	stage       string
	mode        string
	observe     bool
	source      string
	compose     string
	environment string
}

type action int

const (
	actionStart action = iota
	actionAlreadyRunning
)

type session struct {
	root       string
	ws         workspace.Workspace
	values     envfile.Values
	processEnv map[string]string
	envFile    string
	project    string
	files      []string
	command    compose.Command
}

func now() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000000+00:00") }

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

// newSession merges the environment contract, writes the private environment
// file, and resolves the Compose command for one lifecycle mode.
func newSession(root, mode string, observe bool, explicitEnv string) (*session, error) {
	ws, err := workspace.For(root)
	if err != nil {
		return nil, err
	}
	values, err := envfile.Compose(ws.Root, mode, explicitEnv, Environ())
	if err != nil {
		return nil, err
	}
	envFile, err := envfile.WritePrivate(ws.PrivateRoot(), mode, values)
	if err != nil {
		return nil, err
	}
	// Every owned child and one-shot command receives the caller environment
	// plus the merged contract, so tooling that relies on PATH, the Go build
	// cache, or npm configuration keeps working.
	processEnv := Environ()
	for key, value := range values {
		processEnv[key] = value
	}
	files := []string{filepath.Join(ws.Root, "deploy", "compose.local.yaml")}
	profiles := []string{}
	if observe {
		files = append(files, filepath.Join(ws.Root, "deploy", "compose.local-linux.yaml"))
		profiles = append(profiles, "observe")
	}
	project := ws.ProjectDev()
	return &session{
		root:       ws.Root,
		ws:         ws,
		values:     values,
		processEnv: processEnv,
		envFile:    envFile,
		project:    project,
		files:      files,
		command:    compose.Command{Project: project, EnvFile: envFile, Files: files, Profiles: profiles},
	}, nil
}

// Deps starts or refreshes the development dependency project and records a
// dependency-only stage that `dev` or `dev-observe` can continue in place.
func Deps(root, scope, explicitEnv string) error {
	observe := false
	switch scope {
	case "", ScopeBusiness:
	case ScopeObserve:
		observe = true
	default:
		return fmt.Errorf("scope must be %s or %s (got %s)", ScopeBusiness, ScopeObserve, scope)
	}
	mode := envfile.ModeDev
	if observe {
		mode = envfile.ModeObserve
	}
	current, err := newSession(root, mode, observe, explicitEnv)
	if err != nil {
		return err
	}
	if observe {
		if _, err := current.ensureMonitorImage(); err != nil {
			return err
		}
	}
	env, err := current.buildEnvironment()
	if err != nil {
		return err
	}
	source, err := digest.Source(current.root, observe)
	if err != nil {
		return err
	}
	composeDigest, err := digest.Paths(current.root, current.files)
	if err != nil {
		return err
	}
	existing, err := workspace.Load(current.ws)
	if err != nil {
		return err
	}
	requested := request{stage: workspace.StageDeps, mode: mode, observe: observe,
		source: source, compose: composeDigest, environment: env.environmentDigest}
	outcome, err := resolveExisting(current, existing, requested)
	if err != nil {
		return err
	}
	if outcome == actionAlreadyRunning {
		fmt.Printf("[gopulse] dependencies are already running for workspace %s\n", current.ws.Identity)
		return nil
	}
	if err := current.command.Up(current.root, current.processEnv); err != nil {
		current.composeDown(existingOrFresh(current, requested))
		return err
	}
	state := current.baseState(env, requested)
	state.Processes = map[string]workspace.ProcessRecord{}
	state.Logs = map[string]string{}
	if err := workspace.Save(current.ws, state); err != nil {
		return err
	}
	fmt.Printf("[gopulse] dependencies are ready for workspace %s\n", current.ws.Identity)
	return nil
}

// Dev starts the source development lifecycle; observe adds the observation
// dependencies, Router, Marshaller, admin frontend, and Monitor container.
func Dev(root string, observe bool, explicitEnv string) error {
	return start(root, observe, explicitEnv)
}

func start(root string, observe bool, explicitEnv string) error {
	mode := envfile.ModeDev
	stage := workspace.StageDev
	if observe {
		mode = envfile.ModeObserve
		stage = workspace.StageDevObserve
	}
	current, err := newSession(root, mode, observe, explicitEnv)
	if err != nil {
		return err
	}
	source, err := digest.Source(current.root, observe)
	if err != nil {
		return err
	}
	env, err := current.buildEnvironment()
	if err != nil {
		return err
	}
	composeDigest, err := digest.Paths(current.root, current.files)
	if err != nil {
		return err
	}
	existing, err := workspace.Load(current.ws)
	if err != nil {
		return err
	}
	requested := request{stage: stage, mode: mode, observe: observe,
		source: source, compose: composeDigest, environment: env.environmentDigest}
	outcome, err := resolveExisting(current, existing, requested)
	if err != nil {
		return err
	}
	if outcome == actionAlreadyRunning {
		fmt.Printf("[gopulse] %s is already running for workspace %s\n", mode, current.ws.Identity)
		return nil
	}

	requiredPorts := []int{env.integer("HTTP_PORT"), env.integer("FRONTEND_PORT"), 19101, 19102, 19103}
	owned := []workspace.ProcessRecord{}
	if existing != nil && existing.Status == "running" {
		for _, record := range existing.Processes {
			owned = append(owned, record)
		}
	}
	if observe {
		requiredPorts = append(requiredPorts, env.integer("ROUTER_HTTP_PORT"), 19105,
			env.integer("MARSHALLER_HTTP_PORT"), 19106, env.integer("MONITOR_HTTP_PORT"),
			env.integer("ADMIN_FRONTEND_PORT"), env.integer("REDIS_EXPORTER_HTTP_PORT"))
	}
	if err := proc.CheckPorts(requiredPorts, owned); err != nil {
		return err
	}

	composeEnv := copyEnvironment(current.processEnv)
	if observe {
		image, err := current.ensureMonitorImage()
		if err != nil {
			return err
		}
		composeEnv["GOPULSE_MONITOR_IMAGE"] = image
	}
	if err := current.command.Up(current.root, composeEnv); err != nil {
		current.composeDown(existingOrFresh(current, requested))
		return err
	}

	state := current.baseState(env, requested)
	state.Processes = map[string]workspace.ProcessRecord{}
	state.Logs = map[string]string{}
	if err := workspace.Save(current.ws, state); err != nil {
		return err
	}
	if err := current.startApplications(state, observe, env); err != nil {
		stopState(current.ws, state, true)
		return err
	}
	fmt.Printf("[gopulse] %s is ready for workspace %s\n", mode, current.ws.Identity)
	return nil
}

// Stop terminates everything this workspace owns and preserves named volumes.
func Stop(root string) error {
	ws, err := workspace.For(root)
	if err != nil {
		return err
	}
	state, err := workspace.Load(ws)
	if err != nil {
		return err
	}
	if state == nil {
		fmt.Printf("[gopulse] no owned local lifecycle for workspace %s\n", ws.Identity)
		return nil
	}
	stopState(ws, state, true)
	fmt.Printf("[gopulse] stopped owned local lifecycle for workspace %s; named volumes were preserved\n", ws.Identity)
	return nil
}

// MonitorImage prepares or reuses the Monitor development image. The observe
// lifecycles call the same function on demand; the explicit entry point stays
// until its remaining callers migrate to those lifecycles.
func MonitorImage(root string) error {
	ws, err := workspace.For(root)
	if err != nil {
		return err
	}
	current := &session{root: ws.Root, ws: ws, processEnv: Environ(), values: envfile.Values{}}
	_, err = current.ensureMonitorImage()
	return err
}

// environment carries the resolved digests of one lifecycle start.
type environment struct {
	environmentDigest string
	token             map[string]string
}

func (e environment) integer(key string) int {
	value := 0
	fmt.Sscanf(e.token[key], "%d", &value)
	return value
}

func (s *session) buildEnvironment() (environment, error) {
	environmentDigest, err := digest.Paths(s.root, []string{s.envFile})
	if err != nil {
		return environment{}, err
	}
	if s.values["HTTP_PORT"] == "" || s.values["FRONTEND_PORT"] == "" {
		return environment{}, fmt.Errorf("HTTP_PORT and FRONTEND_PORT must be set")
	}
	return environment{environmentDigest: environmentDigest, token: s.values}, nil
}

func (s *session) baseState(env environment, requested request) *workspace.State {
	return &workspace.State{
		Schema:              workspace.Schema,
		Status:              "running",
		WorkspaceRoot:       s.ws.Root,
		WorkspaceID:         s.ws.Identity,
		Branch:              s.git("branch", "--show-current"),
		Revision:            s.git("rev-parse", "HEAD"),
		Mode:                requested.mode,
		Stage:               requested.stage,
		Observe:             requested.observe,
		Project:             s.project,
		ComposeFiles:        s.files,
		EnvFile:             s.envFile,
		SourceDigest:        requested.source,
		EnvironmentDigest:   env.environmentDigest,
		ComposeDigest:       requested.compose,
		MonitorInputDigest:  s.monitorInputDigest(requested.observe),
		MonitorImage:        s.monitorImage(requested.observe),
		PreparationCommands: [][]string{{"go", "run", "./cmd/migrate", "up"}, {"go", "run", "./cmd/search-reindex", "--if-missing"}},
		StartedAt:           now(),
		Processes:           map[string]workspace.ProcessRecord{},
		Logs:                map[string]string{},
	}
}

func (s *session) git(args ...string) string {
	value, err := proc.Capture(append([]string{"git"}, args...), s.root, s.processEnv)
	if err != nil {
		return ""
	}
	return value
}

func (s *session) monitorInputDigest(observe bool) string {
	if !observe {
		return ""
	}
	value, err := digest.MonitorInput(s.root)
	if err != nil {
		return ""
	}
	return value
}

func (s *session) monitorImage(observe bool) string {
	if !observe {
		return ""
	}
	return s.monitorImageTag()
}

func (s *session) monitorImageTag() string {
	input, err := digest.MonitorInput(s.root)
	if err != nil {
		return "gopulse/monitor:local"
	}
	if len(input) < 16 {
		return "gopulse/monitor:local"
	}
	return "gopulse/monitor:local-" + input[:16]
}

// ensureMonitorImage reuses the content-addressed Monitor image when it already
// exists, so an unchanged environment never rebuilds it, and otherwise builds
// it once.
func (s *session) ensureMonitorImage() (string, error) {
	if runtime.GOOS != "linux" {
		return "", fmt.Errorf("dev-observe requires Linux for the trusted host-network Monitor image")
	}
	tag := s.monitorImageTag()
	if proc.Succeeds([]string{"docker", "image", "inspect", tag}, s.root, s.processEnv) {
		fmt.Printf("[gopulse] reusing prepared Monitor image %s\n", tag)
		return tag, nil
	}
	version := strings.TrimSpace(s.readFile(filepath.Join(s.root, "VERSION")))
	if !semverLike(version) {
		return "", fmt.Errorf("VERSION must use major.minor.patch before building Monitor")
	}
	revision := s.git("rev-parse", "HEAD")
	input, err := digest.MonitorInput(s.root)
	if err != nil {
		return "", err
	}
	if err := proc.Run([]string{
		"docker", "build", "--pull=false", "--target", "monitor", "--file", "deploy/docker/observability.Dockerfile",
		"--build-arg", "VERSION=" + version, "--build-arg", "REVISION=" + revision, "--tag", tag, ".",
	}, s.root, s.processEnv, "Monitor image preparation", 0); err != nil {
		return "", err
	}
	if err := workspace.SaveJSON(filepath.Join(s.ws.PrivateRoot(), "monitor-image.json"), map[string]string{
		"tag": tag, "input_digest": input, "revision": revision, "version": version, "built_at": now(),
	}); err != nil {
		return "", err
	}
	fmt.Printf("[gopulse] prepared Monitor image %s\n", tag)
	return tag, nil
}

func semverLike(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, char := range part {
			if char < '0' || char > '9' {
				return false
			}
		}
	}
	return true
}

func (s *session) readFile(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(raw)
}

func existingOrFresh(s *session, requested request) *workspace.State {
	if state, err := workspace.Load(s.ws); err == nil && state != nil {
		return state
	}
	return &workspace.State{Project: s.project, EnvFile: s.envFile, ComposeFiles: s.files, Observe: requested.observe}
}

func (s *session) composeDown(state *workspace.State) {
	if state == nil {
		return
	}
	project := state.Project
	if project == "" {
		project = s.project
	}
	envFile := state.EnvFile
	if envFile == "" {
		envFile = s.envFile
	}
	files := state.ComposeFiles
	if len(files) == 0 {
		files = s.files
	}
	if project == "" || len(files) == 0 {
		return
	}
	if info, err := os.Stat(envFile); err != nil || info.IsDir() {
		return
	}
	env := Environ()
	for key, value := range s.values {
		env[key] = value
	}
	if state.MonitorImage != "" {
		env["GOPULSE_MONITOR_IMAGE"] = state.MonitorImage
	}
	command := compose.Command{Project: project, EnvFile: envFile, Files: files}
	if state.Observe {
		command.Profiles = []string{"observe"}
	}
	if err := command.Down(s.root, env); err != nil {
		compose.Warn(err)
	}
}

func stopState(ws workspace.Workspace, state *workspace.State, down bool) {
	names := make([]string, 0, len(state.Processes))
	for name := range state.Processes {
		names = append(names, name)
	}
	sortStrings(names)
	for _, name := range names {
		proc.Terminate(state.Processes[name])
	}
	if down {
		s := &session{root: ws.Root, ws: ws, values: envfile.Values{}, envFile: state.EnvFile, project: state.Project, files: state.ComposeFiles}
		s.composeDown(state)
	}
	state.Status = "stopped"
	state.StoppedAt = now()
	_ = workspace.Save(ws, state)
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

// resolveExisting decides whether a requested start may proceed, continue a
// dependency-only stage, or must be refused.
func resolveExisting(s *session, state *workspace.State, requested request) (action, error) {
	if state == nil || state.Status == "stopped" || state.Status == "failed" {
		return actionStart, nil
	}
	stage := state.Stage
	if stage == "" {
		// A state written by the replaced helper has no stage field.
		if state.Observe {
			stage = workspace.StageDevObserve
		} else {
			stage = workspace.StageDev
		}
	}
	if stage == workspace.StageDeps {
		for _, record := range state.Processes {
			if proc.Owned(record) {
				return actionStart, fmt.Errorf("owned local process state is stale; run make stop before restarting")
			}
		}
		if state.Project != s.project || state.EnvironmentDigest != requested.environment {
			return actionStart, fmt.Errorf("active local lifecycle inputs changed; run make stop before restarting")
		}
		if state.Observe && !requested.observe {
			return actionStart, fmt.Errorf("workspace already owns an active %s lifecycle; run make stop first", stage)
		}
		if requested.stage == workspace.StageDeps {
			return actionAlreadyRunning, nil
		}
		return actionStart, nil
	}
	if requested.stage == workspace.StageDeps {
		if state.Observe == requested.observe {
			return actionAlreadyRunning, nil
		}
		return actionStart, fmt.Errorf("workspace already owns an active %s lifecycle; run make stop first", stage)
	}
	if stage != requested.stage {
		return actionStart, fmt.Errorf("workspace already owns an active %s lifecycle; run make stop first", stage)
	}
	if state.SourceDigest != requested.source || state.ComposeDigest != requested.compose ||
		state.EnvironmentDigest != requested.environment {
		return actionStart, fmt.Errorf("active local lifecycle inputs changed; run make stop before restarting")
	}
	for _, record := range state.Processes {
		if !proc.Owned(record) {
			return actionStart, fmt.Errorf("owned local process state is stale or a process exited; run make stop before restarting")
		}
	}
	return actionAlreadyRunning, nil
}

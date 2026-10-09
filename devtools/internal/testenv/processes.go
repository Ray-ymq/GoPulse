package testenv

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Ray-ymq/GoPulse/devtools/internal/digest"
	"github.com/Ray-ymq/GoPulse/devtools/internal/proc"
	"github.com/Ray-ymq/GoPulse/devtools/internal/ready"
	"github.com/Ray-ymq/GoPulse/devtools/internal/workspace"
)

const (
	observeAdminPassword = "observe-admin-password-32-bytes-0123456789"
	// observeAdminUsername is the deterministic account the observation flow
	// registers and then removes again.
	observeAdminUsername = "observe_admin_%s"
)

var observeUsernamePattern = regexp.MustCompile(`^observe_(?:admin|user|demote)_[0-9a-f]+$`)

// runIntegrationTests executes the scope-specific test command. The caller marks
// the lifecycle failed and still cleans up when this returns an error.
func (s *session) runIntegrationTests(state *workspace.State, scope string, observe bool) error {
	if !observe {
		oneShot := s.testEnvironment()
		oneShot["LOG_MONITOR_URL"] = ""
		oneShot["LOG_MONITOR_INGEST_TOKEN"] = ""
		if err := proc.Run([]string{"go", "run", "./cmd/migrate", "up"},
			filepath.Join(s.root, "backend"), oneShot, "test database migration", 0); err != nil {
			return err
		}
		if err := proc.Run([]string{"go", "run", "./cmd/search-reindex", "--if-missing"},
			filepath.Join(s.root, "backend"), oneShot, "test search reindex", 0); err != nil {
			return err
		}
		environment := s.testEnvironment()
		environment["LOG_MONITOR_URL"] = ""
		environment["LOG_MONITOR_INGEST_TOKEN"] = ""
		return proc.Run([]string{"go", "-C", "backend", "test", "-p", "1", "-tags=integration", "./..."},
			s.root, environment, "business integration tests", businessIntegrationTimeout)
	}
	if err := s.startObservationProcesses(state); err != nil {
		return err
	}
	environment := s.testEnvironment()
	environment["OBSERVABILITY_INTEGRATION"] = "1"
	return proc.Run([]string{"go", "-C", "backend", "test", "-p", "1",
		"-tags=integration,observability_integration", "./internal/http",
		"-run", "^TestObservabilityFlowIntegration$", "-count=1", "-timeout", "6m"},
		s.root, environment, "observability integration tests", observeIntegrationTimeout)
}

// testEnvironment is the owned child environment plus the integration flag.
func (s *session) testEnvironment() map[string]string {
	environment := copyEnvironment(s.values)
	for key, value := range s.processEnv {
		if _, known := environment[key]; !known {
			environment[key] = value
		}
	}
	environment["INTEGRATION_TESTS"] = "1"
	return environment
}

// startObservationProcesses starts only the source services the native
// observation flow needs, then registers and promotes its test administrator.
func (s *session) startObservationProcesses(state *workspace.State) error {
	oneShot := s.testEnvironment()
	oneShot["LOG_MONITOR_URL"] = ""
	oneShot["LOG_MONITOR_INGEST_TOKEN"] = ""
	backendDir := filepath.Join(s.root, "backend")
	if err := proc.Run([]string{"go", "run", "./cmd/migrate", "up"}, backendDir, oneShot,
		"test database migration", 0); err != nil {
		return err
	}
	if err := proc.Run([]string{"go", "run", "./cmd/search-reindex", "--if-missing"}, backendDir, oneShot,
		"test search reindex", 0); err != nil {
		return err
	}
	buildDigest, err := digest.SourceBuilds(s.root)
	if err != nil {
		return err
	}
	observationDigest, err := digest.Source(s.root, true)
	if err != nil {
		return err
	}
	backend, err := s.buildBinary("integration-backend", backendDir, "./cmd/server", buildDigest)
	if err != nil {
		return err
	}
	router, err := s.buildBinary("integration-router", filepath.Join(s.root, "router"), "./cmd/router", observationDigest)
	if err != nil {
		return err
	}
	marshaller, err := s.buildBinary("integration-marshaller", filepath.Join(s.root, "marshaller"), "./cmd/marshaller", observationDigest)
	if err != nil {
		return err
	}
	adminRole, err := s.buildBinary("integration-admin-role", backendDir, "./cmd/admin-role", buildDigest)
	if err != nil {
		return err
	}

	routerEnv := copyEnvironment(s.values)
	routerEnv["GOPULSE_INSTANCE_ID"] = "router-test"
	marshallerEnv := copyEnvironment(s.values)
	marshallerEnv["GOPULSE_INSTANCE_ID"] = "marshaller-test"
	backendEnv := copyEnvironment(s.values)
	backendEnv["GOPULSE_INSTANCE_ID"] = "backend-test"

	if err := s.spawn(state, "router", []string{router}, routerEnv, filepath.Join(s.root, "router")); err != nil {
		return err
	}
	if err := ready.Wait(fmt.Sprintf("http://127.0.0.1:%s/ready", s.values["ROUTER_HTTP_PORT"]), readinessTimeout,
		s.values["ROUTER_API_TOKEN"], "test Router", aliveIn(state, "router")); err != nil {
		return err
	}
	if err := s.spawn(state, "marshaller", []string{marshaller}, marshallerEnv, filepath.Join(s.root, "marshaller")); err != nil {
		return err
	}
	if err := ready.Wait(fmt.Sprintf("http://127.0.0.1:%s/ready", s.values["MARSHALLER_HTTP_PORT"]), readinessTimeout,
		s.values["MARSHALLER_API_TOKEN"], "test Marshaller", aliveIn(state, "marshaller")); err != nil {
		return err
	}
	if err := s.spawn(state, "backend", []string{backend}, backendEnv, backendDir); err != nil {
		return err
	}
	if err := ready.Wait(fmt.Sprintf("http://127.0.0.1:%s/ready", s.values["HTTP_PORT"]), readinessTimeout,
		"", "test Backend", aliveIn(state, "backend")); err != nil {
		return err
	}
	if err := ready.Wait(fmt.Sprintf("http://127.0.0.1:%s/ready", s.values["MONITOR_HTTP_PORT"]), readinessTimeout,
		s.values["MONITOR_API_TOKEN"], "test Monitor", nil); err != nil {
		return err
	}

	username := fmt.Sprintf(observeAdminUsername, s.ws.Identity)
	s.values["OBSERVE_ADMIN_USERNAME"] = username
	s.values["OBSERVE_ADMIN_PASSWORD"] = observeAdminPassword
	if err := s.registerObservationAdmin(username, adminRole); err != nil {
		return err
	}
	return s.saveIntegrationState(state)
}

// registerObservationAdmin creates the deterministic administrator and promotes
// it through the real command the observation flow expects.
func (s *session) registerObservationAdmin(username, adminRole string) error {
	payload, err := json.Marshal(map[string]string{"username": username, "password": observeAdminPassword})
	if err != nil {
		return err
	}
	registration := string(payload)
	command := exec.Command("curl", "--silent", "--show-error", "--max-time", "20",
		"-H", "Content-Type: application/json", "-d", registration, "-o", os.DevNull, "-w", "%{http_code}",
		fmt.Sprintf("http://127.0.0.1:%s/api/v1/auth/register", s.values["HTTP_PORT"]))
	command.Dir = s.root
	command.Env = proc.Environment(s.testEnvironment())
	output, err := command.Output()
	if err != nil {
		return fmt.Errorf("test observability admin registration failed: %w", err)
	}
	switch strings.TrimSpace(string(output)) {
	case "201", "409":
	default:
		return fmt.Errorf("test observability admin registration failed")
	}
	return proc.Run([]string{adminRole, "promote", "--username", username},
		filepath.Join(s.root, "backend"), s.testEnvironment(), "test observability admin-role bootstrap", 0)
}

// cleanup terminates the owned test processes and removes only the accounts the
// observation flow created.
func (s *session) cleanup(state *workspace.State, observe bool) {
	for _, record := range state.Processes {
		proc.Terminate(record)
	}
	if observe {
		if err := s.cleanupObservationAccounts(state); err != nil {
			state.Status = "failed"
			fmt.Fprintf(os.Stderr, "[gopulse] warning: %v\n", err)
		}
	}
	s.stopDependencies(state)
	state.StoppedAt = now()
	if err := s.saveIntegrationState(state); err != nil {
		fmt.Fprintf(os.Stderr, "[gopulse] warning: %v\n", err)
	}
}

// observationUsernames is the deterministic set the browser and integration
// flows may create for this workspace.
func (s *session) observationUsernames() ([]string, error) {
	usernames := []string{}
	for _, key := range []string{"OBSERVE_ADMIN_USERNAME", "OBSERVE_USER_USERNAME", "OBSERVE_DEMOTION_USERNAME"} {
		if value := s.values[key]; value != "" {
			usernames = append(usernames, value)
		}
	}
	for _, username := range usernames {
		if !observeUsernamePattern.MatchString(username) {
			return nil, fmt.Errorf("refusing to clean an unexpected observability test username")
		}
	}
	return usernames, nil
}

// cleanupObservationAccounts deletes the related rows and then the test accounts
// themselves, in the order the replaced helper used.
func (s *session) cleanupObservationAccounts(state *workspace.State) error {
	usernames, err := s.observationUsernames()
	if err != nil || len(usernames) == 0 {
		return err
	}
	quoted := make([]string, 0, len(usernames))
	for _, username := range usernames {
		escaped := strings.ReplaceAll(username, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `'`, `\'`)
		quoted = append(quoted, "'"+escaped+"'")
	}
	list := strings.Join(quoted, ",")
	sql := strings.Join([]string{
		fmt.Sprintf("DELETE n FROM notifications AS n LEFT JOIN users AS recipient ON recipient.id=n.recipient_id LEFT JOIN users AS actor ON actor.id=n.actor_id LEFT JOIN posts AS post ON post.id=n.post_id LEFT JOIN users AS post_author ON post_author.id=post.author_id LEFT JOIN comments AS comment ON comment.id=n.comment_id LEFT JOIN users AS comment_author ON comment_author.id=comment.author_id WHERE recipient.username IN (%s) OR actor.username IN (%s) OR post_author.username IN (%s) OR comment_author.username IN (%s);", list, list, list, list),
		fmt.Sprintf("DELETE b FROM post_bookmarks AS b LEFT JOIN users AS bookmark_user ON bookmark_user.id=b.user_id LEFT JOIN posts AS post ON post.id=b.post_id LEFT JOIN users AS post_author ON post_author.id=post.author_id WHERE bookmark_user.username IN (%s) OR post_author.username IN (%s);", list, list),
		fmt.Sprintf("DELETE l FROM post_likes AS l LEFT JOIN users AS liker ON liker.id=l.user_id LEFT JOIN posts AS post ON post.id=l.post_id LEFT JOIN users AS post_author ON post_author.id=post.author_id WHERE liker.username IN (%s) OR post_author.username IN (%s);", list, list),
		fmt.Sprintf("DELETE comment FROM comments AS comment LEFT JOIN users AS comment_author ON comment_author.id=comment.author_id LEFT JOIN posts AS post ON post.id=comment.post_id LEFT JOIN users AS post_author ON post_author.id=post.author_id WHERE comment_author.username IN (%s) OR post_author.username IN (%s);", list, list),
		fmt.Sprintf("DELETE post FROM posts AS post INNER JOIN users AS post_author ON post_author.id=post.author_id WHERE post_author.username IN (%s);", list),
		fmt.Sprintf("DELETE FROM bootstrap_super_admin WHERE user_id IN (SELECT id FROM users WHERE username IN (%s));", list),
		fmt.Sprintf("DELETE FROM users WHERE username IN (%s);", list),
	}, " ")
	if state.Project == "" || state.EnvFile == "" || len(state.ComposeFiles) == 0 {
		return fmt.Errorf("cannot clean observability test administrator without Compose state")
	}
	arguments := []string{"exec", "-T", "mysql", "mysql", "-uroot",
		"-p" + s.values["MYSQL_ROOT_PASSWORD"], s.values["MYSQL_DATABASE"], "-e", sql}
	return s.command.Run(s.root, Environ(), "observability test administrator cleanup", 0, arguments...)
}

// buildBinary reuses a previously built binary while its source digest matches.
func (s *session) buildBinary(name, directory, pkg, sourceDigest string) (string, error) {
	binary := filepath.Join(s.ws.BinRoot(), name)
	metadata := filepath.Join(s.ws.BinRoot(), name+".json")
	if previous := readMetadata(metadata); previous["digest"] == sourceDigest {
		if info, err := os.Stat(binary); err == nil && !info.IsDir() {
			return binary, nil
		}
	}
	if err := os.MkdirAll(s.ws.BinRoot(), 0o755); err != nil {
		return "", err
	}
	command := []string{"go", "build", "-trimpath", "-o", binary, pkg}
	if err := proc.Run(command, directory, s.processEnv, "build "+name, 0); err != nil {
		return "", err
	}
	if err := workspace.SaveJSON(metadata, map[string]any{
		"name": name, "digest": sourceDigest, "command": command,
	}); err != nil {
		return "", err
	}
	return binary, nil
}

func (s *session) spawn(state *workspace.State, name string, command []string, environment map[string]string, cwd string) error {
	record, err := proc.Spawn(filepath.Join(s.ws.LogRoot(), name+".log"), cwd, command, environment, now())
	if err != nil {
		return err
	}
	if state.Processes == nil {
		state.Processes = map[string]workspace.ProcessRecord{}
	}
	state.Processes[name] = record
	return s.saveIntegrationState(state)
}

func aliveIn(state *workspace.State, name string) func() bool {
	return func() bool {
		record, found := state.Processes[name]
		return found && proc.Owned(record)
	}
}

// ensureMonitorImage prepares or reuses the content-addressed Monitor image so an
// observation test scope never depends on a manual preparation step.
func ensureMonitorImage(s *session) (string, error) {
	tag := monitorImageTag(s.root)
	if proc.Succeeds([]string{"docker", "image", "inspect", tag}, s.root, s.processEnv) {
		fmt.Printf("[gopulse] reusing prepared Monitor image %s\n", tag)
		return tag, nil
	}
	raw, err := os.ReadFile(filepath.Join(s.root, "VERSION"))
	if err != nil {
		return "", err
	}
	version := strings.TrimSpace(string(raw))
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

// monitorImageTag is the content-addressed tag both test scopes and the
// development observation lifecycle share.
func monitorImageTag(root string) string {
	input, err := digest.MonitorInput(root)
	if err != nil || len(input) < 16 {
		return "gopulse/monitor:local"
	}
	return "gopulse/monitor:local-" + input[:16]
}

func semverLike(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, character := range part {
			if character < '0' || character > '9' {
				return false
			}
		}
	}
	return true
}

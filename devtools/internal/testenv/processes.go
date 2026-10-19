package testenv

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/Ray-ymq/GoPulse/devtools/internal/digest"
	"github.com/Ray-ymq/GoPulse/devtools/internal/exporterpackages"
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
		if err := s.runCommand([]string{"go", "run", "./cmd/migrate", "up"},
			filepath.Join(s.root, "backend"), oneShot, "test database migration", 0); err != nil {
			return err
		}
		if err := s.runCommand([]string{"go", "run", "./cmd/search-reindex", "--if-missing"},
			filepath.Join(s.root, "backend"), oneShot, "test search reindex", 0); err != nil {
			return err
		}
		environment := s.testEnvironment()
		environment["LOG_MONITOR_URL"] = ""
		environment["LOG_MONITOR_INGEST_TOKEN"] = ""
		return s.runCommand([]string{"go", "-C", "backend", "test", "-p", "1", "-tags=integration", "./..."},
			s.root, environment, "business integration tests", businessIntegrationTimeout)
	}
	if err := s.startObservationProcesses(state); err != nil {
		return err
	}
	environment := s.testEnvironment()
	environment["OBSERVABILITY_INTEGRATION"] = "1"
	return s.runCommand([]string{"go", "-C", "backend", "test", "-p", "1",
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
	if err := s.runCommand([]string{"go", "run", "./cmd/migrate", "up"}, backendDir, oneShot,
		"test database migration", 0); err != nil {
		return err
	}
	if err := s.runCommand([]string{"go", "run", "./cmd/search-reindex", "--if-missing"}, backendDir, oneShot,
		"test search reindex", 0); err != nil {
		return err
	}
	if err := exporterpackages.Prepare(s.root, s.values["MONITOR_PACKAGE_DIR"], s.processEnv); err != nil {
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
	worker, err := s.buildBinary("integration-business-worker", backendDir, "./cmd/business-worker", buildDigest)
	if err != nil {
		return err
	}
	indexer, err := s.buildBinary("integration-search-indexer", backendDir, "./cmd/search-indexer", buildDigest)
	if err != nil {
		return err
	}
	marshaller, err := s.buildBinary("integration-marshaller", filepath.Join(s.root, "marshaller"), "./cmd/marshaller", observationDigest)
	if err != nil {
		return err
	}
	monitorDigest, err := digest.Paths(s.root, []string{
		filepath.Join(s.root, "runtimekit"),
		filepath.Join(s.root, "monitor"),
	})
	if err != nil {
		return err
	}
	monitor, err := s.buildBinary("integration-monitor", filepath.Join(s.root, "monitor"), "./cmd/monitor", monitorDigest)
	if err != nil {
		return err
	}
	adminRole, err := s.buildBinary("integration-admin-role", backendDir, "./cmd/admin-role", buildDigest)
	if err != nil {
		return err
	}

	marshallerEnv := copyEnvironment(s.values)
	marshallerEnv["GOPULSE_INSTANCE_ID"] = "marshaller-test"
	backendEnv := copyEnvironment(s.values)
	backendEnv["GOPULSE_INSTANCE_ID"] = "backend-test"
	workerEnv := copyEnvironment(s.values)
	workerEnv["GOPULSE_INSTANCE_ID"] = "business-worker-test"
	indexerEnv := copyEnvironment(s.values)
	indexerEnv["GOPULSE_INSTANCE_ID"] = "search-indexer-test"
	monitorEnv := copyEnvironment(s.values)
	monitorEnv["GOPULSE_RUNTIME_MODE"] = "host"
	monitorEnv["GOPULSE_INSTANCE_ID"] = "monitor-test"
	monitorEnv["MONITOR_HTTP_HOST"] = "127.0.0.1"
	monitorEnv["MONITOR_PLUGIN_ROOT"] = filepath.Join(s.ws.PrivateRoot(), "monitor-plugins")
	monitorEnv["MONITOR_SPOOL_DIR"] = filepath.Join(s.ws.PrivateRoot(), "monitor-spool")
	monitorEnv["MONITOR_RELEASE_CATALOG"] = filepath.Join(s.values["MONITOR_PACKAGE_DIR"], "catalog.json")
	monitorEnv["MONITOR_BOOTSTRAP_PACKAGE"] = redisExporterPackage(s.root, s.values["MONITOR_PACKAGE_DIR"])

	if err := s.spawn(state, "marshaller", []string{marshaller}, marshallerEnv, filepath.Join(s.root, "marshaller")); err != nil {
		return err
	}
	if err := ready.Wait(fmt.Sprintf("http://127.0.0.1:%s/ready", s.values["MARSHALLER_HTTP_PORT"]), readinessTimeout,
		s.values["MARSHALLER_API_TOKEN"], "test Marshaller", s.processAlive(state, "marshaller")); err != nil {
		return err
	}
	if err := s.spawn(state, "backend", []string{backend}, backendEnv, backendDir); err != nil {
		return err
	}
	if err := ready.Wait(fmt.Sprintf("http://127.0.0.1:%s/ready", s.values["HTTP_PORT"]), readinessTimeout,
		"", "test Backend", s.processAlive(state, "backend")); err != nil {
		return err
	}
	if err := s.spawn(state, "business-worker", []string{worker}, workerEnv, backendDir); err != nil {
		return err
	}
	if err := ready.Wait("http://127.0.0.1:19102/ready", readinessTimeout, "",
		"test Business Worker", s.processAlive(state, "business-worker")); err != nil {
		return err
	}
	if err := s.spawn(state, "search-indexer", []string{indexer}, indexerEnv, backendDir); err != nil {
		return err
	}
	if err := ready.Wait("http://127.0.0.1:19103/ready", readinessTimeout, "",
		"test Search Indexer", s.processAlive(state, "search-indexer")); err != nil {
		return err
	}
	if err := s.spawn(state, "monitor", []string{monitor}, monitorEnv, filepath.Join(s.root, "monitor")); err != nil {
		return err
	}
	if err := ready.Wait(fmt.Sprintf("http://127.0.0.1:%s/ready", s.values["MONITOR_HTTP_PORT"]), readinessTimeout,
		monitorEnv["MONITOR_API_TOKEN"], "test Monitor", s.processAlive(state, "monitor")); err != nil {
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

func redisExporterPackage(root, packagesRoot string) string {
	versionBytes, err := os.ReadFile(filepath.Join(root, "exporters", "redis", "VERSION"))
	if err != nil {
		return ""
	}
	version := strings.TrimSpace(string(versionBytes))
	if version == "" {
		return ""
	}
	return filepath.Join(packagesRoot, fmt.Sprintf("gopulse-redis-exporter-%s-linux-%s.tar.gz", version, runtime.GOARCH))
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
	return s.runCommand([]string{adminRole, "promote", "--username", username},
		filepath.Join(s.root, "backend"), s.testEnvironment(), "test observability admin-role bootstrap", 0)
}

// cleanup terminates the owned test processes and removes only the accounts the
// observation flow created.
func (s *session) cleanup(state *workspace.State, observe bool) []error {
	cleanupErrors := []error{}
	for _, record := range state.Processes {
		proc.Terminate(record)
	}
	if observe {
		if err := s.cleanupObservationAccounts(state); err != nil {
			state.Status = "failed"
			cleanupErrors = append(cleanupErrors, err)
			fmt.Fprintf(os.Stderr, "[gopulse] warning: %v\n", err)
		}
	}
	if s.stopDependenciesResult != nil {
		if err := s.stopDependenciesResult(state); err != nil {
			cleanupErrors = append(cleanupErrors, err)
			fmt.Fprintf(os.Stderr, "[gopulse] warning: %v\n", err)
		}
	} else if s.stopDependencies != nil {
		s.stopDependencies(state)
	}
	state.StoppedAt = now()
	if err := s.saveIntegrationState(state); err != nil {
		cleanupErrors = append(cleanupErrors, err)
		fmt.Fprintf(os.Stderr, "[gopulse] warning: %v\n", err)
	}
	return cleanupErrors
}

// observationUsernames is the deterministic set the integration flow may
// create for this workspace.
func (s *session) observationUsernames() ([]string, error) {
	usernames := []string{}
	add := func(username string) {
		if username == "" {
			return
		}
		for _, existing := range usernames {
			if existing == username {
				return
			}
		}
		usernames = append(usernames, username)
	}
	for _, key := range []string{"OBSERVE_ADMIN_USERNAME", "OBSERVE_USER_USERNAME", "OBSERVE_DEMOTION_USERNAME"} {
		add(s.values[key])
	}
	// The observability integration creates the matching user directly in the
	// flow test. Derive that integration-only name so cleanup covers both users.
	if admin := s.values["OBSERVE_ADMIN_USERNAME"]; strings.HasPrefix(admin, "observe_admin_") {
		add(strings.Replace(admin, "observe_admin_", "observe_user_", 1))
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
	if state == nil || state.Project == "" || s.containers == nil || s.containers.mysql == nil {
		return fmt.Errorf("cannot clean observability test administrator without Testcontainers state")
	}
	code, output, execErr := s.containers.mysql.Exec(context.Background(), []string{
		"mysql", "-uroot", "-p" + s.values["MYSQL_ROOT_PASSWORD"], s.values["MYSQL_DATABASE"], "-e", sql,
	})
	if output != nil {
		_, _ = io.Copy(io.Discard, output)
	}
	if execErr != nil {
		return fmt.Errorf("observability test administrator cleanup failed: %w", execErr)
	}
	if code != 0 {
		return fmt.Errorf("observability test administrator cleanup failed with exit code %d", code)
	}
	return nil
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
	if err := s.runCommand(command, directory, s.processEnv, "build "+name, 0); err != nil {
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

func (s *session) processAlive(state *workspace.State, name string) func() bool {
	return func() bool {
		if s.context != nil {
			select {
			case <-s.context.Done():
				return false
			default:
			}
		}
		return aliveIn(state, name)()
	}
}

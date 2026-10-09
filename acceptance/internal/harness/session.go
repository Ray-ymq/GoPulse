// Package harness owns one disposable Compose acceptance project and its
// private environment, command records, and cleanup boundary.
package harness

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Ray-ymq/GoPulse/acceptance/internal/contracts"
	"github.com/Ray-ymq/GoPulse/acceptance/internal/receipt"
)

type Result struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

type CommandRecord struct {
	Args       []string
	ExitCode   int
	DurationMS int64
	Error      string
}

type Session struct {
	Root     string
	Version  string
	Revision string
	Token    string
	Project  string
	TempDir  string
	EnvFile  string
	Keep     bool
	Started  bool
	Cleaned  bool
	Commands []CommandRecord
	receipt  receipt.Document
	values   map[string]string
}

func New(root string, keep bool) (*Session, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	version, err := readVersion(filepath.Join(root, "VERSION"))
	if err != nil {
		return nil, err
	}
	revision, err := commandOutput(root, "git", "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	project := "gopulse-accept-" + token
	if err := contracts.ValidateProjectName(project); err != nil {
		return nil, err
	}
	temporary, err := os.MkdirTemp("", "gopulse-compose-"+token+"-")
	if err != nil {
		return nil, err
	}
	values, err := parseEnv(filepath.Join(root, ".env.example"))
	if err != nil {
		os.RemoveAll(temporary)
		return nil, err
	}
	applyAcceptanceValues(values, token, version, revision)
	envFile := filepath.Join(temporary, "acceptance.env")
	if err := writeEnv(envFile, values); err != nil {
		os.RemoveAll(temporary)
		return nil, err
	}
	return &Session{
		Root:     root,
		Version:  version,
		Revision: revision,
		Token:    token,
		Project:  project,
		TempDir:  temporary,
		EnvFile:  envFile,
		Keep:     keep,
		receipt:  receipt.New(project, version, revision),
		values:   values,
	}, nil
}

func (s *Session) Receipt() receipt.Document {
	document := s.receipt
	document.Commands = make([]receipt.Command, 0, len(s.Commands))
	for _, command := range s.Commands {
		document.Commands = append(document.Commands, receipt.Command{
			Args:       append([]string(nil), command.Args...),
			ExitCode:   command.ExitCode,
			DurationMS: command.DurationMS,
			Error:      command.Error,
		})
	}
	return document
}

func (s *Session) SetStatus(status string, err error) {
	s.receipt.Status = status
	if err != nil {
		s.receipt.Error = err.Error()
	}
}

func (s *Session) Compose(args ...string) Result {
	command := append([]string{"compose", "--project-name", s.Project, "--env-file", s.EnvFile, "--file", filepath.Join(s.Root, "deploy", "compose.yaml")}, args...)
	return s.run("docker", command...)
}

// AcceptanceRun executes one of the checked-in Playwright scenarios inside
// the acceptance image. Environment names are passed without values so the
// command inherits the private session environment without placing secrets in
// the receipt command arguments.
func (s *Session) AcceptanceRun(scenario, spec string, environment ...string) Result {
	return s.AcceptanceRunArgs(scenario, spec, nil, environment...)
}

// AcceptanceRunArgs is AcceptanceRun with additional Playwright arguments,
// such as a focused grep used by a migrated specialist suite.
func (s *Session) AcceptanceRunArgs(scenario, spec string, extra []string, environment ...string) Result {
	args := []string{"--profile", "acceptance", "run", "--rm", "--no-deps"}
	for _, key := range environment {
		if _, ok := s.values[key]; !ok {
			continue
		}
		args = append(args, "-e", key)
	}
	if scenario != "" {
		s.values["GOPULSE_ACCEPTANCE_SCENARIO"] = scenario
		args = append(args, "-e", "GOPULSE_ACCEPTANCE_SCENARIO")
	}
	args = append(args, "acceptance", spec)
	args = append(args, extra...)
	return s.Compose(args...)
}

// Value returns a private generated acceptance value for scenario wiring.
func (s *Session) Value(key string) string { return s.values[key] }

// SetValue adds a non-secret derived value to the private scenario environment.
func (s *Session) SetValue(key, value string) { s.values[key] = value }

func (s *Session) Run(name string, args ...string) Result { return s.run(name, args...) }

func (s *Session) run(name string, args ...string) Result {
	started := time.Now()
	command := exec.Command(name, args...)
	command.Dir = s.Root
	command.Env = s.environment()
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	result := Result{Stdout: stdout.String(), Stderr: stderr.String()}
	if err == nil {
		result.ExitCode = 0
	} else if exit, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exit.ExitCode()
	} else {
		result.ExitCode = 1
		result.Stderr = strings.TrimSpace(result.Stderr + "\n" + err.Error())
	}
	if result.Stdout != "" {
		fmt.Print(result.Stdout)
	}
	if result.Stderr != "" {
		fmt.Fprint(os.Stderr, result.Stderr)
	}
	s.Commands = append(s.Commands, CommandRecord{Args: append([]string{name}, args...), ExitCode: result.ExitCode, DurationMS: time.Since(started).Milliseconds()})
	return result
}

func (s *Session) Preflight() error {
	if result := s.Run("docker", "info"); result.ExitCode != 0 {
		return fmt.Errorf("Docker Engine is unavailable")
	}
	if result := s.Run("docker", "compose", "version"); result.ExitCode != 0 {
		return fmt.Errorf("Docker Compose v2 is unavailable")
	}
	for _, args := range [][]string{
		{"ps", "-aq", "--filter", "label=com.docker.compose.project=" + s.Project},
		{"network", "ls", "-q", "--filter", "label=com.docker.compose.project=" + s.Project},
		{"volume", "ls", "-q", "--filter", "label=com.docker.compose.project=" + s.Project},
	} {
		result := s.Run("docker", args...)
		if result.ExitCode != 0 {
			return fmt.Errorf("inspect acceptance project ownership failed with exit code %d", result.ExitCode)
		}
		if strings.TrimSpace(result.Stdout) != "" {
			return fmt.Errorf("acceptance project %s already owns Docker resources", s.Project)
		}
	}
	return nil
}

func (s *Session) Cleanup() error {
	if s.Cleaned {
		return nil
	}
	s.Cleaned = true
	if s.Keep {
		return nil
	}
	if !s.Started {
		return os.RemoveAll(s.TempDir)
	}
	if err := contracts.ValidateProjectName(s.Project); err != nil {
		return err
	}
	result := s.Compose("down", "--volumes", "--remove-orphans")
	removeErr := os.RemoveAll(s.TempDir)
	if result.ExitCode != 0 {
		s.receipt.CleanupPassed = false
		return fmt.Errorf("acceptance cleanup failed with exit code %d", result.ExitCode)
	}
	if removeErr != nil {
		s.receipt.CleanupPassed = false
	}
	return removeErr
}

func (s *Session) WriteReceipt(path string) error {
	if path == "" {
		return nil
	}
	return receipt.WriteAtomic(path, s.Receipt())
}

func (s *Session) MarkStarted() { s.Started = true }

func readVersion(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	version := strings.TrimSpace(string(raw))
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("VERSION must use major.minor.patch")
	}
	for _, part := range parts {
		if part == "" {
			return "", fmt.Errorf("VERSION must use major.minor.patch")
		}
		for _, character := range part {
			if character < '0' || character > '9' {
				return "", fmt.Errorf("VERSION must use major.minor.patch")
			}
		}
	}
	return version, nil
}

func commandOutput(root, name string, args ...string) (string, error) {
	command := exec.Command(name, args...)
	command.Dir = root
	raw, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("%s failed: %w", name, err)
	}
	return strings.TrimSpace(string(raw)), nil
}

func randomToken() (string, error) {
	raw := make([]byte, 6)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func parseEnv(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), "\"'")
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func applyAcceptanceValues(values map[string]string, token, version, revision string) {
	values["APP_ENV"] = "test"
	values["PUBLISHED_HOST"] = "127.0.0.1"
	values["HTTP_PORT"] = "0"
	values["FRONTEND_PORT"] = "0"
	values["MYSQL_DATABASE"] = "gopulse_" + token
	values["MYSQL_USER"] = "user_" + token
	values["MYSQL_PASSWORD"] = "mysql-" + token
	values["MYSQL_ROOT_PASSWORD"] = "root-" + token
	values["REDIS_PASSWORD"] = "redis-" + token
	values["RABBITMQ_USER"] = "rabbit_" + token
	values["RABBITMQ_PASSWORD"] = "rabbit-" + token
	values["AUTH_JWT_SECRET"] = "jwt-" + token + "-0123456789abcdef0123456789abcdef"
	values["AUTH_COOKIE_NAME"] = "gopulse_" + token
	values["MONITOR_API_TOKEN"] = "monitor-" + token + "-0123456789abcdef0123456789"
	values["GOPULSE_OBSERVABILITY_ADMIN_USERNAME"] = "admin_" + token
	values["GOPULSE_OBSERVABILITY_USER_USERNAME"] = "user_" + token
	values["GOPULSE_OBSERVABILITY_PASSWORD"] = "acceptance-" + token + "-password"
	values["GOPULSE_PLUGIN_ACCOUNT_PASSWORD"] = "metrics-" + token
	values["GOPULSE_ADMIN_USERNAME"] = values["GOPULSE_OBSERVABILITY_ADMIN_USERNAME"]
	values["GOPULSE_USER_USERNAME"] = values["GOPULSE_OBSERVABILITY_USER_USERNAME"]
	values["GOPULSE_DEMOTION_USERNAME"] = values["GOPULSE_OBSERVABILITY_ADMIN_USERNAME"]
	values["GOPULSE_ACCEPTANCE_PASSWORD"] = values["GOPULSE_OBSERVABILITY_PASSWORD"]
	values["GOPULSE_BASE_URL"] = "http://frontend:8080"
	values["GOPULSE_SCREENSHOT_DIR"] = "frontend/test-results"
	for _, key := range []string{"BACKEND_METRICS_TOKEN", "BUSINESS_WORKER_METRICS_TOKEN", "SEARCH_INDEXER_METRICS_TOKEN", "MONITOR_METRICS_TOKEN", "ROUTER_METRICS_TOKEN", "MARSHALLER_METRICS_TOKEN"} {
		values[key] = "metrics-" + strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(key, ""), "_METRICS_TOKEN")) + "-" + token + "-0123456789abcdef0123456789"
	}
	values["LOG_MONITOR_INGEST_TOKEN"] = "logs-" + token + "-0123456789abcdef0123456789ab"
	values["ROUTER_API_TOKEN"] = "router-" + token + "-0123456789abcdef0123456789"
	values["MARSHALLER_API_TOKEN"] = "marshaller-" + token + "-0123456789abcdef012345"
	values["VICTORIAMETRICS_USERNAME"] = "vm_" + token
	values["VICTORIAMETRICS_PASSWORD"] = "vm-" + token + "-0123456789abcdef0123456789abc"
	values["GOPULSE_VERSION"] = version
	values["GOPULSE_REVISION"] = revision
	values["GOPULSE_IMAGE_TAG"] = version
	values["GOPULSE_UPDATE_VERSION"] = nextVersion(version)
}

func nextVersion(version string) string {
	parts := strings.Split(version, ".")
	patch, _ := strconv.Atoi(parts[2])
	return parts[0] + "." + parts[1] + "." + strconv.Itoa(patch+1)
}

func writeEnv(path string, values map[string]string) error {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	for _, key := range keys {
		builder.WriteString(key)
		builder.WriteByte('=')
		builder.WriteString(values[key])
		builder.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(builder.String()), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func acceptanceEnvironment(version, revision, token string, generated map[string]string) []string {
	values := map[string]string{}
	for _, entry := range os.Environ() {
		key, value, found := strings.Cut(entry, "=")
		if found {
			values[key] = value
		}
	}
	values["GOPULSE_VERSION"] = version
	values["GOPULSE_REVISION"] = revision
	values["GOPULSE_IMAGE_TAG"] = version
	values["GOPULSE_ACCEPTANCE_TOKEN"] = token
	for key, value := range generated {
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result
}

func (s *Session) environment() []string {
	return acceptanceEnvironment(s.Version, s.Revision, s.Token, s.values)
}

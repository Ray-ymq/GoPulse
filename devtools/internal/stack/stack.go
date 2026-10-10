// Package stack owns the container-native daily lifecycle that used to live in
// make dev, make stack-down and make stack-verify.
package stack

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var projectPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)

var productTargets = []string{
	"backend", "business-worker", "search-indexer", "admin-frontend", "frontend",
	"acceptance", "router", "marshaller", "monitor", "redis-exporter",
}

var requiredServices = []string{
	"mysql", "redis", "rabbitmq", "elasticsearch", "kafka", "victoriametrics",
	"router", "marshaller", "monitor", "backend", "business-worker", "search-indexer",
	"admin-frontend", "frontend",
}

var completedServices = []string{"migrate", "search-init", "kafka-init"}

// Up builds and starts the default project, after checking that any existing
// resources with the selected project label belong to this Compose directory.
func Up(root string) error {
	ctx, err := newContext(root)
	if err != nil {
		return err
	}
	if err := ctx.validateHostContract(); err != nil {
		return err
	}
	if err := ctx.checkOwnership(); err != nil {
		return err
	}
	if err := ctx.prepareBackendPortOverride(); err != nil {
		return err
	}
	defer ctx.removeBackendPortOverride()
	if err := ctx.run(append(ctx.composeArgs("build"), productTargets...)...); err != nil {
		return fmt.Errorf("build product images: %w", err)
	}
	if err := ctx.run(ctx.composeArgs("up", "--detach", "--wait", "--wait-timeout", "420")...); err != nil {
		return fmt.Errorf("start Compose stack: %w", err)
	}
	return Verify(root)
}

// Down stops only the verified project and preserves named volumes.
func Down(root string) error {
	ctx, err := newContext(root)
	if err != nil {
		return err
	}
	if err := ctx.checkOwnership(); err != nil {
		return err
	}
	if err := ctx.run(ctx.composeArgs("down", "--remove-orphans")...); err != nil {
		return fmt.Errorf("stop Compose stack: %w", err)
	}
	return nil
}

// Verify checks service state, health and the two intentionally published
// loopback ports without changing product data.
func Verify(root string) error {
	ctx, err := newContext(root)
	if err != nil {
		return err
	}
	if err := ctx.checkOwnership(); err != nil {
		return err
	}
	for _, service := range requiredServices {
		if err := ctx.verifyService(service, "running"); err != nil {
			return err
		}
	}
	for _, service := range completedServices {
		if err := ctx.verifyService(service, "exited"); err != nil {
			return err
		}
	}
	for _, service := range append([]string{"admin-frontend", "mysql", "redis", "rabbitmq", "elasticsearch", "kafka", "victoriametrics", "router", "marshaller", "monitor", "business-worker", "search-indexer"}, "platform-api") {
		if err := ctx.verifyNotPublished(service); err != nil {
			return err
		}
	}
	for _, service := range []string{"frontend", "backend"} {
		if err := ctx.verifyLoopbackPort(service); err != nil {
			return err
		}
	}
	frontendPort, err := ctx.port("frontend", "8080")
	if err != nil {
		return err
	}
	client := http.Client{Timeout: 5 * time.Second}
	response, err := client.Get("http://127.0.0.1:" + frontendPort + "/frontend-health")
	if err != nil {
		return fmt.Errorf("frontend health request failed: %w", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("frontend health returned HTTP %d", response.StatusCode)
	}
	return nil
}

type context struct {
	root         string
	composeDir   string
	project      string
	envFile      string
	env          []string
	host         string
	httpPort     string
	frontendPort string
	overrideFile string
}

func newContext(root string) (*context, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	project := os.Getenv("COMPOSE_PROJECT_NAME")
	if project == "" {
		project = "gopulse"
	}
	if !projectPattern.MatchString(project) {
		return nil, fmt.Errorf("project name must match %s", projectPattern.String())
	}
	envFile := filepath.Join(absolute, ".env")
	if _, err := os.Stat(envFile); os.IsNotExist(err) {
		data, readErr := os.ReadFile(filepath.Join(absolute, ".env.example"))
		if readErr != nil {
			return nil, readErr
		}
		if writeErr := os.WriteFile(envFile, data, 0o600); writeErr != nil {
			return nil, writeErr
		}
	} else if err != nil {
		return nil, err
	}
	if err := os.Chmod(envFile, 0o600); err != nil {
		return nil, err
	}
	version, err := readValue(filepath.Join(absolute, "VERSION"))
	if err != nil {
		return nil, err
	}
	revision, err := output(absolute, "git", "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	env := append([]string{}, os.Environ()...)
	env = append(env, "GOPULSE_VERSION="+version, "GOPULSE_REVISION="+revision, "GOPULSE_IMAGE_TAG="+version)
	return &context{
		root:         absolute,
		composeDir:   filepath.Join(absolute, "deploy"),
		project:      project,
		envFile:      envFile,
		env:          env,
		host:         effectiveEnvValue(envFile, "PUBLISHED_HOST", "127.0.0.1"),
		httpPort:     effectiveEnvValue(envFile, "HTTP_PORT", ""),
		frontendPort: effectiveEnvValue(envFile, "FRONTEND_PORT", ""),
	}, nil
}

func (c *context) composeArgs(args ...string) []string {
	base := []string{"docker", "compose", "--project-name", c.project, "--env-file", c.envFile, "--file", filepath.Join(c.composeDir, "compose.yaml")}
	if c.overrideFile != "" {
		base = append(base, "--file", c.overrideFile)
	}
	return append(base, args...)
}

func (c *context) run(args ...string) error {
	command := exec.Command(args[0], args[1:]...)
	command.Dir = c.root
	command.Env = c.env
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return err
	}
	return nil
}

func (c *context) capture(args ...string) (string, error) {
	command := exec.Command(args[0], args[1:]...)
	command.Dir = c.root
	command.Env = c.env
	data, err := command.Output()
	return strings.TrimSpace(string(data)), err
}

func (c *context) checkOwnership() error {
	ids, err := c.capture("docker", "ps", "-aq", "--filter", "label=com.docker.compose.project="+c.project)
	if err != nil {
		return fmt.Errorf("inspect project containers: %w", err)
	}
	for _, id := range nonEmptyLines(ids) {
		workingDir, inspectErr := c.capture("docker", "inspect", "--format", "{{index .Config.Labels \"com.docker.compose.project.working_dir\"}}", id)
		if inspectErr != nil || filepath.Clean(workingDir) != filepath.Clean(c.composeDir) {
			return fmt.Errorf("container %s failed project ownership validation", id)
		}
	}
	for _, kind := range []string{"network", "volume"} {
		ids, inspectErr := c.capture("docker", kind, "ls", "-q", "--filter", "label=com.docker.compose.project="+c.project)
		if inspectErr != nil {
			return fmt.Errorf("inspect project %s: %w", kind, inspectErr)
		}
		for _, id := range nonEmptyLines(ids) {
			label, labelErr := c.capture("docker", kind, "inspect", "--format", "{{index .Labels \"com.docker.compose.project\"}}", id)
			if labelErr != nil || label != c.project {
				return fmt.Errorf("%s %s failed project ownership validation", kind, id)
			}
		}
	}
	return nil
}

func (c *context) verifyService(service, expected string) error {
	id, err := c.capture("docker", "ps", "-aq", "--filter", "label=com.docker.compose.project="+c.project, "--filter", "label=com.docker.compose.service="+service)
	ids := nonEmptyLines(id)
	if err != nil || len(ids) != 1 {
		return fmt.Errorf("%s must have exactly one project container; found %d", service, len(ids))
	}
	state, err := c.capture("docker", "inspect", "--format", "{{.State.Status}}", ids[0])
	if err != nil || state != expected {
		return fmt.Errorf("%s state is %s, expected %s", service, state, expected)
	}
	if expected == "exited" {
		code, codeErr := c.capture("docker", "inspect", "--format", "{{.State.ExitCode}}", ids[0])
		if codeErr != nil || code != "0" {
			return fmt.Errorf("%s exited with code %s", service, code)
		}
		return nil
	}
	health, healthErr := c.capture("docker", "inspect", "--format", "{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}", ids[0])
	if healthErr != nil || health != "healthy" {
		return fmt.Errorf("%s health is %s", service, health)
	}
	return nil
}

func (c *context) verifyNotPublished(service string) error {
	id, err := c.capture("docker", "ps", "-aq", "--filter", "label=com.docker.compose.project="+c.project, "--filter", "label=com.docker.compose.service="+service)
	ids := nonEmptyLines(id)
	if err != nil || len(ids) != 1 {
		return fmt.Errorf("%s must have exactly one project container; found %d", service, len(ids))
	}
	bindings, err := c.capture("docker", "inspect", "--format", "{{json .HostConfig.PortBindings}}", ids[0])
	if err != nil || bindings != "{}" && bindings != "null" {
		return fmt.Errorf("%s unexpectedly publishes host ports", service)
	}
	return nil
}

func (c *context) verifyLoopbackPort(service string) error {
	id, err := c.capture("docker", "ps", "-aq", "--filter", "label=com.docker.compose.project="+c.project, "--filter", "label=com.docker.compose.service="+service)
	ids := nonEmptyLines(id)
	if err != nil || len(ids) != 1 {
		return fmt.Errorf("%s must have exactly one project container; found %d", service, len(ids))
	}
	bindings, err := c.capture("docker", "inspect", "--format", "{{range $p, $bindings := .HostConfig.PortBindings}}{{range $bindings}}{{.HostIp}} {{end}}{{end}}", ids[0])
	fields := strings.Fields(bindings)
	if err != nil || len(fields) != 1 || fields[0] != "127.0.0.1" {
		return fmt.Errorf("%s must publish exactly one loopback port", service)
	}
	return nil
}

func (c *context) port(service, containerPort string) (string, error) {
	value, err := c.capture(c.composeArgs("port", service, containerPort)...)
	if err != nil {
		return "", err
	}
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 || parts[0] != "127.0.0.1" {
		return "", fmt.Errorf("%s port is not loopback published: %s", service, value)
	}
	if _, err := strconv.Atoi(parts[1]); err != nil {
		return "", fmt.Errorf("%s published port is invalid: %s", service, parts[1])
	}
	return parts[1], nil
}

func (c *context) validateHostContract() error {
	if c.host != "127.0.0.1" {
		return fmt.Errorf("PUBLISHED_HOST must be exactly 127.0.0.1")
	}
	if err := validatePort("HTTP_PORT", c.httpPort); err != nil {
		return err
	}
	if err := validatePort("FRONTEND_PORT", c.frontendPort); err != nil {
		return err
	}
	return nil
}

func (c *context) prepareBackendPortOverride() error {
	file, err := os.CreateTemp("", "gopulse-stack-override-*.yaml")
	if err != nil {
		return err
	}
	c.overrideFile = file.Name()
	content := fmt.Sprintf("services:\n  backend:\n    ports:\n      - \"127.0.0.1:%s:8080\"\n", c.httpPort)
	if _, err := file.WriteString(content); err != nil {
		file.Close()
		c.removeBackendPortOverride()
		return err
	}
	if err := file.Close(); err != nil {
		c.removeBackendPortOverride()
		return err
	}
	return nil
}

func (c *context) removeBackendPortOverride() {
	if c.overrideFile == "" {
		return
	}
	_ = os.Remove(c.overrideFile)
	c.overrideFile = ""
}

func readValue(path string) (string, error) {
	data, err := os.ReadFile(path)
	return strings.TrimSpace(string(data)), err
}

func output(root string, name string, args ...string) (string, error) {
	command := exec.Command(name, args...)
	command.Dir = root
	data, err := command.Output()
	return strings.TrimSpace(string(data)), err
}

func envFileValue(path, wanted string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if found && strings.TrimSpace(key) == wanted {
			return strings.Trim(strings.TrimSpace(value), "\"'"), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("%s is not set in %s", wanted, path)
}

func effectiveEnvValue(path, wanted, fallback string) string {
	if value, ok := os.LookupEnv(wanted); ok {
		return value
	}
	value, err := envFileValue(path, wanted)
	if err == nil {
		return value
	}
	return fallback
}

func validatePort(key, value string) error {
	if value == "" || len(value) > 5 || !regexp.MustCompile(`^[0-9]+$`).MatchString(value) {
		return fmt.Errorf("%s must be a single decimal port between 1 and 65535", key)
	}
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("%s must be a single decimal port between 1 and 65535", key)
	}
	return nil
}

func nonEmptyLines(value string) []string {
	lines := strings.Split(strings.TrimSpace(value), "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			result = append(result, strings.TrimSpace(line))
		}
	}
	return result
}

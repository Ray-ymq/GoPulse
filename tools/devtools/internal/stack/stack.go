// Package stack 管理当前工作区的三种 Compose 产品模式。
package stack

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Ray-ymq/GoPulse/devtools/internal/buildcache"
	"github.com/Ray-ymq/GoPulse/devtools/internal/deployment"
	"github.com/Ray-ymq/GoPulse/devtools/internal/devrun"
	"github.com/Ray-ymq/GoPulse/devtools/internal/exporterpackages"
)

var projectPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)

func Up(root string, modes ...string) (result error) {
	ctx, err := newContext(root, modes...)
	if err != nil {
		return err
	}
	unlock, err := deployment.Lock(ctx.root)
	if err != nil {
		return err
	}
	defer unlock()
	if err := deployment.Check(ctx.root, ctx.mode, "product", ctx.project); err != nil {
		return err
	}
	if err := ctx.validateHostContract(); err != nil {
		return err
	}
	if err := ctx.checkOwnership(); err != nil {
		return err
	}
	if err := deployment.Record(ctx.root, ctx.mode, ctx.project, "starting"); err != nil {
		return err
	}
	defer func() {
		if result != nil {
			_ = deployment.Record(ctx.root, ctx.mode, ctx.project, "failed")
		}
	}()
	if err := exporterpackages.Prepare(ctx.root, filepath.Join(ctx.root, ".run/packages"), devrun.Environ()); err != nil {
		return err
	}
	code, err := buildcache.Run(ctx.root, buildcache.Options{NoCache: true})
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("产品镜像构建失败，退出码 %d", code)
	}
	if err := ctx.run(ctx.composeArgs("up", "--detach", "--wait", "--wait-timeout", "420")...); err != nil {
		return fmt.Errorf("启动 Compose 失败：%w", err)
	}
	if err := ctx.verify(); err != nil {
		return err
	}
	return deployment.Record(ctx.root, ctx.mode, ctx.project, "running")
}

func Down(root string, modes ...string) error {
	ctx, err := newContext(root, modes...)
	if err != nil {
		return err
	}
	unlock, err := deployment.Lock(ctx.root)
	if err != nil {
		return err
	}
	defer unlock()
	if err := ctx.checkOwnership(); err != nil {
		return err
	}
	if err := ctx.run(ctx.composeArgs("down")...); err != nil {
		return err
	}
	return deployment.Record(ctx.root, ctx.mode, ctx.project, "stopped")
}

func Verify(root string, modes ...string) error {
	ctx, err := newContext(root, modes...)
	if err != nil {
		return err
	}
	return ctx.verify()
}
func Status(root string, modes ...string) error {
	ctx, err := newContext(root, modes...)
	if err != nil {
		return err
	}
	if err := ctx.checkOwnership(); err != nil {
		return err
	}
	return ctx.run(ctx.composeArgs("ps", "--all")...)
}
func (c *context) verify() error {
	if err := c.checkOwnership(); err != nil {
		return err
	}
	raw, err := c.capture(c.composeArgs("config", "--format", "json")...)
	if err != nil {
		return err
	}
	var rendered struct {
		Services map[string]struct {
			Profiles []string `json:"profiles"`
			Restart  string   `json:"restart"`
		} `json:"services"`
	}
	if err := json.Unmarshal([]byte(raw), &rendered); err != nil {
		return err
	}
	for name, service := range rendered.Services {
		if len(service.Profiles) > 0 {
			continue
		}
		expected := "running"
		if name == "migrate" || name == "search-init" || name == "kafka-init" {
			expected = "exited"
		}
		if err := c.verifyService(name, expected); err != nil {
			return err
		}
		edge := "frontend"
		if c.mode == "observability" {
			edge = "admin-frontend"
		}
		if name != edge {
			if err := c.verifyNotPublished(name); err != nil {
				return err
			}
		}
	}
	edge := "frontend"
	if c.mode == "observability" {
		edge = "admin-frontend"
	}
	if err := c.verifyLoopbackPort(edge); err != nil {
		return err
	}
	port, err := c.port(edge, "8080")
	if err != nil {
		return err
	}
	client := http.Client{Timeout: 5 * time.Second}
	res, err := client.Get("http://127.0.0.1:" + port + "/frontend-health")
	if err != nil {
		return err
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		return fmt.Errorf("前端健康检查返回 HTTP %d", res.StatusCode)
	}
	path := "/"
	if c.mode == "observability" {
		path = "/admin/login"
	}
	fmt.Printf("访问地址：http://127.0.0.1:%s%s\n", port, path)
	return nil
}

type context struct {
	mode         string
	composeFile  string
	root         string
	composeDir   string
	project      string
	envFile      string
	env          []string
	host         string
	httpPort     string
	frontendPort string
}

func newContext(root string, modes ...string) (*context, error) {
	mode := "all"
	if len(modes) > 0 && modes[0] != "" {
		mode = modes[0]
	}
	files := map[string]string{"all": "compose.yaml", "business": "compose.business.yaml", "observability": "compose.observability.yaml"}
	file, ok := files[mode]
	if !ok {
		return nil, fmt.Errorf("MODE 必须为 business、observability 或 all")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	project := os.Getenv("COMPOSE_PROJECT_NAME")
	if project == "" {
		project = "gopulse"
		if mode != "all" {
			project += "-" + mode
		}
	}
	if !projectPattern.MatchString(project) {
		return nil, fmt.Errorf("project name must match %s", projectPattern.String())
	}
	envFile := os.Getenv("GOPULSE_ENV_FILE")
	if envFile == "" {
		envFile = filepath.Join(absolute, ".env")
	}
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
	env = append(env, "GOPULSE_VERSION="+version, "GOPULSE_REVISION="+revision, "GOPULSE_WORKSPACE_ROOT="+absolute)
	return &context{
		root:         absolute,
		mode:         mode,
		composeFile:  file,
		composeDir:   filepath.Join(absolute, "deploy", "compose"),
		project:      project,
		envFile:      envFile,
		env:          env,
		host:         effectiveEnvValue(envFile, "PUBLISHED_HOST", "127.0.0.1"),
		httpPort:     effectiveEnvValue(envFile, "HTTP_PORT", ""),
		frontendPort: effectiveEnvValue(envFile, map[bool]string{true: "ADMIN_FRONTEND_PORT", false: "FRONTEND_PORT"}[mode == "observability"], ""),
	}, nil
}

func (c *context) composeArgs(args ...string) []string {
	base := []string{"docker", "compose", "--project-name", c.project, "--env-file", c.envFile, "--file", filepath.Join(c.composeDir, c.composeFile)}
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
		if inspectErr != nil || (filepath.Clean(workingDir) != filepath.Clean(c.composeDir) && filepath.Clean(workingDir) != filepath.Join(c.root, "deploy")) {
			return fmt.Errorf("container %s failed project ownership validation", id)
		}
		files, fileErr := c.capture("docker", "inspect", "--format", `{{index .Config.Labels "com.docker.compose.project.config_files"}}`, id)
		expected := filepath.Join(c.composeDir, c.composeFile)
		if fileErr != nil || !strings.Contains(files, expected) {
			return fmt.Errorf("容器 %s 不属于当前 MODE", id)
		}

	}
	for _, kind := range []string{"network", "volume"} {
		ids, inspectErr := c.capture("docker", kind, "ls", "-q", "--filter", "label=com.docker.compose.project="+c.project)
		if inspectErr != nil {
			return fmt.Errorf("inspect project %s: %w", kind, inspectErr)
		}
		for _, id := range nonEmptyLines(ids) {
			label, labelErr := c.capture("docker", kind, "inspect", "--format", "{{index .Labels \"com.docker.compose.project\"}}", id)
			owner, ownerErr := c.capture("docker", kind, "inspect", "--format", `{{index .Labels "io.gopulse.workspace"}}`, id)
			if ownerErr != nil || owner != c.root {
				return fmt.Errorf("%s %s 缺少当前工作区归属标签，拒绝接管", kind, id)
			}
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
	if err := validatePort("FRONTEND_PORT", c.frontendPort); err != nil {
		return err
	}
	return nil
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
	defer func() { _ = file.Close() }()
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

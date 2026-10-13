package scenario

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	controlMySQL         = "mysql"
	controlRabbitMQ      = "rabbitmq"
	controlElasticsearch = "elasticsearch"
)

type dependencyControl struct {
	runner     *formalV1Runner
	container  string
	network    string
	upstreamID string
	service    string
	services   []string
	controlURL string
	restored   bool
}

func (runner *formalV1Runner) startMySQLControl(mode string, services ...string) (*dependencyControl, error) {
	return runner.startDependencyControl(controlMySQL, "mysql-protocol-proxy", "3306", "19125", mode, services...)
}

func (runner *formalV1Runner) startAMQPControl(mode string, services ...string) (*dependencyControl, error) {
	return runner.startDependencyControl(controlRabbitMQ, "amqp-protocol-proxy", "5672", "19126", mode, services...)
}

func (runner *formalV1Runner) startElasticsearchControl(mode string, services ...string) (*dependencyControl, error) {
	return runner.startDependencyControl(controlElasticsearch, "http-effect-proxy", "9200", "19127", mode, services...)
}

func (runner *formalV1Runner) startDependencyControl(service, fixture, dataPort, controlPort, mode string, services ...string) (*dependencyControl, error) {
	network, err := runner.businessNetwork()
	if err != nil {
		return nil, err
	}
	upstreamID, err := runner.containerID(service)
	if err != nil {
		return nil, err
	}
	binaryPath, err := runner.buildFixture(fixture)
	if err != nil {
		return nil, err
	}
	container := fmt.Sprintf("%s-fault-%s-%s", runner.session.Project, service, runner.session.Token[:8])
	// The native acceptance runner executes on the host, while the proxy must
	// remain on the private business network to reach the rewired dependency.
	// Publish only the control listener on a Docker-assigned loopback port; the
	// data listener stays private and is reached by the application through the
	// service alias.
	control := &dependencyControl{runner: runner, container: container, network: network, upstreamID: upstreamID, service: service, services: append([]string(nil), services...)}
	ready := false
	rewired := false
	defer func() {
		if !ready && rewired {
			_ = control.Restore()
		}
	}()
	if result := runner.session.Run("docker", "network", "disconnect", network, upstreamID); result.ExitCode != 0 {
		return nil, fmt.Errorf("disconnect %s from business network failed with exit code %d", service, result.ExitCode)
	}
	rewired = true
	if result := runner.session.Run("docker", "network", "connect", "--alias", service+"-upstream", network, upstreamID); result.ExitCode != 0 {
		return nil, fmt.Errorf("reconnect %s with upstream alias failed with exit code %d", service, result.ExitCode)
	}
	image := runner.session.Value("GOPULSE_ACCEPTANCE_IMAGE")
	if image == "" {
		image = "gopulse/acceptance:" + runner.session.Value("GOPULSE_IMAGE_TAG")
	}
	args := []string{
		"create", "--name", container,
		"--label", "io.gopulse.acceptance.project=" + runner.session.Project,
		"--label", "io.gopulse.acceptance.owner=v1-fault-control",
		"--network", network, "--network-alias", service,
		"--publish", "127.0.0.1::" + controlPort,
		"--volume", binaryPath + ":/usr/local/bin/gopulse-fault-control:ro",
		"--entrypoint", "/usr/local/bin/gopulse-fault-control", image,
		"--listen", "0.0.0.0:" + dataPort,
		"--control", "0.0.0.0:" + controlPort,
		"--upstream", service + "-upstream:" + dataPort,
		"--mode", mode,
	}
	if result := runner.session.Run("docker", args...); result.ExitCode != 0 {
		return nil, fmt.Errorf("create %s protocol control failed with exit code %d", service, result.ExitCode)
	}
	controlPortNumber, err := publishedControlPort(runner, container, controlPort)
	if err != nil {
		return nil, err
	}
	control.controlURL = "http://127.0.0.1:" + strconv.Itoa(controlPortNumber)
	if result := runner.session.Run("docker", "start", container); result.ExitCode != 0 {
		return nil, fmt.Errorf("start %s protocol control failed with exit code %d", service, result.ExitCode)
	}
	if err := control.waitReady(10 * time.Second); err != nil {
		return nil, err
	}
	if len(control.services) > 0 {
		up := append([]string{"up", "--detach", "--wait", "--wait-timeout", "420", "--force-recreate", "--no-deps"}, control.services...)
		if err := runner.compose(up...); err != nil {
			return nil, err
		}
	}
	ready = true
	return control, nil
}

func publishedControlPort(runner *formalV1Runner, container, controlPort string) (int, error) {
	inspect := runner.session.Run("docker", "inspect", "--format", "{{json .NetworkSettings.Ports}}", container)
	if inspect.ExitCode != 0 {
		return 0, fmt.Errorf("inspect %s control port failed with exit code %d", container, inspect.ExitCode)
	}
	var ports map[string][]struct {
		HostIP   string `json:"HostIp"`
		HostPort string `json:"HostPort"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(inspect.Stdout)), &ports); err != nil {
		return 0, fmt.Errorf("decode %s control port mapping: %w", container, err)
	}
	bindings := ports[controlPort+"/tcp"]
	if len(bindings) != 1 || bindings[0].HostPort == "" {
		return 0, fmt.Errorf("%s control port was not published", container)
	}
	port, err := strconv.Atoi(bindings[0].HostPort)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("invalid %s control port %q", container, bindings[0].HostPort)
	}
	return port, nil
}

func (runner *formalV1Runner) businessNetwork() (string, error) {
	result := runner.session.Run("docker", "network", "ls", "--filter", "label=com.docker.compose.project="+runner.session.Project, "--filter", "label=com.docker.compose.network=business", "--format", "{{.Name}}")
	if result.ExitCode != 0 {
		return "", fmt.Errorf("find business network failed with exit code %d", result.ExitCode)
	}
	for _, value := range strings.Fields(result.Stdout) {
		if value != "" {
			return value, nil
		}
	}
	// Compose's network label has changed across supported Docker Compose
	// versions. The project-scoped name is deterministic for this file and is
	// safe to use as a fallback after the label query returns no rows.
	fallback := runner.session.Project + "_business"
	inspect := runner.session.Run("docker", "network", "inspect", fallback)
	if inspect.ExitCode == 0 {
		return fallback, nil
	}
	return "", errors.New("acceptance business network was not found")
}

func (runner *formalV1Runner) containerID(service string) (string, error) {
	result := runner.session.Compose("ps", "-q", service)
	if result.ExitCode != 0 {
		return "", fmt.Errorf("find %s container failed with exit code %d", service, result.ExitCode)
	}
	fields := strings.Fields(result.Stdout)
	if len(fields) != 1 {
		return "", fmt.Errorf("expected one %s container, found %d", service, len(fields))
	}
	return fields[0], nil
}

func (runner *formalV1Runner) buildFixture(fixture string) (string, error) {
	path := filepath.Join(runner.session.TempDir, fixture)
	// The repository intentionally contains multiple Go modules and has no
	// root go.mod. Build acceptance fixtures in their owning module just like
	// the response-loss fixture; invoking go from the repository root makes
	// every protocol-control case fail before it can start.
	result := runner.session.Run("go", "-C", "acceptance", "build", "-trimpath", "-o", path, "./internal/fixtures/"+fixture)
	if result.ExitCode != 0 {
		detail := strings.TrimSpace(result.Stderr)
		if detail == "" {
			return "", fmt.Errorf("build %s failed with exit code %d", fixture, result.ExitCode)
		}
		return "", fmt.Errorf("build %s failed with exit code %d: %s", fixture, result.ExitCode, detail)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		return "", err
	}
	return path, nil
}

func (control *dependencyControl) waitReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		request, err := http.NewRequest(http.MethodGet, control.controlURL+"/ready", nil)
		if err == nil {
			response, requestErr := (&http.Client{Timeout: 500 * time.Millisecond}).Do(request)
			if requestErr == nil {
				_ = response.Body.Close()
				if response.StatusCode == http.StatusOK {
					return nil
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("fault control %s did not become ready", control.service)
}

func (control *dependencyControl) status(ctx context.Context) (map[string]any, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, control.controlURL+"/status", nil)
	if err != nil {
		return nil, err
	}
	response, err := (&http.Client{Timeout: 2 * time.Second}).Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fault control status returned HTTP %d", response.StatusCode)
	}
	var value map[string]any
	if err := json.NewDecoder(response.Body).Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func (control *dependencyControl) waitStatus(timeout time.Duration, predicate func(map[string]any) bool) (map[string]any, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		value, err := control.status(context.Background())
		if err == nil && predicate(value) {
			return value, nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	value, err := control.status(context.Background())
	if err != nil {
		return nil, err
	}
	return value, errors.New("fault control did not reach the requested protocol state")
}

func (control *dependencyControl) setTarget(eventID string) error {
	values := url.Values{}
	values.Set("event_id", eventID)
	request, err := http.NewRequest(http.MethodGet, control.controlURL+"/target?"+values.Encode(), nil)
	if err != nil {
		return err
	}
	response, err := (&http.Client{Timeout: 2 * time.Second}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("set fault control target returned HTTP %d", response.StatusCode)
	}
	return nil
}

func (control *dependencyControl) Restore() error {
	if control == nil || control.restored {
		return nil
	}
	control.restored = true
	var first error
	remember := func(err error) {
		if first == nil && err != nil {
			first = err
		}
	}
	if result := control.runner.session.Run("docker", "rm", "-f", control.container); result.ExitCode != 0 {
		remember(fmt.Errorf("remove fault control failed with exit code %d", result.ExitCode))
	}
	if result := control.runner.session.Run("docker", "network", "disconnect", control.network, control.upstreamID); result.ExitCode != 0 {
		remember(fmt.Errorf("disconnect upstream alias failed with exit code %d", result.ExitCode))
	}
	if result := control.runner.session.Run("docker", "network", "connect", "--alias", control.service, control.network, control.upstreamID); result.ExitCode != 0 {
		remember(fmt.Errorf("restore %s network alias failed with exit code %d", control.service, result.ExitCode))
	}
	if len(control.services) > 0 {
		up := append([]string{"up", "--detach", "--wait", "--wait-timeout", "420", "--force-recreate", "--no-deps"}, control.services...)
		if err := control.runner.compose(up...); err != nil {
			remember(err)
		}
	}
	return first
}

func numericStatus(value map[string]any, name string) int64 {
	if number, ok := value[name].(float64); ok {
		return int64(number)
	}
	return 0
}

func stringStatus(value map[string]any, name string) string {
	if text, ok := value[name].(string); ok {
		return text
	}
	return ""
}

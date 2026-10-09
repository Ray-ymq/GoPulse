package scenario

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/Ray-ymq/GoPulse/acceptance/internal/harness"
)

// Lifecycle exercises the immutable lifecycle tool through the Bundle's own
// Compose entry. The product containers are therefore created by the same
// host-socket, read-only tool contract used by an installed release.
func Lifecycle(root string, options Options) (returnErr error) {
	if options.Install == "" {
		options.Install = "clean"
	}
	if options.Install != "clean" && options.Install != "reuse" {
		return fmt.Errorf("install must be clean or reuse")
	}
	platform := options.Platform
	if platform == "" {
		platform = "linux/amd64"
	}
	if platform != "linux/amd64" {
		return fmt.Errorf("lifecycle acceptance supports linux/amd64 only")
	}
	if options.Manifest == "" {
		return fmt.Errorf("lifecycle requires --manifest")
	}
	manifest, err := filepath.Abs(options.Manifest)
	if err != nil {
		return err
	}
	bundle := filepath.Dir(manifest)
	if err := requireBundle(manifest, bundle); err != nil {
		return err
	}
	if err := validateManifestIdentity(manifest, root); err != nil {
		return err
	}

	session, err := harness.New(root, options.Keep)
	if err != nil {
		return err
	}
	runner, err := newLifecycleRunner(session, bundle, options.Install)
	if err != nil {
		_ = session.Cleanup()
		return err
	}
	defer func() {
		if !options.Keep {
			if cleanupErr := runner.purge(); returnErr == nil && cleanupErr != nil {
				returnErr = cleanupErr
			}
		}
		if cleanupErr := session.Cleanup(); returnErr == nil && cleanupErr != nil {
			returnErr = cleanupErr
		}
		if options.Receipt != "" {
			if receiptErr := session.WriteReceipt(resolvePath(root, options.Receipt)); returnErr == nil && receiptErr != nil {
				returnErr = receiptErr
			}
		}
	}()

	if options.Install == "reuse" {
		returnErr = runner.reuse(options.Install)
	} else {
		returnErr = runner.clean()
	}
	if returnErr == nil {
		session.SetStatus("passed", nil)
	} else {
		session.SetStatus("failed", returnErr)
	}
	return returnErr
}

type lifecycleRunner struct {
	session *harness.Session
	bundle  string
	install string
	project string
	port    string
	state   map[string]any
}

func newLifecycleRunner(session *harness.Session, bundle, mode string) (*lifecycleRunner, error) {
	if mode == "reuse" {
		return nil, fmt.Errorf("reuse mode requires an existing installation path")
	}
	installRoot, err := os.MkdirTemp("", "gopulse lifecycle ")
	if err != nil {
		return nil, err
	}
	install := filepath.Join(installRoot, "installation with spaces")
	if err := os.Mkdir(install, 0o700); err != nil {
		_ = os.RemoveAll(installRoot)
		return nil, err
	}
	port, err := freePort()
	if err != nil {
		_ = os.RemoveAll(installRoot)
		return nil, err
	}
	socket := "/var/run/docker.sock"
	stat, err := os.Stat(socket)
	if err != nil {
		_ = os.RemoveAll(installRoot)
		return nil, fmt.Errorf("Docker socket is unavailable: %w", err)
	}
	socketStat, ok := stat.Sys().(*syscall.Stat_t)
	if !ok {
		_ = os.RemoveAll(installRoot)
		return nil, fmt.Errorf("cannot inspect Docker socket ownership")
	}
	for key, value := range map[string]string{
		"GOPULSE_BUNDLE_DIR":  bundle,
		"GOPULSE_INSTALL_DIR": install,
		"GOPULSE_TOOL_UID":    fmt.Sprintf("%d", os.Getuid()),
		"GOPULSE_TOOL_GID":    fmt.Sprintf("%d", os.Getgid()),
		"GOPULSE_SOCKET_GID":  fmt.Sprintf("%d", socketStat.Gid),
	} {
		session.SetValue(key, value)
	}
	return &lifecycleRunner{
		session: session,
		bundle:  bundle,
		install: install,
		project: "gopulse-lifecycle-" + session.Token,
		port:    fmt.Sprintf("%d", port),
	}, nil
}

func (r *lifecycleRunner) clean() error {
	if err := r.expect("doctor", 0, "--port", r.port); err != nil {
		return err
	}
	if err := r.expect("init", 0, "--port", r.port); err != nil {
		return err
	}
	if err := r.expect("init", 14, "--port", r.port); err != nil {
		return err
	}
	if err := r.expect("verify", 19); err != nil {
		return err
	}
	if err := r.expect("up", 0); err != nil {
		return err
	}
	before, err := r.snapshot()
	if err != nil {
		return err
	}
	if err := r.expect("verify", 0); err != nil {
		return err
	}
	after, err := r.snapshot()
	if err != nil {
		return err
	}
	if string(before.state) != string(after.state) || string(before.secrets) != string(after.secrets) {
		return fmt.Errorf("read-only lifecycle verify mutated installation state")
	}
	status, err := r.expectJSON("status", 0)
	if err != nil {
		return err
	}
	if phase, _ := status["phase"].(string); phase != "ready" {
		return fmt.Errorf("lifecycle status phase is %q, expected ready", phase)
	}
	edge, _ := status["edge"].(string)
	for _, path := range []string{"/", "/admin/", "/health"} {
		if err := checkHTTP(edge + path); err != nil {
			return err
		}
	}
	if err := r.expect("logs", 0, "--service", "backend", "--tail", "10"); err != nil {
		return err
	}
	if err := r.expect("logs", 2, "--service", "arbitrary-container"); err != nil {
		return err
	}
	if err := r.expect("down", 0); err != nil {
		return err
	}
	if err := r.expect("down", 0); err != nil {
		return err
	}
	if err := r.expect("up", 0); err != nil {
		return err
	}
	if err := r.expect("verify", 0); err != nil {
		return err
	}
	return r.expect("down", 0)
}

func (r *lifecycleRunner) reuse(_ string) error {
	return fmt.Errorf("reuse mode requires --install to name an existing installation")
}

type lifecycleSnapshot struct {
	state   []byte
	secrets []byte
}

func (r *lifecycleRunner) snapshot() (lifecycleSnapshot, error) {
	state, err := os.ReadFile(filepath.Join(r.install, "state.json"))
	if err != nil {
		return lifecycleSnapshot{}, err
	}
	secrets, err := os.ReadFile(filepath.Join(r.install, "secrets.json"))
	if err != nil {
		return lifecycleSnapshot{}, err
	}
	return lifecycleSnapshot{state: state, secrets: secrets}, nil
}

func (r *lifecycleRunner) expect(command string, code int, flags ...string) error {
	result := r.run(command, flags...)
	if result.ExitCode != code {
		return fmt.Errorf("lifecycle %s exited %d, expected %d", command, result.ExitCode, code)
	}
	if command == "init" && code == 0 {
		r.state, _ = readJSON(result.Stdout)
	}
	return nil
}

func (r *lifecycleRunner) expectJSON(command string, code int, flags ...string) (map[string]any, error) {
	result := r.run(command, flags...)
	if result.ExitCode != code {
		return nil, fmt.Errorf("lifecycle %s exited %d, expected %d", command, result.ExitCode, code)
	}
	value, err := readJSON(result.Stdout)
	if err != nil {
		return nil, fmt.Errorf("lifecycle %s returned invalid JSON: %w", command, err)
	}
	return value, nil
}

func (r *lifecycleRunner) run(command string, flags ...string) harness.Result {
	args := []string{
		"compose", "--project-name", r.project, "--file", filepath.Join(r.bundle, "compose.yaml"),
		"run", "--rm", "-T", "--no-deps", "lifecycle", command,
		"--install", r.install, "--bundle", "/bundle", "--endpoint", "unix:///var/run/docker.sock",
	}
	args = append(args, flags...)
	return r.session.Run("docker", args...)
}

func (r *lifecycleRunner) purge() error {
	if r.state == nil {
		return nil
	}
	project, _ := r.state["project"].(string)
	if project == "" {
		return nil
	}
	result := r.run("down", "--purge", "--confirm", project)
	if result.ExitCode != 0 {
		return fmt.Errorf("lifecycle cleanup exited %d", result.ExitCode)
	}
	return nil
}

func requireBundle(manifest, bundle string) error {
	for _, path := range []string{manifest, filepath.Join(bundle, "compose.yaml"), filepath.Join(bundle, "README.md"), filepath.Join(bundle, "checksums")} {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("lifecycle bundle asset is missing: %s", path)
		}
	}
	return nil
}

func validateManifestIdentity(path, root string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var manifest struct {
		Version  string `json:"version"`
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil || manifest.Version == "" || manifest.Revision == "" {
		return fmt.Errorf("invalid lifecycle manifest")
	}
	version, err := os.ReadFile(filepath.Join(root, "VERSION"))
	if err != nil || strings.TrimSpace(string(version)) != manifest.Version {
		return fmt.Errorf("lifecycle manifest version does not match VERSION")
	}
	return nil
}

func readJSON(output string) (map[string]any, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		var value map[string]any
		if json.Unmarshal([]byte(lines[index]), &value) == nil {
			return value, nil
		}
	}
	return nil, fmt.Errorf("no JSON object in command output")
}

func freePort() (int, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func checkHTTP(url string) error {
	response, err := (&http.Client{}).Get(url)
	if err != nil {
		return fmt.Errorf("lifecycle endpoint %s failed: %w", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("lifecycle endpoint %s returned HTTP %d", url, response.StatusCode)
	}
	return nil
}

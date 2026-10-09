package scenario

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

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
	runner, err := newLifecycleRunner(session, bundle, options.Install, options.InstallPath)
	if err != nil {
		_ = session.Cleanup()
		return err
	}
	defer func() {
		if !options.Keep {
			if cleanupErr := runner.purge(); returnErr == nil && cleanupErr != nil {
				returnErr = cleanupErr
			}
			if cleanupErr := runner.removeOwnedRoot(); returnErr == nil && cleanupErr != nil {
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
		if options.FailureMatrix {
			returnErr = fmt.Errorf("failure matrix requires a clean installation")
		} else {
			returnErr = runner.reuse()
		}
	} else if options.FailureMatrix {
		returnErr = runner.failureMatrix()
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
	session       *harness.Session
	bundle        string
	install       string
	project       string
	port          string
	state         map[string]any
	temporaryRoot string
	owned         bool
	endpoint      string
	socketGID     string
}

func newLifecycleRunner(session *harness.Session, bundle, mode, requestedPath string) (*lifecycleRunner, error) {
	if mode != "clean" && mode != "reuse" {
		return nil, fmt.Errorf("install must be clean or reuse")
	}
	installPath := strings.TrimSpace(requestedPath)
	if installPath == "" {
		installPath = strings.TrimSpace(os.Getenv("GOPULSE_LIFECYCLE_INSTALL"))
	}
	owned := false
	temporaryRoot := ""
	install := installPath
	if mode == "reuse" && install == "" {
		return nil, fmt.Errorf("reuse mode requires --install-path or GOPULSE_LIFECYCLE_INSTALL")
	}
	if install != "" && !filepath.IsAbs(install) {
		return nil, fmt.Errorf("lifecycle install path must be absolute")
	}
	if mode == "clean" && install == "" {
		var err error
		temporaryRoot, err = os.MkdirTemp("", "gopulse lifecycle ")
		if err != nil {
			return nil, err
		}
		install = filepath.Join(temporaryRoot, "installation with spaces")
		owned = true
	}
	if mode == "clean" {
		if err := os.Mkdir(install, 0o700); err != nil {
			if temporaryRoot != "" {
				_ = os.RemoveAll(temporaryRoot)
			}
			return nil, fmt.Errorf("create lifecycle install path: %w", err)
		}
	}
	if mode == "reuse" {
		if stat, err := os.Stat(install); err != nil || !stat.IsDir() {
			return nil, fmt.Errorf("reuse installation path must be an existing directory")
		}
	}
	port := 0
	if mode == "clean" {
		var err error
		port, err = freePort()
		if err != nil {
			if temporaryRoot != "" {
				_ = os.RemoveAll(temporaryRoot)
			}
			return nil, err
		}
	}
	socket := "/var/run/docker.sock"
	stat, err := os.Stat(socket)
	if err != nil {
		if temporaryRoot != "" {
			_ = os.RemoveAll(temporaryRoot)
		}
		return nil, fmt.Errorf("Docker socket is unavailable: %w", err)
	}
	socketStat, ok := stat.Sys().(*syscall.Stat_t)
	if !ok {
		if temporaryRoot != "" {
			_ = os.RemoveAll(temporaryRoot)
		}
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
		session:       session,
		bundle:        bundle,
		install:       install,
		project:       "gopulse-lifecycle-" + session.Token,
		port:          fmt.Sprintf("%d", port),
		temporaryRoot: temporaryRoot,
		owned:         owned,
		endpoint:      "unix:///var/run/docker.sock",
		socketGID:     fmt.Sprintf("%d", socketStat.Gid),
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

func (r *lifecycleRunner) reuse() error {
	beforeState, err := readPrivateJSON(filepath.Join(r.install, "state.json"))
	if err != nil {
		return fmt.Errorf("reuse installation state is invalid: %w", err)
	}
	restore, err := readPrivateJSON(filepath.Join(r.install, "restore-result.json"))
	if err != nil {
		return fmt.Errorf("reuse requires a successful restore-result.json: %w", err)
	}
	verified, _ := restore["facts_verified"].(bool)
	if !verified {
		return fmt.Errorf("reuse requires a restore result with facts_verified=true")
	}
	if _, err := os.Stat(filepath.Join(r.install, "restore-pending.json")); err == nil {
		return fmt.Errorf("reuse refuses an installation with restore-pending.json")
	} else if !os.IsNotExist(err) {
		return err
	}
	r.state = beforeState
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
	if _, err := r.expectJSON("status", 0); err != nil {
		return err
	}
	after, err := r.snapshot()
	if err != nil {
		return err
	}
	if string(before.state) != string(after.state) || string(before.secrets) != string(after.secrets) {
		return fmt.Errorf("read-only lifecycle verify mutated restored state")
	}
	for _, key := range []string{"project", "installation_token", "manifest_digest", "version", "edge_port"} {
		if beforeState[key] != afterStateValue(after.state, key) {
			return fmt.Errorf("restored identity changed on lifecycle reuse: %s", key)
		}
	}
	status, err := r.expectJSON("status", 0)
	if err != nil {
		return err
	}
	edge, _ := status["edge"].(string)
	for _, path := range []string{"/login", "/admin/"} {
		if err := checkHTML(edge + path); err != nil {
			return err
		}
	}
	return nil
}

// failureMatrix reproduces the lifecycle failure contract at the same native
// boundary as the clean install scenario. Every injected fault is scoped to
// this runner's installation, project, or bundle copy.
func (r *lifecycleRunner) failureMatrix() error {
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

	invalid, err := os.MkdirTemp("", "gopulse invalid bundle ")
	if err != nil {
		return err
	}
	defer os.RemoveAll(invalid)
	if err := copyTree(r.bundle, invalid); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(invalid, "README.md"), []byte("tampered\n"), 0o644); err != nil {
		return err
	}
	r.session.SetValue("GOPULSE_BUNDLE_DIR", invalid)
	if err := r.expect("doctor", 10, "--port", r.port); err != nil {
		return err
	}
	r.session.SetValue("GOPULSE_BUNDLE_DIR", r.bundle)

	r.endpoint = "unix:///missing-daemon"
	if err := r.expect("doctor", 11, "--port", r.port); err != nil {
		return err
	}
	r.endpoint = "unix:///var/run/docker.sock"

	if err := r.expectDirect("doctor", 13, true, "--port", r.port); err != nil {
		return err
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:"+r.port)
	if err != nil {
		return err
	}
	if err := r.expect("doctor", 15, "--port", r.port); err != nil {
		listener.Close()
		return err
	}
	listener.Close()

	if err := os.Chmod(r.install, 0o755); err != nil {
		return err
	}
	if err := r.expect("status", 14); err != nil {
		_ = os.Chmod(r.install, 0o700)
		return err
	}
	if err := os.Chmod(r.install, 0o700); err != nil {
		return err
	}
	if err := r.expect("down", 2, "--purge", "--confirm", "foreign"); err != nil {
		return err
	}

	lock, err := os.OpenFile(filepath.Join(r.install, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return err
	}
	if err := r.expect("up", 16); err != nil {
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		lock.Close()
		return err
	}
	_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	lock.Close()

	foreign := r.projectName() + "_mysql_data"
	created := r.session.Run("docker", "volume", "create", foreign)
	if created.ExitCode != 0 {
		return fmt.Errorf("create foreign volume failed with exit code %d", created.ExitCode)
	}
	if err := r.expect("up", 17); err != nil {
		_ = r.session.Run("docker", "volume", "rm", foreign)
		return err
	}
	removed := r.session.Run("docker", "volume", "rm", foreign)
	if removed.ExitCode != 0 {
		return fmt.Errorf("remove foreign volume failed with exit code %d", removed.ExitCode)
	}

	if err := r.signal("SIGINT"); err != nil {
		return err
	}
	if err := r.expect("down", 0); err != nil {
		return err
	}
	if err := r.expect("up", 0); err != nil {
		return err
	}
	if err := r.signal("SIGTERM"); err != nil {
		return err
	}
	if err := r.expect("down", 0); err != nil {
		return err
	}
	if err := r.expect("up", 0); err != nil {
		return err
	}

	mysql := r.session.Run("docker", "ps", "-q", "--filter", "label=com.docker.compose.project="+r.projectName(), "--filter", "label=com.docker.compose.service=mysql")
	if mysql.ExitCode != 0 || strings.TrimSpace(mysql.Stdout) == "" {
		return fmt.Errorf("find lifecycle MySQL container failed")
	}
	mysqlID := strings.Fields(mysql.Stdout)[0]
	paused := r.session.Run("docker", "pause", mysqlID)
	if paused.ExitCode != 0 {
		return fmt.Errorf("pause lifecycle dependency failed with exit code %d", paused.ExitCode)
	}
	upErr := r.expect("up", 18)
	unpaused := r.session.Run("docker", "unpause", mysqlID)
	if unpaused.ExitCode != 0 {
		return fmt.Errorf("unpause lifecycle dependency failed with exit code %d", unpaused.ExitCode)
	}
	if upErr != nil {
		return upErr
	}
	return r.expect("down", 0)
}

func (r *lifecycleRunner) projectName() string {
	if project, ok := r.state["project"].(string); ok && project != "" {
		return project
	}
	return "gopulse-unknown"
}

func (r *lifecycleRunner) expectDirect(command string, code int, lowDisk bool, flags ...string) error {
	result := r.direct(command, lowDisk, flags...)
	if result.ExitCode != code {
		return fmt.Errorf("lifecycle %s exited %d, expected %d", command, result.ExitCode, code)
	}
	return nil
}

func (r *lifecycleRunner) direct(command string, lowDisk bool, flags ...string) harness.Result {
	image, err := r.lifecycleImage()
	if err != nil {
		return harness.Result{ExitCode: 1, Stderr: err.Error()}
	}
	user := fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())
	if lowDisk {
		user = "0:0"
	}
	args := []string{"run", "--rm", "--network", "host", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--user", user, "--group-add", r.socketGID, "--tmpfs", "/tmp"}
	args = append(args, "-v", "/var/run/docker.sock:/var/run/docker.sock", "-v", r.bundle+":/bundle:ro")
	if lowDisk {
		args = append(args, "--tmpfs", r.install+":rw,size=1048576,mode=0700")
	} else {
		args = append(args, "-v", r.install+":"+r.install)
	}
	args = append(args, image, command, "--install", r.install, "--bundle", "/bundle", "--endpoint", r.endpoint)
	args = append(args, flags...)
	return r.session.Run("docker", args...)
}

func (r *lifecycleRunner) lifecycleImage() (string, error) {
	raw, err := os.ReadFile(filepath.Join(r.bundle, "compose.yaml"))
	if err != nil {
		return "", err
	}
	var document struct {
		Services map[string]struct {
			Image string `json:"image"`
		} `json:"services"`
	}
	if err := json.Unmarshal(raw, &document); err != nil || document.Services["lifecycle"].Image == "" {
		return "", fmt.Errorf("lifecycle bundle image is invalid")
	}
	return document.Services["lifecycle"].Image, nil
}

func (r *lifecycleRunner) signal(name string) error {
	container := "gopulse-lifecycle-signal-" + name[3:] + "-" + r.session.Token[:8]
	args := []string{"compose", "--project-name", r.project, "--file", filepath.Join(r.bundle, "compose.yaml"), "run", "--name", container, "--rm", "-T", "--no-deps", "lifecycle", "up", "--install", r.install, "--bundle", "/bundle", "--endpoint", r.endpoint}
	command := exec.Command("docker", args...)
	command.Dir = r.session.Root
	command.Env = r.session.Environment()
	if err := command.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	deadline := time.Now().Add(120 * time.Second)
	reached := false
	finished := false
	for time.Now().Before(deadline) {
		state, err := readPrivateJSON(filepath.Join(r.install, "state.json"))
		if err == nil && state["phase"] == "pull" && state["operation_id"] != r.state["operation_id"] {
			reached = true
			break
		}
		select {
		case <-done:
			finished = true
		default:
		}
		if finished {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !reached {
		if !finished {
			_ = command.Process.Kill()
			<-done
		}
		return fmt.Errorf("lifecycle up did not reach pull before signal injection")
	}
	kill := exec.Command("docker", "kill", "--signal", name, container)
	if output, err := kill.CombinedOutput(); err != nil {
		_ = command.Process.Kill()
		if !finished {
			<-done
		}
		return fmt.Errorf("send %s to lifecycle runner: %v", name, strings.TrimSpace(string(output)))
	}
	err := <-done
	if exit := exitCode(err); exit != 20 {
		return fmt.Errorf("lifecycle %s exited %d, expected 20", name, exit)
	}
	state, err := readPrivateJSON(filepath.Join(r.install, "state.json"))
	if err != nil || state["phase"] != "interrupted" {
		return fmt.Errorf("lifecycle %s did not persist interrupted state", name)
	}
	return nil
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode()
	}
	return 1
}

func copyTree(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(destination, data, 0o644)
	})
}

func afterStateValue(raw []byte, key string) any {
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	return value[key]
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
		state, err := readJSON(result.Stdout)
		if err != nil {
			return fmt.Errorf("lifecycle init returned invalid JSON: %w", err)
		}
		r.state = state
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
		"--install", r.install, "--bundle", "/bundle", "--endpoint", r.endpoint,
	}
	args = append(args, flags...)
	return r.session.Run("docker", args...)
}

func (r *lifecycleRunner) purge() error {
	if !r.owned {
		return nil
	}
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

func (r *lifecycleRunner) removeOwnedRoot() error {
	if !r.owned || r.temporaryRoot == "" {
		return nil
	}
	return os.RemoveAll(r.temporaryRoot)
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
	revision, err := commandOutput(root, "git", "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(revision) != manifest.Revision {
		return fmt.Errorf("lifecycle manifest revision does not match HEAD")
	}
	return nil
}

func commandOutput(root, name string, args ...string) (string, error) {
	command := exec.Command(name, args...)
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func readPrivateJSON(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return value, nil
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

func checkHTML(url string) error {
	response, err := (&http.Client{}).Get(url)
	if err != nil {
		return fmt.Errorf("lifecycle endpoint %s failed: %w", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("lifecycle endpoint %s returned HTTP %d", url, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil || !strings.Contains(strings.ToLower(string(body)), "<html") {
		return fmt.Errorf("lifecycle endpoint %s did not return an HTML document", url)
	}
	return nil
}

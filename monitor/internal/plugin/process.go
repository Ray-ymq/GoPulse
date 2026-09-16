package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Ray-ymq/GoPulse/componentmetrics"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

type runtimeProcess struct {
	cmd         *exec.Cmd
	done        chan error
	record      processRecord
	intentional atomic.Bool
}

func procStartTicks(pid int) (string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	text := string(data)
	index := strings.LastIndex(text, ") ")
	if index < 0 {
		return "", errors.New("invalid proc stat")
	}
	fields := strings.Fields(text[index+2:])
	if len(fields) < 20 {
		return "", errors.New("invalid proc stat")
	}
	return fields[19], nil
}
func processIdentity(pid int) (string, string, string, error) {
	executable, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return "", "", "", err
	}
	cwd, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid))
	if err != nil {
		return "", "", "", err
	}
	commandLine, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return "", "", "", err
	}
	return filepath.Clean(executable), filepath.Clean(cwd), string(commandLine), nil
}
func ownsProcess(record processRecord) bool {
	if record.PID <= 1 {
		return false
	}
	ticks, err := procStartTicks(record.PID)
	if err != nil || ticks != record.StartTicks {
		return false
	}
	executable, cwd, commandLine, err := processIdentity(record.PID)
	return err == nil && executable == filepath.Clean(record.ExecutablePath) && cwd == filepath.Clean(record.WorkingDirectory) && strings.Contains(commandLine, record.CommandLineMarker)
}
func processRecordPath(pluginDir string) string {
	return filepath.Join(pluginDir, "runtime", "process.json")
}
func loadProcessRecord(pluginDir string) (processRecord, error) {
	data, err := os.ReadFile(processRecordPath(pluginDir))
	if err != nil {
		return processRecord{}, err
	}
	var record processRecord
	if json.Unmarshal(data, &record) != nil {
		return processRecord{}, errors.New("invalid process record")
	}
	return record, nil
}
func saveProcessRecord(pluginDir string, record processRecord) error {
	data, _ := json.Marshal(record)
	return atomicWrite(processRecordPath(pluginDir), append(data, '\n'), 0600)
}
func removeProcessRecord(pluginDir string) { _ = os.Remove(processRecordPath(pluginDir)) }

func startProcess(ctx context.Context, pluginDir string, manifest Manifest, env map[string]string, healthURL string, startup time.Duration) (*runtimeProcess, error) {
	release := filepath.Join(pluginDir, "releases", manifest.Version)
	executable, err := filepath.Abs(filepath.Join(release, filepath.FromSlash(manifest.Entrypoint)))
	if err != nil {
		return nil, err
	}
	release, err = filepath.Abs(release)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(executable)
	cmd.Dir = release
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	entry, _ := LookupOfficial(manifest.ID)
	allowed := map[string]bool{"GOPULSE_RUNTIME_MODE": true, "GOPULSE_VERSION": true, "GOPULSE_REVISION": true}
	prefix := strings.ToUpper(entry.Source)
	for _, suffix := range []string{"HOST", "PORT", "MANAGEMENT_PORT", "PASSWORD", "USERNAME", "DB", "DATABASE", "VHOST", "EXPORTER_HTTP_HOST", "EXPORTER_HTTP_PORT", "EXPORTER_SCRAPE_TIMEOUT", "EXPORTER_CONNECT_TIMEOUT", "EXPORTER_SHUTDOWN_TIMEOUT"} {
		allowed[prefix+"_"+suffix] = true
	}
	if entry.Source == "kafka" {
		allowed["KAFKA_TOPIC"] = true
		allowed["KAFKA_CONSUMER_GROUP"] = true
	}
	for key, value := range env {
		if allowed[key] {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}

	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if runtimeContractManifest(manifest) {
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stdout
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	ticks, err := procStartTicks(cmd.Process.Pid)
	if err != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_, _ = cmd.Process.Wait()
		return nil, err
	}
	record := processRecord{PluginID: manifest.ID, Revision: env["_GOPULSE_REVISION"], PID: cmd.Process.Pid, StartTicks: ticks, ExecutablePath: executable, WorkingDirectory: release, CommandLineMarker: executable}
	if err = saveProcessRecord(pluginDir, record); err != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_, _ = cmd.Process.Wait()
		return nil, err
	}
	runtime := &runtimeProcess{cmd: cmd, done: make(chan error, 1), record: record}
	go func() { runtime.done <- cmd.Wait(); close(runtime.done) }()

	deadlineContext, cancel := context.WithTimeout(ctx, startup)
	defer cancel()
	client := &http.Client{Timeout: 500 * time.Millisecond}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		request, _ := componentmetrics.NewRequest(deadlineContext, http.MethodGet, healthURL, nil)
		response, requestErr := client.Do(request)
		if requestErr == nil {
			body, _ := io.ReadAll(io.LimitReader(response.Body, 256))
			response.Body.Close()
			if response.StatusCode == http.StatusOK && validProcessHealth(manifest, body) {
				// A response on the fixed port must not hide an immediately
				// exiting candidate (for example a retained failure fixture).
				select {
				case <-runtime.done:
					removeProcessRecord(pluginDir)
					return nil, errors.New("plugin process exited during startup")
				case <-time.After(20 * time.Millisecond):
					if ownsProcess(runtime.record) {
						return runtime, nil
					}
				}
			}
		}
		select {
		case <-deadlineContext.Done():
			_ = terminateProcess(runtime.record, 2*time.Second)
			select {
			case <-runtime.done:
			case <-time.After(2 * time.Second):
			}
			removeProcessRecord(pluginDir)
			return nil, deadlineContext.Err()
		case <-runtime.done:
			removeProcessRecord(pluginDir)
			return nil, errors.New("plugin process exited during startup")
		case <-ticker.C:
		}
	}
}
func terminateProcess(record processRecord, timeout time.Duration) error {
	if !ownsProcess(record) {
		return errors.New("plugin process ownership mismatch")
	}
	if err := syscall.Kill(-record.PID, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !ownsProcess(record) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ownsProcess(record) {
		return nil
	}
	if err := syscall.Kill(-record.PID, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	for range 40 {
		if !ownsProcess(record) {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return errors.New("plugin process did not stop")
}

// terminateProcessBounded never extends the process-wide deadline. A forced
// kill is a shutdown failure even when it successfully reaps the child.
func terminateProcessBounded(ctx context.Context, record processRecord, timeout time.Duration) error {
	if !ownsProcess(record) {
		return errors.New("plugin process ownership mismatch")
	}
	if err := syscall.Kill(-record.PID, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for ownsProcess(record) {
		select {
		case <-tick.C:
		case <-ctx.Done():
			_ = syscall.Kill(-record.PID, syscall.SIGKILL)
			return errors.New("plugin_shutdown_timeout")
		case <-timer.C:
			_ = syscall.Kill(-record.PID, syscall.SIGKILL)
			reap := time.NewTimer(time.Second)
			defer reap.Stop()
			for ownsProcess(record) {
				select {
				case <-tick.C:
				case <-ctx.Done():
					return errors.New("plugin_shutdown_timeout")
				case <-reap.C:
					return errors.New("plugin_shutdown_timeout")
				}
			}
			return errors.New("plugin_shutdown_timeout")
		}
	}
	return nil
}

func validateHealthPort(env map[string]string) error {
	port, err := strconv.Atoi(env["REDIS_EXPORTER_HTTP_PORT"])
	if err != nil || port < 1 || port > 65535 {
		return errors.New("invalid exporter port")
	}
	return nil
}

// Historical packages retain their original exact health response. Current
// official packages use the shared runtime wire contract on the same port.
func runtimeContractManifest(manifest Manifest) bool {
	var major, minor, patch int
	_, _ = fmt.Sscanf(manifest.Version, "%d.%d.%d", &major, &minor, &patch)
	return major > 1 || major == 1 && (minor > 14 || minor == 14 && patch >= 3)
}
func validProcessHealth(manifest Manifest, body []byte) bool {
	if !runtimeContractManifest(manifest) {
		return string(body) == `{"status":"ok","service":"`+manifest.ID+`"}`
	}
	var response struct {
		Status          string `json:"status"`
		ContractVersion string `json:"contract_version"`
	}
	return json.Unmarshal(body, &response) == nil && response.Status == "ok" && response.ContractVersion == componentmetrics.RuntimeContractVersion
}

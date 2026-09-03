package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	for _, key := range []string{"REDIS_HOST", "REDIS_PORT", "REDIS_PASSWORD", "REDIS_DB", "REDIS_EXPORTER_HTTP_HOST", "REDIS_EXPORTER_HTTP_PORT", "REDIS_EXPORTER_SCRAPE_TIMEOUT", "REDIS_EXPORTER_SHUTDOWN_TIMEOUT"} {
		if value, ok := env[key]; ok {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
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
	record := processRecord{PID: cmd.Process.Pid, StartTicks: ticks, ExecutablePath: executable, WorkingDirectory: release, CommandLineMarker: executable}
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
		request, _ := http.NewRequestWithContext(deadlineContext, http.MethodGet, healthURL, nil)
		response, requestErr := client.Do(request)
		if requestErr == nil {
			body, _ := io.ReadAll(io.LimitReader(response.Body, 256))
			response.Body.Close()
			if response.StatusCode == http.StatusOK && string(body) == `{"status":"ok","service":"redis-exporter"}` {
				return runtime, nil
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
func validateHealthPort(env map[string]string) error {
	port, err := strconv.Atoi(env["REDIS_EXPORTER_HTTP_PORT"])
	if err != nil || port < 1 || port > 65535 {
		return errors.New("invalid exporter port")
	}
	return nil
}

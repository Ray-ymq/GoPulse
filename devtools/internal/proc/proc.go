// Package proc runs foreground commands and owns background child processes.
//
// Ownership is proven by the process birth token: for a live PID the kernel
// start time read from /proc/<pid>/stat is compared with the recorded value, so
// a recycled PID is never mistaken for a process this workspace started.
package proc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/Ray-ymq/GoPulse/devtools/internal/workspace"
)

// Environment merges caller overrides over the calling process environment,
// which is what every owned child and one-shot command receives.
func Environment(overrides map[string]string) []string {
	merged := map[string]string{}
	for _, entry := range os.Environ() {
		if key, value, found := strings.Cut(entry, "="); found {
			merged[key] = value
		}
	}
	for key, value := range overrides {
		merged[key] = value
	}
	keys := make([]string, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	environment := make([]string, 0, len(keys))
	for _, key := range keys {
		environment = append(environment, key+"="+merged[key])
	}
	return environment
}

// BirthIdentity returns the kernel start time of a live, non-zombie PID.
func BirthIdentity(pid int) (string, bool) {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", false
	}
	closing := strings.LastIndex(string(raw), ")")
	if closing < 0 {
		return "", false
	}
	fields := strings.Fields(string(raw)[closing+1:])
	if len(fields) <= 19 {
		return "", false
	}
	if fields[0] == "Z" {
		return "", false
	}
	// fields[0] is the state field (field 3 of /proc/<pid>/stat), so the start
	// time (field 22) is fields[19].
	return fields[19], true
}

// Owned reports whether the record still refers to the process it started.
func Owned(record workspace.ProcessRecord) bool {
	if record.PID <= 0 {
		return false
	}
	birth, ok := BirthIdentity(record.PID)
	return ok && birth == record.Birth
}

// CheckPorts rejects any required loopback port that an unowned process holds.
// Ports used by this workspace's own live processes are accepted so a repeated
// startup stays idempotent.
func CheckPorts(ports []int, owned []workspace.ProcessRecord) error {
	ownedPIDs := []int{}
	for _, record := range owned {
		if Owned(record) {
			ownedPIDs = append(ownedPIDs, record.PID)
		}
	}
	unique := map[int]struct{}{}
	for _, port := range ports {
		unique[port] = struct{}{}
	}
	ordered := make([]int, 0, len(unique))
	for port := range unique {
		ordered = append(ordered, port)
	}
	sort.Ints(ordered)
	for _, port := range ordered {
		if port > 0 && len(ownedPIDs) > 0 {
			continue
		}
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			return fmt.Errorf("port %d is already in use by an unowned process", port)
		}
		listener.Close()
	}
	return nil
}

// Run executes a foreground command with inherited standard streams. The
// command must preserve its exit code: callers depend on failure propagation.
func Run(command []string, cwd string, overrides map[string]string, label string, timeout time.Duration) error {
	if len(command) == 0 {
		return fmt.Errorf("%s has no command", label)
	}
	var ctx context.Context
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), timeout)
		defer cancel()
	} else {
		ctx = context.Background()
	}
	process := exec.CommandContext(ctx, command[0], command[1:]...)
	process.Dir = cwd
	process.Env = Environment(overrides)
	process.Stdin = os.Stdin
	process.Stdout = os.Stdout
	process.Stderr = os.Stderr
	err := process.Run()
	if err == nil {
		return nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if ctx.Err() != nil && timeout > 0 {
			return fmt.Errorf("%s exceeded its %ds bound", label, int(timeout.Seconds()))
		}
		return fmt.Errorf("%s failed with exit code %d", label, exit.ExitCode())
	}
	return fmt.Errorf("%s could not start: %v", label, err)
}

// Capture runs a command and returns its standard output.
func Capture(command []string, cwd string, overrides map[string]string) (string, error) {
	process := exec.Command(command[0], command[1:]...)
	process.Dir = cwd
	process.Env = Environment(overrides)
	raw, err := process.Output()
	if err != nil {
		return "", fmt.Errorf("%s failed: %v", strings.Join(command, " "), err)
	}
	return strings.TrimSpace(string(raw)), nil
}

// Succeeds reports whether a command exits zero, discarding its output.
func Succeeds(command []string, cwd string, overrides map[string]string) bool {
	process := exec.Command(command[0], command[1:]...)
	process.Dir = cwd
	process.Env = Environment(overrides)
	process.Stdout = nil
	process.Stderr = nil
	return process.Run() == nil
}

// Spawn starts an owned background process in its own session, appends its
// output to a private log, and returns its record. The caller must record the
// returned record before the process can be considered owned state.
func Spawn(logPath, cwd string, command []string, overrides map[string]string, now string) (workspace.ProcessRecord, error) {
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return workspace.ProcessRecord{}, err
	}
	logHandle, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return workspace.ProcessRecord{}, err
	}
	devNull, err := os.OpenFile(os.DevNull, os.O_RDONLY, 0)
	if err != nil {
		logHandle.Close()
		return workspace.ProcessRecord{}, err
	}
	defer devNull.Close()

	process := exec.Command(command[0], command[1:]...)
	process.Dir = cwd
	process.Env = Environment(overrides)
	process.Stdin = devNull
	process.Stdout = logHandle
	process.Stderr = logHandle
	process.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := process.Start(); err != nil {
		logHandle.Close()
		return workspace.ProcessRecord{}, fmt.Errorf("start %s failed: %v", command[0], err)
	}
	logHandle.Close()
	// The child must not be waited for here: it is owned background state.
	go func() { _ = process.Wait() }()

	birth, ok := BirthIdentity(process.Process.Pid)
	record := workspace.ProcessRecord{
		Birth:     birth,
		Command:   command,
		Cwd:       cwd,
		Log:       logPath,
		PID:       process.Process.Pid,
		StartedAt: now,
	}
	if !ok {
		Terminate(record)
		return workspace.ProcessRecord{}, fmt.Errorf("could not record process identity for %s", command[0])
	}
	return record, nil
}

// Terminate stops one owned process group: SIGTERM, a bounded wait, then
// SIGKILL. A record that no longer matches a live process is left untouched.
func Terminate(record workspace.ProcessRecord) {
	if !Owned(record) {
		return
	}
	if err := syscall.Kill(-record.PID, syscall.SIGTERM); err != nil {
		return
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) && Owned(record) {
		time.Sleep(100 * time.Millisecond)
	}
	if Owned(record) {
		_ = syscall.Kill(-record.PID, syscall.SIGKILL)
	}
}

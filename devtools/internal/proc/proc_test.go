package proc

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/devtools/internal/workspace"
)

func spawnSleep(t *testing.T, seconds string) workspace.ProcessRecord {
	t.Helper()
	log := filepath.Join(t.TempDir(), "sleep.log")
	record, err := Spawn(log, t.TempDir(), []string{"sleep", seconds}, nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Terminate(record) })
	return record
}

func alive(pid int) bool {
	_, ok := BirthIdentity(pid)
	return ok
}

func TestBirthIdentityPreventsPIDReuseCleanup(t *testing.T) {
	record := spawnSleep(t, "30")
	if !Owned(record) {
		t.Fatal("freshly spawned process must be owned")
	}

	// A recycled PID would carry a different birth token: cleanup must not run.
	impostor := record
	impostor.Birth = "1"
	Terminate(impostor)
	if !alive(record.PID) {
		t.Fatal("a record whose birth token does not match must not terminate the process")
	}

	Terminate(record)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && alive(record.PID) {
		time.Sleep(50 * time.Millisecond)
	}
	if alive(record.PID) {
		t.Fatal("an owned process must be terminated")
	}
}

func TestZombieProcessIsNotOwned(t *testing.T) {
	// Start a short-lived process and deliberately do not reap it: a zombie must
	// never be reported as a live owned process.
	command := exec.Command("sh", "-c", "exit 0")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	pid := command.Process.Pid
	defer func() { _ = command.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := BirthIdentity(pid); !ok {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("exited process %d must not be reported as a live owned process", pid)
}

func TestProcessGroupStopIsBoundedAndOwned(t *testing.T) {
	dir := t.TempDir()
	childFile := filepath.Join(dir, "child.pid")
	command := []string{"sh", "-c", "sleep 30 & echo $! > " + childFile + "; wait"}
	record, err := Spawn(filepath.Join(dir, "group.log"), dir, command, nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	child := waitForPID(t, childFile)
	if !alive(child) {
		t.Fatal("expected the group member to be running")
	}

	Terminate(record)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && (alive(record.PID) || alive(child)) {
		time.Sleep(50 * time.Millisecond)
	}
	if alive(child) {
		t.Fatalf("terminating the owned group must stop group member %d", child)
	}
}

func TestCheckPortsRejectsUnownedOccupantWithoutTouchingIt(t *testing.T) {
	listener, port := listen(t)
	defer listener.Close()

	err := CheckPorts([]int{port}, nil)
	if err == nil {
		t.Fatal("a port held by an unowned process must be refused")
	}
	if !strings.Contains(err.Error(), "already in use by an unowned process") {
		t.Fatalf("unexpected error: %v", err)
	}
	connection, dialErr := net.DialTimeout("tcp", listener.Addr().String(), 2*time.Second)
	if dialErr != nil {
		t.Fatalf("the existing socket must stay usable: %v", dialErr)
	}
	connection.Close()
}

func TestCheckPortsAcceptsOwnedProcessAndFreePort(t *testing.T) {
	record := spawnSleep(t, "30")
	if err := CheckPorts([]int{65001}, []workspace.ProcessRecord{record}); err != nil {
		t.Fatalf("an owned live process may keep a port: %v", err)
	}
	if err := CheckPorts([]int{65002}, nil); err != nil {
		t.Fatalf("a free port must be accepted: %v", err)
	}
}

func TestEnvironmentMergesCallerAndOverrides(t *testing.T) {
	t.Setenv("GOPULSE_TEST_INHERITED", "inherited")
	environment := Environment(map[string]string{"GOPULSE_TEST_OVERRIDE": "override"})
	joined := strings.Join(environment, "\n")
	if !strings.Contains(joined, "GOPULSE_TEST_INHERITED=inherited") {
		t.Error("the caller environment must be inherited")
	}
	if !strings.Contains(joined, "GOPULSE_TEST_OVERRIDE=override") {
		t.Error("overrides must be added")
	}
	if !strings.Contains(joined, "PATH=") {
		t.Error("PATH must reach owned processes so Go, npm, and Docker stay reachable")
	}
}

func TestRunPropagatesExitCode(t *testing.T) {
	err := Run([]string{"sh", "-c", "exit 7"}, t.TempDir(), nil, "failing step", 0)
	if err == nil {
		t.Fatal("a non-zero exit must be reported")
	}
	if !strings.Contains(err.Error(), "failed with exit code 7") {
		t.Fatalf("the exit code must be preserved: %v", err)
	}
}

func TestRunReportsMissingProgram(t *testing.T) {
	err := Run([]string{"gopulse-program-that-does-not-exist"}, t.TempDir(), nil, "missing step", 0)
	if err == nil || !strings.Contains(err.Error(), "could not start") {
		t.Fatalf("a missing program must be reported as a start failure: %v", err)
	}
}

func TestCaptureTrimsOutput(t *testing.T) {
	value, err := Capture([]string{"sh", "-c", "printf 'value\\n'"}, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if value != "value" {
		t.Fatalf("captured value = %q", value)
	}
}

func spawnShortLived(t *testing.T) (int, error) {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "pid")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	script := "sleep 0.1 & echo $! > " + file.Name() + "; wait"
	if _, err := Spawn(filepath.Join(t.TempDir(), "short-lived.log"), t.TempDir(),
		[]string{"sh", "-c", script}, nil, "test"); err != nil {
		return 0, err
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(file.Name())
		if err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil && pid > 0 {
				return pid, nil
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return 0, os.ErrDeadlineExceeded
}

package plugin

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestForgedProcessRecordCannotSignalUnownedProcess(t *testing.T) {
	command := exec.Command("sleep", "30")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Kill(); _, _ = command.Process.Wait() }()
	executable, cwd, _, err := processIdentity(command.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	record := processRecord{PID: command.Process.Pid, StartTicks: "forged", ExecutablePath: executable, WorkingDirectory: cwd, CommandLineMarker: "sleep"}
	if ownsProcess(record) {
		t.Fatal("forged record matched process")
	}
	if err := terminateProcess(record, 100*time.Millisecond); err == nil {
		t.Fatal("terminateProcess accepted forged record")
	}
	if err := syscall.Kill(command.Process.Pid, 0); err != nil {
		t.Fatalf("unowned process was affected: %v", err)
	}
}
func TestProcessHealthAcceptsVersionedRuntimeContract(t *testing.T) {
	current := Manifest{ID: "redis-exporter", Version: "1.14.3"}
	legacy := Manifest{ID: "redis-exporter", Version: "1.9.4"}
	modern := []byte(`{"status":"ok","contract_version":"2"}`)
	old := []byte(`{"status":"ok","service":"redis-exporter"}`)
	if !validProcessHealth(current, modern) || !validProcessHealth(legacy, old) {
		t.Fatal("valid package health rejected")
	}
	if validProcessHealth(current, old) || validProcessHealth(legacy, modern) || validProcessHealth(current, []byte(`{"status":"ok","contract_version":"1"}`)) {
		t.Fatal("incompatible package health accepted")
	}
}

var testExecutableCache struct {
	sync.Mutex
	running []byte
	failing []byte
	err     error
}

func testExecutable(t *testing.T, running bool) []byte {
	t.Helper()
	testExecutableCache.Lock()
	defer testExecutableCache.Unlock()
	if testExecutableCache.running == nil && testExecutableCache.failing == nil && testExecutableCache.err == nil {
		_, source, _, _ := runtime.Caller(0)
		moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(source), "../.."))
		temporary, err := os.MkdirTemp("", "gopulse-test-helpers-")
		if err != nil {
			testExecutableCache.err = err
		} else {
			defer os.RemoveAll(temporary)
			testExecutableCache.running, testExecutableCache.err = buildTestExecutable(moduleRoot, temporary, "testhelper_running")
			if testExecutableCache.err == nil {
				testExecutableCache.failing, testExecutableCache.err = buildTestExecutable(moduleRoot, temporary, "testhelper_failing")
			}
		}
	}
	if testExecutableCache.err != nil {
		t.Fatal(testExecutableCache.err)
	}
	if running {
		return append([]byte(nil), testExecutableCache.running...)
	}
	return append([]byte(nil), testExecutableCache.failing...)
}

func buildTestExecutable(moduleRoot, temporary, packageName string) ([]byte, error) {
	output := filepath.Join(temporary, packageName)
	command := exec.Command("go", "build", "-trimpath", "-o", output, "./internal/plugin/"+packageName)
	command.Dir = moduleRoot
	command.Env = os.Environ()
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if err := command.Run(); err != nil {
		return nil, err
	}
	return os.ReadFile(output)
}

package plugin

import (
	"os"
	"os/exec"
	"path/filepath"
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
	executable, err := os.Readlink(filepath.Join("/proc", itoa(command.Process.Pid), "exe"))
	if err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Readlink(filepath.Join("/proc", itoa(command.Process.Pid), "cwd"))
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
func itoa(value int) string {
	const digits = "0123456789"
	if value == 0 {
		return "0"
	}
	var buffer [24]byte
	index := len(buffer)
	for value > 0 {
		index--
		buffer[index] = digits[value%10]
		value /= 10
	}
	return string(buffer[index:])
}

//go:build !linux && !darwin

package plugin

import (
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

func procStartTicks(pid int) (string, error) { return "", syscall.ENOSYS }

func processIdentity(pid int) (string, string, string, error) {
	return "", "", "", syscall.ENOSYS
}

func processExecutableIdentity(path string) string { return filepath.Clean(path) }

func configurePluginProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func processHealthGrace() time.Duration { return 100 * time.Millisecond }

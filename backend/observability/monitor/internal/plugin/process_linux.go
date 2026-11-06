//go:build linux

package plugin

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

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
	if len(fields) < 20 || fields[0] == "Z" {
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

func processExecutableIdentity(path string) string { return filepath.Clean(path) }

func configurePluginProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
}

func processHealthGrace() time.Duration { return 20 * time.Millisecond }

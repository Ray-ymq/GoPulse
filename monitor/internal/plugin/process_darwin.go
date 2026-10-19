//go:build darwin

package plugin

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func procStartTicks(pid int) (string, error) {
	if pid <= 0 {
		return "", errors.New("invalid process id")
	}
	process, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || int(process.Proc.P_pid) != pid || process.Proc.P_stat == 5 {
		return "", errors.New("process is not live")
	}
	start := process.Proc.P_starttime
	return fmt.Sprintf("%d.%06d", start.Sec, start.Usec), nil
}

func processIdentity(pid int) (string, string, string, error) {
	commandLine, err := commandOutput("ps", "-p", strconv.Itoa(pid), "-o", "command=")
	if err != nil {
		return "", "", "", err
	}
	fields := strings.Fields(commandLine)
	if len(fields) == 0 {
		return "", "", "", errors.New("process command is empty")
	}
	cwd, err := processWorkingDirectory(pid)
	if err != nil {
		return "", "", "", err
	}
	return processExecutableIdentity(fields[0]), filepath.Clean(cwd), commandLine, nil
}

func processWorkingDirectory(pid int) (string, error) {
	output, err := commandOutput("lsof", "-a", "-p", strconv.Itoa(pid), "-d", "cwd", "-Fn")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "n") && len(line) > 1 {
			return strings.TrimSpace(line[1:]), nil
		}
	}
	return "", errors.New("process working directory is unavailable")
}

func commandOutput(command string, args ...string) (string, error) {
	output, err := exec.Command(command, args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func processExecutableIdentity(path string) string {
	return filepath.Base(path)
}

func configurePluginProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func processHealthGrace() time.Duration { return 500 * time.Millisecond }

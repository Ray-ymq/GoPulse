//go:build darwin

package proc

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// BirthIdentity returns the kernel start time of a live, non-zombie PID.
// Darwin does not expose Linux's /proc filesystem. Its kern.proc sysctl
// provides the same identity with microsecond precision.
func BirthIdentity(pid int) (string, bool) {
	if pid <= 0 {
		return "", false
	}
	process, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	// Darwin encodes SZOMB as 5 in struct extern_proc.p_stat.
	if err != nil || int(process.Proc.P_pid) != pid || process.Proc.P_stat == 5 {
		return "", false
	}
	start := process.Proc.P_starttime
	return fmt.Sprintf("%d.%06d", start.Sec, start.Usec), true
}

//go:build linux

package proc

import (
	"fmt"
	"os"
	"strings"
)

// BirthIdentity returns the kernel start time of a live, non-zombie PID.
func BirthIdentity(pid int) (string, bool) {
	if pid <= 0 {
		return "", false
	}
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", false
	}
	value := string(raw)
	closing := strings.LastIndex(value, ")")
	if closing < 0 {
		return "", false
	}
	fields := strings.Fields(value[closing+1:])
	if len(fields) <= 19 || fields[0] == "Z" {
		return "", false
	}
	// fields[0] is the state field (field 3 of /proc/<pid>/stat), so the start
	// time (field 22) is fields[19].
	return fields[19], true
}

//go:build !linux && !darwin

package proc

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// BirthIdentity uses the host ps implementation on unsupported Unix targets.
// Linux and Darwin use kernel APIs with stronger identity guarantees.
func BirthIdentity(pid int) (string, bool) {
	if pid <= 0 {
		return "", false
	}
	output, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "lstart=").Output()
	if err != nil {
		return "", false
	}
	value := strings.TrimSpace(string(output))
	if value == "" {
		return "", false
	}
	return fmt.Sprintf("%d:%s", pid, value), true
}

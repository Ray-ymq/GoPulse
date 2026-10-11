package harness

import (
	"fmt"
	"net"
	"strconv"

	"github.com/Ray-ymq/GoPulse/acceptance/internal/contracts"
)

// CheckPortAvailable checks a caller-selected loopback publication before a
// Compose project is created. Port zero is intentionally rejected here because
// acceptance callers need a deterministic conflict check; Compose sessions may
// still use zero internally when they do not publish a host port.
func CheckPortAvailable(host string, port int) error {
	if err := contracts.ValidateLoopback(host); err != nil {
		return err
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("port must be an integer from 1 to 65535")
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return fmt.Errorf("loopback port %d is unavailable: %w", port, err)
	}
	return listener.Close()
}

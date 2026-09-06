package config

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

type RuntimeMode string

const (
	RuntimeModeHost      RuntimeMode = "host"
	RuntimeModeContainer RuntimeMode = "container"
)

func loadRuntimeMode(lookup func(string) (string, bool)) (RuntimeMode, error) {
	mode := RuntimeModeHost
	if value, ok := lookup("GOPULSE_RUNTIME_MODE"); ok && strings.TrimSpace(value) != "" {
		mode = RuntimeMode(strings.ToLower(strings.TrimSpace(value)))
	}
	if mode != RuntimeModeHost && mode != RuntimeModeContainer {
		return "", errors.New("GOPULSE_RUNTIME_MODE must be host or container")
	}
	return mode, nil
}

func validateListenHost(mode RuntimeMode, key, host string) error {
	ip := net.ParseIP(strings.TrimSpace(host))
	if ip == nil {
		return fmt.Errorf("%s must be an IP address", key)
	}
	if mode == RuntimeModeHost && !ip.IsLoopback() {
		return fmt.Errorf("%s must use a loopback IP address in host mode", key)
	}
	if mode == RuntimeModeContainer && !ip.IsUnspecified() {
		return fmt.Errorf("%s must use an unspecified IP address in container mode", key)
	}
	return nil
}

func validateDependencyHost(mode RuntimeMode, key, host string) error {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" || strings.ContainsAny(host, "\x00\r\n\t /\\@") {
		return fmt.Errorf("%s must contain a valid host", key)
	}
	if mode == RuntimeModeHost {
		if host == "localhost" || (net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()) {
			return nil
		}
		return fmt.Errorf("%s must use a loopback host in host mode", key)
	}
	if host == "localhost" || host == "host.docker.internal" || net.ParseIP(host) != nil || !isServiceDNSName(host) {
		return fmt.Errorf("%s must use a service DNS name in container mode", key)
	}
	return nil
}

func isServiceDNSName(host string) bool {
	if len(host) > 253 || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' && r != '_' {
				return false
			}
		}
	}
	return true
}

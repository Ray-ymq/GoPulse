package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

type RuntimeMode string

const (
	RuntimeModeHost      RuntimeMode = "host"
	RuntimeModeContainer RuntimeMode = "container"
)

func loadRuntimeMode(lookup LookupFunc) (RuntimeMode, error) {
	mode := RuntimeMode(strings.ToLower(valueOrDefault(lookup, "GOPULSE_RUNTIME_MODE", string(RuntimeModeHost))))
	switch mode {
	case RuntimeModeHost, RuntimeModeContainer:
		return mode, nil
	default:
		return "", errors.New("GOPULSE_RUNTIME_MODE must be host or container")
	}
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
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" || strings.ContainsAny(host, "\x00\r\n\t /\\@") {
		return fmt.Errorf("%s must contain a valid host", key)
	}
	if mode == RuntimeModeHost {
		if host == "localhost" {
			return nil
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return nil
		}
		return fmt.Errorf("%s must use a loopback host in host mode", key)
	}
	if host == "localhost" || host == "host.docker.internal" || net.ParseIP(host) != nil || !isServiceDNSName(host) {
		return fmt.Errorf("%s must use a service DNS name in container mode", key)
	}
	return nil
}

func validateOriginHost(mode RuntimeMode, key string, parsed *url.URL) error {
	if parsed == nil {
		return fmt.Errorf("%s must contain a valid origin", key)
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return fmt.Errorf("%s must not include a path", key)
	}
	return validateDependencyHost(mode, key, parsed.Hostname())
}

func isServiceDNSName(host string) bool {
	if len(host) > 253 || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' && character != '_' {
				return false
			}
		}
	}
	return true
}

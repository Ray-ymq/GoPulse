// Package contracts contains small, dependency-free checks for acceptance
// resource and output boundaries. Runtime orchestration lives in harness.
package contracts

import (
	"fmt"
	"regexp"
	"strings"
)

var projectName = regexp.MustCompile(`^gopulse-accept-[a-f0-9]{12}$`)

func ValidProjectName(value string) bool { return projectName.MatchString(value) }

func ValidLoopback(value string) bool { return value == "127.0.0.1" }

func ValidateProjectName(value string) error {
	if !ValidProjectName(value) {
		return fmt.Errorf("unsafe acceptance project name: %q", value)
	}
	return nil
}

func ValidateLoopback(value string) error {
	if !ValidLoopback(value) {
		return fmt.Errorf("acceptance publication must use literal IPv4 loopback: %q", value)
	}
	return nil
}

func ContainsSecret(value string, secrets ...string) bool {
	for _, secret := range secrets {
		if secret != "" && strings.Contains(value, secret) {
			return true
		}
	}
	return false
}

func Redact(value string, secrets ...string) string {
	for _, secret := range secrets {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	return value
}

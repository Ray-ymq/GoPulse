package runtimekit

import (
	"context"
	"errors"
	"os"
	"strings"
)

const (
	maxInstanceIDBytes         = 64
	maxReplicaEndpoints        = 8
	RuntimeIdentityEnvironment = "GOPULSE_INSTANCE_ID"
)

func Mode() string {
	mode := os.Getenv("GOPULSE_RUNTIME_MODE")
	if mode == "" {
		return "host"
	}
	return mode
}

// InstanceID returns the bounded, non-sensitive identity used by logs and
// acceptance evidence. It is deliberately not an authorization input. The
// typed loaders reject an invalid value; this accessor keeps logging construction deterministic when a caller has not loaded config.
func InstanceID(component string) string {
	value := strings.TrimSpace(os.Getenv(RuntimeIdentityEnvironment))
	if ValidateInstanceID(value) == nil {
		return value
	}
	return component + "-local"
}

// ValidateInstanceID enforces a finite, DNS-safe identity vocabulary. A
// process identity may be supplied by Compose, but it may never contain
// credentials, whitespace, or arbitrary caller data.
func ValidateInstanceID(value string) error {
	if value == "" || len(value) > maxInstanceIDBytes || strings.TrimSpace(value) != value {
		return errors.New("instance identity must be 1-64 bytes")
	}
	for index, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '-' {
			if index == 0 && character == '-' {
				return errors.New("instance identity must start with a lowercase letter or digit")
			}
			continue
		}
		return errors.New("instance identity must use lowercase letters, digits, and hyphens")
	}
	if value[len(value)-1] == '-' {
		return errors.New("instance identity must not end with a hyphen")
	}
	return nil
}

func StartProbes(ctx context.Context, id string, probes *Probes) (*Listener, error) {
	if probes == nil {
		return nil, errors.New("健康检查不能为空")
	}
	listener, err := Start(ctx, Mode(), id, probes)
	if err == nil {
		listener.probes = probes
	}
	return listener, err
}

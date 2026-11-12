package runtimekit

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

// RuntimeDefinitions is the closed process inventory. Ports are private process
// probe ports; the remaining listener and environment inventory lives in the
// versioned deployment contract, not a second runtime configuration source.
type RuntimeDefinition struct {
	Port            int
	ShutdownSeconds int
}

var RuntimeDefinitions = map[string]RuntimeDefinition{
	"backend": {8080, 5}, "business-worker": {19102, 10}, "search-indexer": {19103, 10},
}

func runtimeComponentID(component string) string {

	return component
}

// ValidateRuntimeEnvironment validates only common process-boundary rules.
// Component loaders retain ownership of their business defaults, URLs and ranges.
func ValidateRuntimeEnvironment(component string) error {
	definition, ok := RuntimeDefinitions[component]
	if !ok {
		return errors.New("invalid_configuration: component")
	}
	mode := Mode()
	if mode != "host" && mode != "container" {
		return errors.New("invalid_configuration: GOPULSE_RUNTIME_MODE")
	}
	if raw := strings.TrimSpace(os.Getenv("GOPULSE_INSTANCE_ID")); raw != "" {
		if err := ValidateInstanceID(raw); err != nil {
			return errors.New("invalid_configuration: GOPULSE_INSTANCE_ID")
		}
	}
	if raw := strings.TrimSpace(os.Getenv("GOPULSE_REPLICA_COUNT")); raw != "" {
		count, err := strconv.Atoi(raw)
		if err != nil || count < 1 || count > 8 {
			return errors.New("invalid_configuration: GOPULSE_REPLICA_COUNT")
		}
	}
	componentID := runtimeComponentID(component)
	prefix := strings.ToUpper(strings.ReplaceAll(componentID, "-", "_"))
	if raw := os.Getenv(prefix + "_SHUTDOWN_TIMEOUT"); raw != "" {
		budget, err := time.ParseDuration(raw)
		if err != nil || budget <= 0 || budget > time.Duration(definition.ShutdownSeconds)*time.Second {
			return errors.New("invalid_configuration: " + prefix + "_SHUTDOWN_TIMEOUT")
		}
	}
	keys := []string{}
	if componentID == "backend" {
		keys = append(keys, "AUTH_JWT_SECRET")
	}
	seen := map[string]bool{}
	for _, key := range keys {
		value := os.Getenv(key)
		if value == "" {
			continue
		} // requiredness remains with typed loaders
		if len(value) < 32 || strings.ContainsAny(value, " \t\r\n") || seen[value] {
			return errors.New("invalid_configuration: " + key)
		}
		seen[value] = true
	}
	return nil
}

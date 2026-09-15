package componentmetrics

import (
	"errors"
	"os"
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
	"router": {9091, 10}, "marshaller": {9093, 10}, "monitor": {9090, 10},
	"redis-exporter": {9121, 5}, "mysql-exporter": {9122, 5}, "rabbitmq-exporter": {9123, 5},
	"kafka-exporter": {9124, 5}, "elasticsearch-exporter": {9125, 5}, "victoriametrics-exporter": {9126, 5},
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
	prefix := strings.ToUpper(strings.ReplaceAll(component, "-", "_"))
	if raw := os.Getenv(prefix + "_SHUTDOWN_TIMEOUT"); raw != "" {
		budget, err := time.ParseDuration(raw)
		if err != nil || budget <= 0 || budget > time.Duration(definition.ShutdownSeconds)*time.Second {
			return errors.New("invalid_configuration: " + prefix + "_SHUTDOWN_TIMEOUT")
		}
	}
	for key, maxSeconds := range map[string]int{"ROUTER_REQUEST_TIMEOUT": 5, "MARSHALLER_READINESS_TIMEOUT": 2} {
		if !strings.HasPrefix(key, prefix+"_") {
			continue
		}
		if raw := os.Getenv(key); raw != "" {
			d, err := time.ParseDuration(raw)
			if err != nil || d <= 0 || d > time.Duration(maxSeconds)*time.Second {
				return errors.New("invalid_configuration: " + key)
			}
		}
	}
	keys := []string{TokenKey(component)}
	switch component {
	case "backend":
		keys = append(keys, "AUTH_JWT_SECRET", "LOG_MONITOR_INGEST_TOKEN", "MONITOR_API_TOKEN")
	case "business-worker", "search-indexer":
		keys = append(keys, "LOG_MONITOR_INGEST_TOKEN")
	case "router":
		keys = append(keys, "ROUTER_API_TOKEN")
	case "marshaller":
		keys = append(keys, "MARSHALLER_API_TOKEN")
	case "monitor":
		keys = append(keys, "MONITOR_API_TOKEN", "MONITOR_ROUTER_TOKEN", "LOG_MONITOR_INGEST_TOKEN")
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

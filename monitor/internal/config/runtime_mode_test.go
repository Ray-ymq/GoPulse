package config

import (
	"strings"
	"testing"
)

func monitorEnvironment(t *testing.T) map[string]string {
	return map[string]string{
		"MONITOR_API_TOKEN":        "monitor-token-at-least-32-bytes-long",
		"LOG_MONITOR_INGEST_TOKEN": "log-token-distinct-at-least-32-bytes",
		"MONITOR_PLUGIN_ROOT":      t.TempDir(),
		"REDIS_HOST":               "127.0.0.1",
		"REDIS_PORT":               "6379",
		"REDIS_DB":                 "0",
		"REDIS_EXPORTER_HTTP_HOST": "127.0.0.1",
		"REDIS_EXPORTER_HTTP_PORT": "9121",
	}
}

func TestRuntimeModeKeepsMonitorHostSafeAndAcceptsContainerDNS(t *testing.T) {
	host := monitorEnvironment(t)
	if cfg, err := LoadFrom(mapLookup(host)); err != nil || cfg.RuntimeMode != RuntimeModeHost {
		t.Fatalf("host config=%+v error=%v", cfg, err)
	}
	host["MONITOR_HTTP_HOST"] = "0.0.0.0"
	if _, err := LoadFrom(mapLookup(host)); err == nil || !strings.Contains(err.Error(), "host mode") {
		t.Fatalf("host wildcard error=%v", err)
	}

	container := monitorEnvironment(t)
	container["GOPULSE_RUNTIME_MODE"] = "container"
	container["MONITOR_HTTP_HOST"] = "0.0.0.0"
	container["REDIS_HOST"] = "redis"
	cfg, err := LoadFrom(mapLookup(container))
	if err != nil {
		t.Fatalf("container config error=%v", err)
	}
	if cfg.RuntimeMode != RuntimeModeContainer || cfg.HTTPHost != "0.0.0.0" {
		t.Fatalf("container config=%+v", cfg)
	}
}

func TestRuntimeModeRejectsUnsafeMonitorOrigins(t *testing.T) {
	for _, test := range []struct{ key, value string }{
		{"GOPULSE_RUNTIME_MODE", "cluster"},
		{"REDIS_HOST", "127.0.0.1"},
		{"REDIS_HOST", "host.docker.internal"},
		{"REDIS_EXPORTER_HTTP_HOST", "0.0.0.0"},
		{"MONITOR_BOOTSTRAP_PACKAGE", "relative/package.tar.gz"},
	} {
		t.Run(test.key+test.value, func(t *testing.T) {
			env := monitorEnvironment(t)
			env["GOPULSE_RUNTIME_MODE"] = "container"
			env["MONITOR_HTTP_HOST"] = "0.0.0.0"
			env["REDIS_HOST"] = "redis"
			env[test.key] = test.value
			_, err := LoadFrom(mapLookup(env))
			if err == nil || !strings.Contains(err.Error(), test.key) {
				t.Fatalf("error=%v, want %s rejection", err, test.key)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatalf("configuration error leaked credential: %v", err)
			}
		})
	}
}

func mapLookup(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}

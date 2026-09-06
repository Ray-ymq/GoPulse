package config

import (
	"strings"
	"testing"
)

func TestRuntimeModeKeepsRouterHostSafeAndAcceptsContainerDNS(t *testing.T) {
	host := map[string]string{"ROUTER_API_TOKEN": "router-token-at-least-32-bytes-long"}
	lookup := func(key string) (string, bool) { value, ok := host[key]; return value, ok }
	if cfg, err := LoadFrom(lookup); err != nil || cfg.RuntimeMode != RuntimeModeHost {
		t.Fatalf("host config=%+v error=%v", cfg, err)
	}
	host["ROUTER_HTTP_HOST"] = "0.0.0.0"
	if _, err := LoadFrom(lookup); err == nil || !strings.Contains(err.Error(), "host mode") {
		t.Fatalf("host wildcard error=%v", err)
	}

	container := map[string]string{
		"GOPULSE_RUNTIME_MODE": "container",
		"ROUTER_HTTP_HOST":     "0.0.0.0",
		"ROUTER_API_TOKEN":     "router-token-at-least-32-bytes-long",
		"ROUTER_KAFKA_BROKERS": "kafka:19092",
	}
	cfg, err := LoadFrom(func(key string) (string, bool) { value, ok := container[key]; return value, ok })
	if err != nil || cfg.RuntimeMode != RuntimeModeContainer || cfg.KafkaBrokers[0] != "kafka:19092" {
		t.Fatalf("container config=%+v error=%v", cfg, err)
	}
}

func TestRuntimeModeRejectsUnsafeRouterContainerValues(t *testing.T) {
	for key, value := range map[string]string{
		"GOPULSE_RUNTIME_MODE": "cluster",
		"ROUTER_HTTP_HOST":     "127.0.0.1",
		"ROUTER_KAFKA_BROKERS": "127.0.0.1:19092",
	} {
		t.Run(key, func(t *testing.T) {
			env := map[string]string{
				"GOPULSE_RUNTIME_MODE": "container",
				"ROUTER_HTTP_HOST":     "0.0.0.0",
				"ROUTER_API_TOKEN":     "router-token-at-least-32-bytes-long",
				"ROUTER_KAFKA_BROKERS": "kafka:19092",
			}
			env[key] = value
			_, err := LoadFrom(func(name string) (string, bool) { v, ok := env[name]; return v, ok })
			if err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("error=%v, want %s rejection", err, key)
			}
		})
	}
}

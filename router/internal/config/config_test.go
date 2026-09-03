package config

import (
	"strings"
	"testing"
)

func TestLoadFromValidatesRouterConfiguration(t *testing.T) {
	values := map[string]string{
		"ROUTER_API_TOKEN": "0123456789abcdef0123456789abcdef",
	}
	lookup := func(key string) (string, bool) { value, ok := values[key]; return value, ok }
	cfg, err := LoadFrom(lookup)
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}
	if cfg.Address() != "127.0.0.1:9091" || cfg.KafkaTopic != Topic || len(cfg.KafkaBrokers) != 1 {
		t.Fatalf("unexpected config: %+v", cfg)
	}

	for name, value := range map[string]string{
		"short token":   "short",
		"token newline": strings.Repeat("x", 32) + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			values["ROUTER_API_TOKEN"] = value
			if _, err := LoadFrom(lookup); err == nil {
				t.Fatal("LoadFrom() accepted invalid token")
			}
		})
	}
}

func TestLoadFromRejectsInvalidKafkaBounds(t *testing.T) {
	base := map[string]string{"ROUTER_API_TOKEN": "0123456789abcdef0123456789abcdef"}
	for name, pair := range map[string][2]string{
		"topic":            {"ROUTER_KAFKA_TOPIC", "client-topic"},
		"broker":           {"ROUTER_KAFKA_BROKERS", "not-a-broker"},
		"duplicate broker": {"ROUTER_KAFKA_BROKERS", "127.0.0.1:9092,127.0.0.1:9092"},
		"produce timeout":  {"ROUTER_KAFKA_PRODUCE_TIMEOUT", "5s"},
		"small buffer":     {"ROUTER_KAFKA_MAX_BUFFERED_BYTES", "1048575"},
	} {
		t.Run(name, func(t *testing.T) {
			values := make(map[string]string, len(base)+1)
			for key, value := range base {
				values[key] = value
			}
			values[pair[0]] = pair[1]
			lookup := func(key string) (string, bool) { value, ok := values[key]; return value, ok }
			if _, err := LoadFrom(lookup); err == nil {
				t.Fatalf("LoadFrom() accepted %s=%q", pair[0], pair[1])
			}
		})
	}
}

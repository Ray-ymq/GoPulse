package config

import (
	"strings"
	"testing"
)

func TestRuntimeModeKeepsMarshallerHostSafeAndAcceptsContainerDNS(t *testing.T) {
	host := validEnv()
	host["MARSHALLER_HTTP_HOST"] = "0.0.0.0"
	if _, err := loadMap(host); err == nil || !strings.Contains(err.Error(), "host mode") {
		t.Fatalf("host wildcard error=%v", err)
	}

	container := validEnv()
	container["GOPULSE_RUNTIME_MODE"] = "container"
	container["MARSHALLER_HTTP_HOST"] = "0.0.0.0"
	container["MARSHALLER_KAFKA_BROKERS"] = "kafka:19092"
	container["MARSHALLER_VM_URL"] = "http://victoriametrics:8428"
	container["MARSHALLER_ELASTICSEARCH_URL"] = "http://elasticsearch:9200"
	cfg, err := loadMap(container)
	if err != nil || cfg.RuntimeMode != RuntimeModeContainer || cfg.KafkaBrokers[0] != "kafka:19092" {
		t.Fatalf("container config=%+v error=%v", cfg, err)
	}
}

func TestRuntimeModeRejectsUnsafeMarshallerContainerValues(t *testing.T) {
	for key, value := range map[string]string{
		"GOPULSE_RUNTIME_MODE":         "cluster",
		"MARSHALLER_HTTP_HOST":         "127.0.0.1",
		"MARSHALLER_KAFKA_BROKERS":     "127.0.0.1:19092",
		"MARSHALLER_VM_URL":            "http://user:secret@victoriametrics:8428",
		"MARSHALLER_ELASTICSEARCH_URL": "http://elasticsearch:9200/private",
	} {
		t.Run(key, func(t *testing.T) {
			env := validEnv()
			env["GOPULSE_RUNTIME_MODE"] = "container"
			env["MARSHALLER_HTTP_HOST"] = "0.0.0.0"
			env["MARSHALLER_KAFKA_BROKERS"] = "kafka:19092"
			env["MARSHALLER_VM_URL"] = "http://victoriametrics:8428"
			env["MARSHALLER_ELASTICSEARCH_URL"] = "http://elasticsearch:9200"
			env[key] = value
			_, err := loadMap(env)
			if err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("error=%v, want %s rejection", err, key)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatalf("configuration error leaked credential: %v", err)
			}
		})
	}
}

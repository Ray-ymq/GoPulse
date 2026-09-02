package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadWorkerFromRequiresOnlyWorkerDependencies(t *testing.T) {
	cfg, err := LoadWorkerFrom(mapLookup(map[string]string{
		"MYSQL_DATABASE": "gopulse", "MYSQL_USER": "worker", "MYSQL_PASSWORD": "mysql-secret",
		"RABBITMQ_URL": "amqp://worker:rabbit-secret@127.0.0.1:5672/",
	}))
	if err != nil {
		t.Fatalf("LoadWorkerFrom() error = %v", err)
	}
	if cfg.Worker.Prefetch != 10 || cfg.Worker.MaxRetries != 3 || cfg.Worker.RetryDelay != 30*time.Second {
		t.Fatalf("worker defaults = %#v", cfg.Worker)
	}
}

func TestLoadWorkerFromValidatesBoundsWithoutLeakingCredentials(t *testing.T) {
	base := map[string]string{
		"MYSQL_DATABASE": "gopulse", "MYSQL_USER": "worker", "MYSQL_PASSWORD": "mysql-secret",
		"RABBITMQ_URL": "amqp://worker:rabbit-secret@127.0.0.1:5672/",
	}
	for _, test := range []struct{ key, value string }{
		{"BUSINESS_WORKER_PREFETCH", "0"}, {"BUSINESS_WORKER_PREFETCH", "101"},
		{"BUSINESS_WORKER_MAX_RETRIES", "-1"}, {"BUSINESS_WORKER_MAX_RETRIES", "21"},
		{"BUSINESS_WORKER_PUBLISH_TIMEOUT", "1ms"}, {"BUSINESS_WORKER_SHUTDOWN_TIMEOUT", "500ms"},
		{"BUSINESS_WORKER_RECONNECT_MIN", "50ms"}, {"BUSINESS_WORKER_RECONNECT_MAX", "6m"},
	} {
		t.Run(test.key+test.value, func(t *testing.T) {
			env := make(map[string]string, len(base)+1)
			for key, value := range base {
				env[key] = value
			}
			env[test.key] = test.value
			_, err := LoadWorkerFrom(mapLookup(env))
			if err == nil || !strings.Contains(err.Error(), test.key) {
				t.Fatalf("LoadWorkerFrom() error = %v", err)
			}
			if strings.Contains(err.Error(), "mysql-secret") || strings.Contains(err.Error(), "rabbit-secret") {
				t.Fatalf("configuration error leaked credential: %v", err)
			}
		})
	}
}

func TestLoadWorkerFromRejectsInvertedReconnectBounds(t *testing.T) {
	env := map[string]string{
		"MYSQL_DATABASE": "gopulse", "MYSQL_USER": "worker", "MYSQL_PASSWORD": "secret",
		"RABBITMQ_URL":                  "amqp://worker:secret@127.0.0.1:5672/",
		"BUSINESS_WORKER_RECONNECT_MIN": "5s", "BUSINESS_WORKER_RECONNECT_MAX": "1s",
	}
	if _, err := LoadWorkerFrom(mapLookup(env)); err == nil || !strings.Contains(err.Error(), "BUSINESS_WORKER_RECONNECT_MAX") {
		t.Fatalf("LoadWorkerFrom() error = %v", err)
	}
}

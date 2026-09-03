package config

import (
	"strings"
	"testing"
	"time"
)

func validEnvironment(t *testing.T) {
	t.Helper()
	for key, value := range map[string]string{
		"REDIS_HOST": "127.0.0.1", "REDIS_PORT": "6379", "REDIS_PASSWORD": "do-not-log-this",
		"REDIS_DB": "2", "REDIS_EXPORTER_HTTP_HOST": "127.0.0.1", "REDIS_EXPORTER_HTTP_PORT": "9121",
		"REDIS_EXPORTER_SCRAPE_TIMEOUT": "2s", "REDIS_EXPORTER_SHUTDOWN_TIMEOUT": "5s",
	} {
		t.Setenv(key, value)
	}
}

func TestLoadValidConfiguration(t *testing.T) {
	validEnvironment(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RedisAddress() != "127.0.0.1:6379" || cfg.HTTPAddress() != "127.0.0.1:9121" || cfg.RedisDB != 2 {
		t.Fatalf("unexpected configuration: %#v", cfg)
	}
	if cfg.ScrapeTimeout != 2*time.Second || cfg.ShutdownTimeout != 5*time.Second {
		t.Fatalf("unexpected timeouts: %#v", cfg)
	}
}

func TestLoadRejectsInvalidFieldsWithoutCredentialLeakage(t *testing.T) {
	cases := map[string]string{
		"REDIS_HOST": "", "REDIS_PORT": "0", "REDIS_DB": "-1",
		"REDIS_EXPORTER_HTTP_HOST": " ", "REDIS_EXPORTER_HTTP_PORT": "65536",
		"REDIS_EXPORTER_SCRAPE_TIMEOUT": "99ms", "REDIS_EXPORTER_SHUTDOWN_TIMEOUT": "31s",
	}
	for field, value := range cases {
		t.Run(field, func(t *testing.T) {
			validEnvironment(t)
			t.Setenv(field, value)
			_, err := Load()
			if err == nil || Field(err) != field {
				t.Fatalf("Load() error = %v, field = %q", err, Field(err))
			}
			if strings.Contains(err.Error(), "do-not-log-this") {
				t.Fatalf("credential leaked: %v", err)
			}
		})
	}
}

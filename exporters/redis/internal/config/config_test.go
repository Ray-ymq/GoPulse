package config

import (
	"net"
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

func TestValidateHostAcceptsSupportedAddresses(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1":     "127.0.0.1",
		"0.0.0.0":       "0.0.0.0",
		"redis":         "redis",
		"redis.local":   "redis.local",
		"redis.local.":  "redis.local.",
		"::1":           "::1",
		"::":            "::",
		"[::1]":         "::1",
		"[2001:db8::1]": "2001:db8::1",
	}
	for input, expected := range cases {
		t.Run(input, func(t *testing.T) {
			actual, err := validateHost("HOST", input)
			if err != nil || actual != expected {
				t.Fatalf("validateHost(%q) = %q, %v; expected %q", input, actual, err, expected)
			}
		})
	}
}

func TestValidateHostRejectsMalformedAddresses(t *testing.T) {
	for _, input := range []string{"[]", "[", "]", "[[::1]]", "[127.0.0.1]", "bad/host", `bad\\host`, "bad_host", "-redis", "redis-", "redis..local", "host:6379"} {
		t.Run(input, func(t *testing.T) {
			if _, err := validateHost("HOST", input); err == nil || Field(err) != "HOST" {
				t.Fatalf("validateHost(%q) error = %v", input, err)
			}
		})
	}
}

func TestDefaultHTTPHostCreatesLoopbackListener(t *testing.T) {
	validEnvironment(t)
	t.Setenv("REDIS_EXPORTER_HTTP_HOST", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(cfg.HTTPHost, "0"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok || !address.IP.IsLoopback() || address.IP.IsUnspecified() {
		t.Fatalf("default listener address = %v; expected loopback only", listener.Addr())
	}
}

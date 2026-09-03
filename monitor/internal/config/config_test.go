package config

import "testing"

func TestLoadFromValidatesSecurityBoundary(t *testing.T) {
	values := map[string]string{"MONITOR_API_TOKEN": "01234567890123456789012345678901", "MONITOR_PLUGIN_ROOT": t.TempDir(), "REDIS_HOST": "127.0.0.1", "REDIS_PORT": "6379", "REDIS_DB": "0"}
	cfg, err := LoadFrom(func(key string) (string, bool) { v, ok := values[key]; return v, ok })
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}
	if cfg.HTTPAddress() != "127.0.0.1:9090" || cfg.ExporterHealthURL() != "http://127.0.0.1:9121/health" {
		t.Fatalf("unexpected defaults: %#v", cfg)
	}
	values["MONITOR_API_TOKEN"] = "short"
	if _, err = LoadFrom(func(key string) (string, bool) { v, ok := values[key]; return v, ok }); err == nil {
		t.Fatal("short token was accepted")
	}
}

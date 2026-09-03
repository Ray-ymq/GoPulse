package config

import "testing"

func TestLoadFromValidatesSecurityBoundary(t *testing.T) {
	values := map[string]string{"MONITOR_API_TOKEN": "01234567890123456789012345678901", "MONITOR_PLUGIN_ROOT": t.TempDir(), "REDIS_HOST": "127.0.0.1", "REDIS_PORT": "6379", "REDIS_DB": "0"}
	cfg, err := LoadFrom(func(key string) (string, bool) { v, ok := values[key]; return v, ok })
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}
	if cfg.HTTPAddress() != "127.0.0.1:9090" || cfg.ExporterHealthURL() != "http://127.0.0.1:9121/health" || cfg.ScrapeInterval.String() != "15s" || cfg.ScrapeTimeout.String() != "3s" {
		t.Fatalf("unexpected defaults: %#v", cfg)
	}
	values["MONITOR_API_TOKEN"] = "short"
	if _, err = LoadFrom(func(key string) (string, bool) { v, ok := values[key]; return v, ok }); err == nil {
		t.Fatal("short token was accepted")
	}
}

func TestLoadFromRejectsInvalidScrapeAndRouterConfiguration(t *testing.T) {
	values := map[string]string{"MONITOR_API_TOKEN": "01234567890123456789012345678901", "MONITOR_PLUGIN_ROOT": t.TempDir(), "REDIS_HOST": "127.0.0.1", "REDIS_PORT": "6379", "REDIS_DB": "0", "MONITOR_SCRAPE_INTERVAL": "3s", "MONITOR_SCRAPE_TIMEOUT": "3s"}
	lookup := func(key string) (string, bool) { v, ok := values[key]; return v, ok }
	if _, err := LoadFrom(lookup); err == nil {
		t.Fatal("scrape timeout equal to interval was accepted")
	}
	values["MONITOR_SCRAPE_INTERVAL"] = "15s"
	values["MONITOR_ROUTER_URL"] = "http://127.0.0.1:8080"
	if _, err := LoadFrom(lookup); err == nil {
		t.Fatal("router URL without token was accepted")
	}
}

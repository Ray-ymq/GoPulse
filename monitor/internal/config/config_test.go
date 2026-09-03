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

func TestLoadFromEnforcesPhaseSixTimeoutContract(t *testing.T) {
	base := map[string]string{"MONITOR_API_TOKEN": "01234567890123456789012345678901", "MONITOR_PLUGIN_ROOT": t.TempDir(), "REDIS_HOST": "127.0.0.1", "REDIS_PORT": "6379", "REDIS_DB": "0"}
	tests := []struct {
		key, valid, invalid string
	}{
		{"MONITOR_REQUEST_TIMEOUT", "60s", "61s"},
		{"MONITOR_PLUGIN_STARTUP_TIMEOUT", "1s", "999ms"},
		{"MONITOR_PLUGIN_STOP_TIMEOUT", "30s", "31s"},
		{"MONITOR_SCRAPE_INTERVAL", "5m", "5m1s"},
		{"MONITOR_SCRAPE_TIMEOUT", "30s", "30s1ms"},
	}
	for _, test := range tests {
		t.Run(test.key, func(t *testing.T) {
			values := make(map[string]string, len(base)+1)
			for key, value := range base {
				values[key] = value
			}
			values[test.key] = test.valid
			if test.key == "MONITOR_SCRAPE_TIMEOUT" {
				values["MONITOR_SCRAPE_INTERVAL"] = "31s"
			}
			lookup := func(key string) (string, bool) { value, ok := values[key]; return value, ok }
			if _, err := LoadFrom(lookup); err != nil {
				t.Fatalf("valid boundary rejected: %v", err)
			}
			values[test.key] = test.invalid
			if _, err := LoadFrom(lookup); err == nil {
				t.Fatalf("invalid boundary %s=%s was accepted", test.key, test.invalid)
			}
		})
	}
}

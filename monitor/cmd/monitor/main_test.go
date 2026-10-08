package main

import (
	"testing"

	"github.com/Ray-ymq/GoPulse/monitor/internal/config"
	"github.com/Ray-ymq/GoPulse/monitor/internal/plugin"
)

func TestExporterScrapePortUsesConfiguredRedisPort(t *testing.T) {
	cfg := config.Config{ExporterEnv: map[string]string{"REDIS_EXPORTER_HTTP_PORT": "19121"}}
	redis, ok := plugin.LookupOfficial(plugin.PluginID)
	if !ok {
		t.Fatal("redis plugin is not in the official catalog")
	}
	if got := exporterScrapePort(cfg, redis); got != "19121" {
		t.Fatalf("redis scrape port=%q, want 19121", got)
	}

	other := plugin.CatalogEntry{ID: "mysql-exporter", Port: 9122}
	if got := exporterScrapePort(cfg, other); got != "9122" {
		t.Fatalf("non-redis scrape port=%q, want 9122", got)
	}
}

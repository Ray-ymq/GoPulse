package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/monitor/internal/config"
	"github.com/Ray-ymq/GoPulse/monitor/internal/metrics/publisher"
	"github.com/Ray-ymq/GoPulse/monitor/internal/plugin"
)

func TestKafkaProduceTimeoutConfigurationMatchesPublisher(t *testing.T) {
	for _, test := range []struct {
		name, timeout string
		wantError     bool
	}{
		{name: "default"},
		{name: "previously accepted subsecond", timeout: "500ms", wantError: true},
		{name: "below minimum", timeout: "999999999ns", wantError: true},
		{name: "minimum", timeout: "1s"},
		{name: "maximum", timeout: "10s"},
		{name: "above maximum", timeout: "10000000001ns", wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			values := map[string]string{
				"MONITOR_API_TOKEN":             strings.Repeat("a", 32),
				"LOG_MONITOR_INGEST_TOKEN":      strings.Repeat("b", 32),
				"MONITOR_PLUGIN_ROOT":           t.TempDir(),
				"MONITOR_KAFKA_PRODUCE_TIMEOUT": test.timeout,
			}
			cfg, err := config.LoadFrom(func(key string) (string, bool) {
				value, ok := values[key]
				return value, ok
			})
			if test.wantError {
				if err == nil || !strings.Contains(err.Error(), "MONITOR_KAFKA_PRODUCE_TIMEOUT") {
					t.Fatalf("expected Kafka timeout configuration error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			transport, err := publisher.NewKafka(publisher.KafkaConfig{
				Brokers: cfg.KafkaBrokers, ProduceTimeout: cfg.KafkaProduceTimeout,
				MaxBufferedRecords: cfg.KafkaMaxBufferedRecords, MaxBufferedBytes: cfg.KafkaMaxBufferedBytes,
			})
			if err != nil {
				t.Fatalf("accepted configuration cannot create Kafka publisher: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := transport.Close(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

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

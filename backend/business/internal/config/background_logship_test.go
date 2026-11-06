package config

import "testing"

func TestBackgroundCommandsLoadSharedLogShipConfiguration(t *testing.T) {
	logValues := map[string]string{
		"LOG_MONITOR_URL": "http://127.0.0.1:19090", "LOG_MONITOR_INGEST_TOKEN": "01234567890123456789012345678901",
		"LOG_SHIP_REQUEST_TIMEOUT": "3s", "LOG_SHIP_QUEUE_CAPACITY": "17", "LOG_SHIP_RETRY_MIN": "20ms",
		"LOG_SHIP_RETRY_MAX": "2s", "LOG_SHIP_SHUTDOWN_TIMEOUT": "4s",
	}
	worker := map[string]string{
		"MYSQL_DATABASE": "gopulse", "MYSQL_USER": "worker", "MYSQL_PASSWORD": "mysql-secret",
		"RABBITMQ_URL": "amqp://worker:rabbit-secret@127.0.0.1:5672/",
	}
	indexer := map[string]string{
		"MYSQL_DATABASE": "gopulse", "MYSQL_USER": "indexer", "MYSQL_PASSWORD": "mysql-secret",
		"RABBITMQ_URL": "amqp://indexer:rabbit-secret@127.0.0.1:5672/", "ELASTICSEARCH_URL": "http://127.0.0.1:19200",
	}
	reindex := map[string]string{
		"MYSQL_DATABASE": "gopulse", "MYSQL_USER": "reindex", "MYSQL_PASSWORD": "mysql-secret",
		"ELASTICSEARCH_URL": "http://127.0.0.1:19200",
	}
	for _, values := range []map[string]string{worker, indexer, reindex} {
		for key, value := range logValues {
			values[key] = value
		}
	}
	workerConfig, err := LoadWorkerFrom(mapLookup(worker))
	if err != nil {
		t.Fatal(err)
	}
	indexerConfig, err := LoadSearchIndexerFrom(mapLookup(indexer))
	if err != nil {
		t.Fatal(err)
	}
	reindexConfig, err := LoadReindexFrom(mapLookup(reindex))
	if err != nil {
		t.Fatal(err)
	}
	for name, cfg := range map[string]LogShipConfig{
		"worker": workerConfig.LogShip, "indexer": indexerConfig.LogShip, "reindex": reindexConfig.LogShip,
	} {
		if cfg.Endpoint != "http://127.0.0.1:19090/internal/v1/logs" || cfg.QueueCapacity != 17 || !cfg.Enabled() {
			t.Fatalf("%s log ship config = %#v", name, cfg)
		}
	}
}

func TestBackgroundCommandsRejectInvalidEnabledLogShipConfiguration(t *testing.T) {
	values := map[string]string{
		"MYSQL_DATABASE": "gopulse", "MYSQL_USER": "worker", "MYSQL_PASSWORD": "mysql-secret",
		"RABBITMQ_URL":    "amqp://worker:rabbit-secret@127.0.0.1:5672/",
		"LOG_MONITOR_URL": "http://example.com", "LOG_MONITOR_INGEST_TOKEN": "01234567890123456789012345678901",
	}
	if _, err := LoadWorkerFrom(mapLookup(values)); err == nil {
		t.Fatal("LoadWorkerFrom() error = nil")
	}
}

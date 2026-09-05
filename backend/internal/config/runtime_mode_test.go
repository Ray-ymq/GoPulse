package config

import (
	"strings"
	"testing"
)

func TestRuntimeModeKeepsHostSafeByDefault(t *testing.T) {
	env := requiredEnvironment()
	cfg, err := LoadFrom(mapLookup(env))
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}
	if cfg.RuntimeMode != RuntimeModeHost {
		t.Fatalf("RuntimeMode = %q, want host", cfg.RuntimeMode)
	}

	env["HTTP_HOST"] = "0.0.0.0"
	if _, err := LoadFrom(mapLookup(env)); err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("LoadFrom() error = %v, want host loopback rejection", err)
	}
}

func TestRuntimeModeAcceptsContainerServiceDNS(t *testing.T) {
	env := containerEnvironment()
	cfg, err := LoadFrom(mapLookup(env))
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}
	if cfg.RuntimeMode != RuntimeModeContainer || cfg.HTTPHost != "0.0.0.0" || cfg.MySQL.Host != "mysql" || cfg.Redis.Host != "redis" {
		t.Fatalf("container config = %#v", cfg)
	}

	worker, err := LoadWorkerFrom(mapLookup(env))
	if err != nil || worker.MySQL.Host != "mysql" {
		t.Fatalf("LoadWorkerFrom() config=%#v error=%v", worker, err)
	}
	indexer, err := LoadSearchIndexerFrom(mapLookup(env))
	if err != nil || indexer.Elasticsearch.URL != "http://elasticsearch:9200" {
		t.Fatalf("LoadSearchIndexerFrom() config=%#v error=%v", indexer, err)
	}
	reindex, err := LoadReindexFrom(mapLookup(env))
	if err != nil || reindex.MySQL.Host != "mysql" {
		t.Fatalf("LoadReindexFrom() config=%#v error=%v", reindex, err)
	}
}

func TestRuntimeModeRejectsUnknownAndUnsafeContainerHosts(t *testing.T) {
	for _, test := range []struct {
		key   string
		value string
	}{
		{key: "GOPULSE_RUNTIME_MODE", value: "cluster"},
		{key: "MYSQL_HOST", value: "127.0.0.1"},
		{key: "REDIS_HOST", value: "host.docker.internal"},
		{key: "RABBITMQ_URL", value: "amqp://user:credential@127.0.0.1:5672/"},
		{key: "ELASTICSEARCH_URL", value: "http://elasticsearch:9200/private"},
		{key: "MONITOR_URL", value: "http://monitor:9090/?token=credential"},
	} {
		t.Run(test.key, func(t *testing.T) {
			env := containerEnvironment()
			env[test.key] = test.value
			_, err := LoadFrom(mapLookup(env))
			if err == nil || !strings.Contains(err.Error(), test.key) {
				t.Fatalf("LoadFrom() error = %v, want %s rejection", err, test.key)
			}
			if strings.Contains(err.Error(), "credential") {
				t.Fatalf("configuration error leaked a credential: %v", err)
			}
		})
	}
}

func containerEnvironment() map[string]string {
	env := requiredEnvironment()
	env["GOPULSE_RUNTIME_MODE"] = "container"
	env["HTTP_HOST"] = "0.0.0.0"
	env["MYSQL_HOST"] = "mysql"
	env["REDIS_HOST"] = "redis"
	env["RABBITMQ_URL"] = "amqp://gopulse:rabbit-secret@rabbitmq:5672/"
	env["ELASTICSEARCH_URL"] = "http://elasticsearch:9200"
	env["MONITOR_URL"] = "http://monitor:9090"
	env["BACKEND_VICTORIAMETRICS_URL"] = "http://victoriametrics:8428"
	env["LOG_MONITOR_URL"] = "http://monitor:9090"
	env["LOG_MONITOR_INGEST_TOKEN"] = "local-log-monitor-token-at-least-32-bytes"
	return env
}

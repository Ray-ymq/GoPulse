package config

import (
	"strings"
	"testing"
)

func TestParseServiceRoleDefaultsToCombined(t *testing.T) {
	for _, raw := range []string{"", " combined ", "COMBINED"} {
		role, err := ParseServiceRole(raw)
		if err != nil || role != ServiceRoleCombined {
			t.Fatalf("ParseServiceRole(%q) = %q, %v; want combined", raw, role, err)
		}
	}
}

func TestParseServiceRoleRejectsUnknownRole(t *testing.T) {
	if _, err := ParseServiceRole("worker"); err == nil || !strings.Contains(err.Error(), "BACKEND_SERVICE_ROLE") {
		t.Fatalf("ParseServiceRole(worker) error = %v", err)
	}
}

func TestLoadFromBusinessRoleOmitsPlatformRequirements(t *testing.T) {
	env := requiredEnvironment()
	delete(env, "MONITOR_API_TOKEN")
	delete(env, "BACKEND_VICTORIAMETRICS_PASSWORD")
	delete(env, "OBSERVABILITY_ELASTICSEARCH_URL")
	env["BACKEND_SERVICE_ROLE"] = "business"

	cfg, err := LoadFrom(mapLookup(env))
	if err != nil {
		t.Fatalf("LoadFrom(business) error = %v", err)
	}
	if cfg.ServiceRole != ServiceRoleBusiness || !cfg.ServiceRole.RunsBusiness() || cfg.ServiceRole.RunsPlatform() {
		t.Fatalf("business role = %q", cfg.ServiceRole)
	}
	if cfg.HTTPMaxConcurrency != 128 || cfg.MySQL.MaxOpenConns != 10 {
		t.Fatalf("business limits = http %d, mysql %d", cfg.HTTPMaxConcurrency, cfg.MySQL.MaxOpenConns)
	}
	if cfg.Redis.Password == "" || cfg.RabbitMQURL == "" || cfg.Elasticsearch.Purpose != "search" || cfg.Outbox.ClaimBatch == 0 {
		t.Fatalf("business dependencies were not loaded: %#v", cfg)
	}
	if cfg.Monitor.URL != "" || cfg.VictoriaMetrics.URL != "" || cfg.ObservabilityElasticsearch.URL != "" || cfg.AlertEvaluationEnabled {
		t.Fatalf("platform dependencies were loaded for business role: monitor=%#v vm=%#v obs=%#v alerts=%v", cfg.Monitor, cfg.VictoriaMetrics, cfg.ObservabilityElasticsearch, cfg.AlertEvaluationEnabled)
	}
}

func TestLoadFromPlatformRoleOmitsBusinessRequirementsAndUsesPlatformLimits(t *testing.T) {
	env := requiredEnvironment()
	delete(env, "REDIS_PASSWORD")
	delete(env, "RABBITMQ_URL")
	delete(env, "ELASTICSEARCH_URL")
	env["BACKEND_SERVICE_ROLE"] = "platform"

	cfg, err := LoadFrom(mapLookup(env))
	if err != nil {
		t.Fatalf("LoadFrom(platform) error = %v", err)
	}
	if cfg.ServiceRole != ServiceRolePlatform || cfg.ServiceRole.RunsBusiness() || !cfg.ServiceRole.RunsPlatform() {
		t.Fatalf("platform role = %q", cfg.ServiceRole)
	}
	if cfg.HTTPMaxConcurrency != 32 || cfg.MySQL.MaxOpenConns != 4 {
		t.Fatalf("platform limits = http %d, mysql %d", cfg.HTTPMaxConcurrency, cfg.MySQL.MaxOpenConns)
	}
	if cfg.Monitor.URL == "" || cfg.VictoriaMetrics.URL == "" || cfg.ObservabilityElasticsearch.Purpose != "observability" || !cfg.AlertEvaluationEnabled {
		t.Fatalf("platform dependencies were not loaded: monitor=%#v vm=%#v obs=%#v alerts=%v", cfg.Monitor, cfg.VictoriaMetrics, cfg.ObservabilityElasticsearch, cfg.AlertEvaluationEnabled)
	}
	if cfg.Redis.Host != "" || cfg.RabbitMQURL != "" || cfg.Elasticsearch.URL != "" || cfg.Outbox.ClaimBatch != 0 {
		t.Fatalf("business dependencies were loaded for platform role: redis=%#v rabbit=%q search=%#v outbox=%#v", cfg.Redis, cfg.RabbitMQURL, cfg.Elasticsearch, cfg.Outbox)
	}
}

func TestLoadFromRoleValidatesOnlyActiveDependencySecrets(t *testing.T) {
	business := requiredEnvironment()
	delete(business, "MONITOR_API_TOKEN")
	delete(business, "BACKEND_VICTORIAMETRICS_PASSWORD")
	delete(business, "REDIS_PASSWORD")
	business["BACKEND_SERVICE_ROLE"] = "business"
	if _, err := LoadFrom(mapLookup(business)); err == nil || !strings.Contains(err.Error(), "REDIS_PASSWORD") {
		t.Fatalf("business missing active secret error = %v", err)
	}

	platform := requiredEnvironment()
	delete(platform, "REDIS_PASSWORD")
	delete(platform, "RABBITMQ_URL")
	delete(platform, "MONITOR_API_TOKEN")
	platform["BACKEND_SERVICE_ROLE"] = "platform"
	if _, err := LoadFrom(mapLookup(platform)); err == nil || !strings.Contains(err.Error(), "MONITOR_API_TOKEN") {
		t.Fatalf("platform missing active secret error = %v", err)
	}
}

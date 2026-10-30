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

func TestStandaloneObservabilityNeedsOnlyPlatformDependencies(t *testing.T) {
	env := requiredEnvironment()
	env["BACKEND_SERVICE_ROLE"] = "platform"
	env["BACKEND_STANDALONE_OBSERVABILITY"] = "true"
	env["GOPULSE_RUNTIME_MODE"] = "container"
	env["HTTP_HOST"] = "0.0.0.0"
	env["MYSQL_HOST"] = "mysql"
	env["MONITOR_URL"] = "http://monitor:9090"
	env["OBSERVABILITY_ELASTICSEARCH_URL"] = "http://observability-elasticsearch:9200"
	env["BACKEND_VICTORIAMETRICS_URL"] = "http://victoriametrics:8428"
	for _, key := range []string{"REDIS_PASSWORD", "RABBITMQ_URL", "ELASTICSEARCH_URL", "BUSINESS_STAGE_ENDPOINTS", "BUSINESS_STAGE_SERVICE_CREDENTIAL"} {
		delete(env, key)
	}
	cfg, err := LoadFrom(mapLookup(env))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.StandaloneObservability || cfg.ServiceRole.RunsBusiness() || len(cfg.BusinessStage.Endpoints) != 0 || cfg.BusinessStage.Credential != "" {
		t.Fatalf("unexpected standalone configuration: role=%s, standalone=%v", cfg.ServiceRole, cfg.StandaloneObservability)
	}
	if !cfg.Protection.RateLimitEnabled || cfg.Protection.ResourceAuth <= 0 || cfg.Protection.LocalMaxIdentities <= 0 {
		t.Fatal("standalone authentication protection is missing")
	}
}

func TestStandaloneObservabilityRejectsBusinessAndCombinedRoles(t *testing.T) {
	for _, role := range []string{"business", "combined"} {
		env := requiredEnvironment()
		env["BACKEND_SERVICE_ROLE"] = role
		env["BACKEND_STANDALONE_OBSERVABILITY"] = "true"
		if _, err := LoadFrom(mapLookup(env)); err == nil || !strings.Contains(err.Error(), "BACKEND_STANDALONE_OBSERVABILITY") {
			t.Fatalf("role %s accepted standalone observability: %v", role, err)
		}
	}
}

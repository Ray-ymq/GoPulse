package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadReindexUsesOnlyMySQLAndElasticsearchSettings(t *testing.T) {
	values := map[string]string{
		"MYSQL_DATABASE": "gopulse", "MYSQL_USER": "user", "MYSQL_PASSWORD": "password",
		"ELASTICSEARCH_URL": "http://127.0.0.1:19200", "ELASTICSEARCH_REQUEST_TIMEOUT": "2s", "SEARCH_REINDEX_BATCH": "123",
	}
	cfg, err := LoadReindexFrom(func(key string) (string, bool) { value, ok := values[key]; return value, ok })
	if err != nil {
		t.Fatalf("LoadReindexFrom() error = %v", err)
	}
	if cfg.Elasticsearch.URL != values["ELASTICSEARCH_URL"] || cfg.Elasticsearch.RequestTimeout != 2*time.Second || cfg.Elasticsearch.ReindexBatch != 123 {
		t.Fatalf("Elasticsearch config = %#v", cfg.Elasticsearch)
	}
}

func TestElasticsearchConfigRejectsUserinfoAndBounds(t *testing.T) {
	base := map[string]string{"MYSQL_DATABASE": "db", "MYSQL_USER": "user", "MYSQL_PASSWORD": "pass"}
	for _, test := range []struct{ key, value, message string }{
		{"ELASTICSEARCH_URL", "http://user:secret@127.0.0.1:9200", "without userinfo"},
		{"ELASTICSEARCH_REQUEST_TIMEOUT", "50ms", "between"},
		{"SEARCH_REINDEX_BATCH", "5001", "between"},
	} {
		values := make(map[string]string, len(base)+1)
		for key, value := range base {
			values[key] = value
		}
		values[test.key] = test.value
		_, err := LoadReindexFrom(func(key string) (string, bool) { value, ok := values[key]; return value, ok })
		if err == nil || !strings.Contains(err.Error(), test.message) {
			t.Fatalf("%s error = %v", test.key, err)
		}
	}
}

package config

import (
	"strings"
	"testing"
)

func TestLoadFromDefaults(t *testing.T) {
	cfg, err := LoadFrom(mapLookup(requiredEnvironment()))
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}

	if cfg.AppEnv != "development" {
		t.Fatalf("AppEnv = %q, want development", cfg.AppEnv)
	}
	if cfg.HTTPHost != "127.0.0.1" || cfg.HTTPPort != 8080 {
		t.Fatalf("HTTP endpoint = %s:%d, want 127.0.0.1:8080", cfg.HTTPHost, cfg.HTTPPort)
	}
	if cfg.MySQL.Host != "127.0.0.1" || cfg.MySQL.Port != 3306 {
		t.Fatalf("MySQL endpoint = %s:%d, want 127.0.0.1:3306", cfg.MySQL.Host, cfg.MySQL.Port)
	}
	if cfg.Redis.Host != "127.0.0.1" || cfg.Redis.Port != 6379 || cfg.Redis.DB != 0 {
		t.Fatalf("Redis config = %#v, want default endpoint and DB", cfg.Redis)
	}
}

func TestLoadFromOverrides(t *testing.T) {
	env := requiredEnvironment()
	env["APP_ENV"] = "test"
	env["HTTP_HOST"] = "127.0.0.1"
	env["HTTP_PORT"] = "18080"
	env["MYSQL_HOST"] = "mysql.internal"
	env["MYSQL_PORT"] = "13306"
	env["REDIS_HOST"] = "redis.internal"
	env["REDIS_PORT"] = "16379"
	env["REDIS_DB"] = "3"

	cfg, err := LoadFrom(mapLookup(env))
	if err != nil {
		t.Fatalf("LoadFrom() error = %v", err)
	}

	if cfg.AppEnv != "test" || cfg.HTTPAddress() != "127.0.0.1:18080" {
		t.Fatalf("unexpected application config: %#v", cfg)
	}
	if cfg.MySQL.Host != "mysql.internal" || cfg.MySQL.Port != 13306 {
		t.Fatalf("unexpected MySQL config: %#v", cfg.MySQL)
	}
	if cfg.Redis.Host != "redis.internal" || cfg.Redis.Port != 16379 || cfg.Redis.DB != 3 {
		t.Fatalf("unexpected Redis config: %#v", cfg.Redis)
	}
}

func TestLoadFromMissingRequiredValue(t *testing.T) {
	for _, key := range []string{"MYSQL_DATABASE", "MYSQL_USER", "MYSQL_PASSWORD", "REDIS_PASSWORD", "RABBITMQ_URL"} {
		t.Run(key, func(t *testing.T) {
			env := requiredEnvironment()
			delete(env, key)

			_, err := LoadFrom(mapLookup(env))
			if err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("LoadFrom() error = %v, want field name %s", err, key)
			}
		})
	}
}

func TestLoadFromRejectsInvalidPorts(t *testing.T) {
	for _, test := range []struct {
		key   string
		value string
	}{
		{key: "HTTP_PORT", value: "invalid"},
		{key: "HTTP_PORT", value: "0"},
		{key: "MYSQL_PORT", value: "65536"},
		{key: "REDIS_PORT", value: "-1"},
	} {
		t.Run(test.key+"_"+test.value, func(t *testing.T) {
			env := requiredEnvironment()
			env[test.key] = test.value

			_, err := LoadFrom(mapLookup(env))
			if err == nil || !strings.Contains(err.Error(), test.key) {
				t.Fatalf("LoadFrom() error = %v, want field name %s", err, test.key)
			}
		})
	}
}

func TestLoadFromRejectsInvalidRedisDB(t *testing.T) {
	for _, value := range []string{"invalid", "-1"} {
		t.Run(value, func(t *testing.T) {
			env := requiredEnvironment()
			env["REDIS_DB"] = value

			_, err := LoadFrom(mapLookup(env))
			if err == nil || !strings.Contains(err.Error(), "REDIS_DB") {
				t.Fatalf("LoadFrom() error = %v, want REDIS_DB error", err)
			}
		})
	}
}

func TestLoadFromRejectsInvalidRabbitMQURLWithoutLeakingIt(t *testing.T) {
	secret := "do-not-leak"
	for _, value := range []string{
		"http://user:" + secret + "@localhost:5672/",
		"amqp://user:" + secret + "@/",
		"amqp://user:" + secret + "@%zz/",
	} {
		t.Run(value[:4], func(t *testing.T) {
			env := requiredEnvironment()
			env["RABBITMQ_URL"] = value

			_, err := LoadFrom(mapLookup(env))
			if err == nil {
				t.Fatal("LoadFrom() error = nil, want invalid RabbitMQ URL error")
			}
			if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), value) {
				t.Fatalf("error leaked sensitive URL: %v", err)
			}
		})
	}
}

func requiredEnvironment() map[string]string {
	return map[string]string{
		"MYSQL_DATABASE": "gopulse",
		"MYSQL_USER":     "gopulse",
		"MYSQL_PASSWORD": "mysql-secret",
		"REDIS_PASSWORD": "redis-secret",
		"RABBITMQ_URL":   "amqp://gopulse:rabbit-secret@127.0.0.1:5672/",
	}
}

func mapLookup(values map[string]string) LookupFunc {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}

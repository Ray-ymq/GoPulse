package platform

import (
	"context"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/config"
)

func TestClientsCanBeConstructedWhenDependenciesAreUnavailable(t *testing.T) {
	mysqlClient, err := NewMySQL(config.MySQLConfig{
		Host:     "127.0.0.1",
		Port:     1,
		Database: "gopulse",
		User:     "gopulse",
		Password: "secret",
	})
	if err != nil {
		t.Fatalf("NewMySQL() error = %v", err)
	}
	defer mysqlClient.Close()

	redisClient := NewRedis(config.RedisConfig{
		Host:     "127.0.0.1",
		Port:     1,
		Password: "secret",
	})
	defer redisClient.Close()

	rabbitMQ, err := NewRabbitMQ("amqp://gopulse:secret@127.0.0.1:1/")
	if err != nil {
		t.Fatalf("NewRabbitMQ() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := mysqlClient.Check(ctx); err == nil {
		t.Fatal("MySQL Check() error = nil, want unavailable dependency")
	}
	if err := redisClient.Check(ctx); err == nil {
		t.Fatal("Redis Check() error = nil, want unavailable dependency")
	}
	if err := rabbitMQ.Check(ctx); err == nil {
		t.Fatal("RabbitMQ Check() error = nil, want unavailable dependency")
	}
}

func TestElasticsearchRejectsObservationPurpose(t *testing.T) {
	_, err := NewElasticsearch(config.ElasticsearchConfig{
		URL: "http://127.0.0.1:9200", RequestTimeout: time.Second, Purpose: "observability",
	})
	if err == nil {
		t.Fatal("observation Elasticsearch purpose was accepted by the business client")
	}
}

func TestMySQLDriverConfigUsesUTCAndUTF8MB4(t *testing.T) {
	cfg := mysqlDriverConfig(config.MySQLConfig{
		Host:     "mysql.internal",
		Port:     3306,
		Database: "gopulse",
		User:     "gopulse",
		Password: "secret",
	})

	if cfg.Loc == nil || cfg.Loc.String() != "UTC" {
		t.Fatalf("MySQL location = %v, want UTC", cfg.Loc)
	}
	if cfg.Collation != "utf8mb4_0900_ai_ci" {
		t.Fatalf("MySQL collation = %q, want utf8mb4_0900_ai_ci", cfg.Collation)
	}
	if cfg.Params["time_zone"] != "'+00:00'" {
		t.Fatalf("MySQL time_zone = %q, want '+00:00'", cfg.Params["time_zone"])
	}
	if !cfg.ParseTime {
		t.Fatal("MySQL ParseTime = false, want true")
	}
}

func TestMySQLMigrationDriverConfigEnablesMultiStatementsOnlyForMigrations(t *testing.T) {
	cfg := config.MySQLConfig{
		Host:     "mysql.internal",
		Port:     3306,
		Database: "gopulse",
		User:     "gopulse",
		Password: "secret",
	}

	if mysqlDriverConfig(cfg).MultiStatements {
		t.Fatal("application MySQL config enables multi-statements")
	}
	if !mysqlMigrationDriverConfig(cfg).MultiStatements {
		t.Fatal("migration MySQL config does not enable multi-statements")
	}
}

func TestMySQLMigrationReadTimeoutIsSeparateAndBounded(t *testing.T) {
	cfg := config.MySQLConfig{Host: "mysql.internal", Port: 3306, Database: "gopulse", User: "gopulse"}
	application := mysqlDriverConfig(cfg)
	migration := mysqlMigrationDriverConfig(cfg)
	if application.ReadTimeout != time.Second || migration.ReadTimeout != 2*time.Minute {
		t.Fatalf("read timeouts: application=%s migration=%s", application.ReadTimeout, migration.ReadTimeout)
	}
	if migration.Timeout != application.Timeout || migration.WriteTimeout != application.WriteTimeout {
		t.Fatal("migration override must not change dial or write timeouts")
	}
}

func TestMySQLPoolConfigurationIsApplied(t *testing.T) {
	database, err := OpenMySQLDatabase(config.MySQLConfig{
		Host: "127.0.0.1", Port: 1, Database: "gopulse", User: "gopulse", Password: "secret",
		MaxOpenConns: 6, MaxIdleConns: 3, ConnMaxLifetime: 11 * time.Minute,
	})
	if err != nil {
		t.Fatalf("OpenMySQLDatabase() error = %v", err)
	}
	defer database.Close()
	stats := database.Stats()
	if stats.MaxOpenConnections != 6 {
		t.Fatalf("MaxOpenConnections = %d, want 6", stats.MaxOpenConnections)
	}
	if database.Stats().MaxOpenConnections <= 0 {
		t.Fatal("MySQL pool did not retain a positive open-connection limit")
	}
}

func TestMySQLPoolFallsBackToBoundedDefaults(t *testing.T) {
	database, err := OpenMySQLDatabase(config.MySQLConfig{Host: "127.0.0.1", Port: 1, Database: "gopulse", User: "gopulse"})
	if err != nil {
		t.Fatalf("OpenMySQLDatabase() error = %v", err)
	}
	defer database.Close()
	if got := database.Stats().MaxOpenConnections; got != defaultMaxOpenConns {
		t.Fatalf("MaxOpenConnections = %d, want %d", got, defaultMaxOpenConns)
	}
}

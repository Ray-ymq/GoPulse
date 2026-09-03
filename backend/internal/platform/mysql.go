package platform

import (
	"context"
	"database/sql"
	"net"
	"strconv"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	"github.com/go-sql-driver/mysql"
)

const mysqlCollation = "utf8mb4_0900_ai_ci"

type discardMySQLLogger struct{}

func (discardMySQLLogger) Print(...any) {}

type MySQL struct {
	database *sql.DB
}

func NewMySQL(cfg config.MySQLConfig) (*MySQL, error) {
	database, err := OpenMySQLDatabase(cfg)
	if err != nil {
		return nil, err
	}
	return &MySQL{database: database}, nil
}

// OpenMySQLDatabase opens a UTC MySQL connection for ordinary application use.
func OpenMySQLDatabase(cfg config.MySQLConfig) (*sql.DB, error) {
	return openMySQLDatabase(mysqlDriverConfig(cfg))
}

// OpenMySQLMigrationDatabase opens a MySQL connection that allows versioned SQL
// migration files to contain multiple DDL statements. General application
// connections keep multi-statements disabled.
func OpenMySQLMigrationDatabase(cfg config.MySQLConfig) (*sql.DB, error) {
	return openMySQLDatabase(mysqlMigrationDriverConfig(cfg))
}

func openMySQLDatabase(driverConfig *mysql.Config) (*sql.DB, error) {
	connector, err := mysql.NewConnector(driverConfig)
	if err != nil {
		return nil, err
	}

	database := sql.OpenDB(connector)
	database.SetConnMaxLifetime(3 * time.Minute)
	database.SetMaxIdleConns(2)
	database.SetMaxOpenConns(10)
	return database, nil
}

func mysqlDriverConfig(cfg config.MySQLConfig) *mysql.Config {
	driverConfig := mysql.NewConfig()
	driverConfig.User = cfg.User
	driverConfig.Passwd = cfg.Password
	driverConfig.Net = "tcp"
	driverConfig.Addr = net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	driverConfig.DBName = cfg.Database
	driverConfig.ParseTime = true
	driverConfig.Loc = time.UTC
	driverConfig.Collation = mysqlCollation
	driverConfig.Logger = discardMySQLLogger{}
	driverConfig.Timeout = time.Second
	driverConfig.ReadTimeout = time.Second
	driverConfig.WriteTimeout = time.Second
	driverConfig.Params = map[string]string{"time_zone": "'+00:00'"}
	return driverConfig
}

func mysqlMigrationDriverConfig(cfg config.MySQLConfig) *mysql.Config {
	driverConfig := mysqlDriverConfig(cfg)
	driverConfig.MultiStatements = true
	return driverConfig
}

// DB exposes the shared application connection pool to repositories.
func (client *MySQL) DB() *sql.DB {
	return client.database
}

func (client *MySQL) Check(ctx context.Context) error {
	return client.database.PingContext(ctx)
}

func (client *MySQL) Close() error {
	return client.database.Close()
}

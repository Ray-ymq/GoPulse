//go:build integration

package migrations

import (
	"database/sql"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/backend/internal/config"
	"github.com/go-sql-driver/mysql"
)

// openIntegrationMySQL keeps migration-package integration tests independent
// from internal/platform, whose runtime schema check imports this package.
func openIntegrationMySQL(t *testing.T, cfg config.MySQLConfig) *sql.DB {
	t.Helper()
	driverConfig := mysql.NewConfig()
	driverConfig.User = cfg.User
	driverConfig.Passwd = cfg.Password
	driverConfig.Net = "tcp"
	driverConfig.Addr = net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	driverConfig.DBName = cfg.Database
	driverConfig.ParseTime = true
	driverConfig.Loc = time.UTC
	driverConfig.Collation = "utf8mb4_0900_ai_ci"
	driverConfig.Timeout = time.Second
	driverConfig.ReadTimeout = 2 * time.Minute
	driverConfig.WriteTimeout = time.Second
	driverConfig.MultiStatements = true
	driverConfig.Params = map[string]string{"time_zone": "'+00:00'"}
	db, err := sql.Open("mysql", driverConfig.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		t.Fatal(err)
	}
	return db
}

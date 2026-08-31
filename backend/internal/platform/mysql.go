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

type MySQL struct {
	database *sql.DB
}

func NewMySQL(cfg config.MySQLConfig) (*MySQL, error) {
	driverConfig := mysql.NewConfig()
	driverConfig.User = cfg.User
	driverConfig.Passwd = cfg.Password
	driverConfig.Net = "tcp"
	driverConfig.Addr = net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	driverConfig.DBName = cfg.Database
	driverConfig.ParseTime = true
	driverConfig.Timeout = time.Second
	driverConfig.ReadTimeout = time.Second
	driverConfig.WriteTimeout = time.Second

	connector, err := mysql.NewConnector(driverConfig)
	if err != nil {
		return nil, err
	}

	database := sql.OpenDB(connector)
	database.SetConnMaxLifetime(3 * time.Minute)
	database.SetMaxIdleConns(2)
	database.SetMaxOpenConns(10)
	return &MySQL{database: database}, nil
}

func (client *MySQL) Check(ctx context.Context) error {
	return client.database.PingContext(ctx)
}

func (client *MySQL) Close() error {
	return client.database.Close()
}

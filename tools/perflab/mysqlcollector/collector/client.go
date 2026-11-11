package collector

import (
	"database/sql"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/Ray-ymq/GoPulse/exporters/mysql/config"
	mysql "github.com/go-sql-driver/mysql"
)

type silentLogger struct{}

func (silentLogger) Print(...any) {}

func NewClient(c *config.Config) (*Client, error) {
	cfg := mysql.NewConfig()
	cfg.User = c.Username
	cfg.Passwd = c.Password
	cfg.Net = "tcp"
	port := c.Port
	if port == 0 {
		port = 3306
	}
	cfg.Addr = net.JoinHostPort(c.Host, strconv.Itoa(port))
	// Database is a validated product configuration field, not a reason to grant
	// access to business tables. Server/global status uses no default schema.
	cfg.Timeout = c.ConnectTimeout
	cfg.ReadTimeout = c.ScrapeTimeout
	cfg.WriteTimeout = c.ScrapeTimeout
	cfg.Logger = silentLogger{}
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		return nil, config.ErrConfig
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Minute)
	return &Client{DB: db, collectors: c.Collectors}, nil
}

type Client struct {
	DB         *sql.DB
	collectors string
	mu         sync.Mutex
	failures   map[string]uint64
}

func (c *Client) CloseIdleConnections() { _ = c.DB.Close() }

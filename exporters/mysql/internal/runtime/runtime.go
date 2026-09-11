package runtime

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Ray-ymq/GoPulse/exporters/mysql/internal/collector"
	mysql "github.com/go-sql-driver/mysql"
)

type Config struct {
	Host, Username, Password, Database, Listen string
	ConnectTimeout, ScrapeTimeout              time.Duration
}

var ErrConfig = errors.New("invalid_configuration")

func Load() (Config, error) {
	c := Config{Host: os.Getenv("MYSQL_HOST"), Username: os.Getenv("MYSQL_USERNAME"), Password: os.Getenv("MYSQL_PASSWORD"), Database: os.Getenv("MYSQL_DATABASE")}
	mode := os.Getenv("GOPULSE_RUNTIME_MODE")
	if (mode == "container" && c.Host != "mysql") || ((mode == "host" || mode == "") && c.Host != "127.0.0.1" && c.Host != "::1") || (mode != "" && mode != "host" && mode != "container") || os.Getenv("MYSQL_PORT") != "3306" {
		return Config{}, ErrConfig
	}
	for _, s := range []string{c.Username, c.Database} {
		if !utf8.ValidString(s) || utf8.RuneCountInString(s) < 1 || utf8.RuneCountInString(s) > 64 {
			return Config{}, ErrConfig
		}
		for _, r := range s {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("._-", r) {
				return Config{}, ErrConfig
			}
		}
	}
	if !utf8.ValidString(c.Password) || len(c.Password) < 1 || len(c.Password) > 256 || strings.ContainsRune(c.Password, 0) {
		return Config{}, ErrConfig
	}
	var err error
	c.ConnectTimeout, err = time.ParseDuration(os.Getenv("MYSQL_EXPORTER_CONNECT_TIMEOUT"))
	if err != nil {
		return Config{}, ErrConfig
	}
	c.ScrapeTimeout, err = time.ParseDuration(os.Getenv("MYSQL_EXPORTER_SCRAPE_TIMEOUT"))
	if err != nil || c.ConnectTimeout < 100*time.Millisecond || c.ConnectTimeout > c.ScrapeTimeout || c.ScrapeTimeout > 10*time.Second {
		return Config{}, ErrConfig
	}
	host := os.Getenv("MYSQL_EXPORTER_HTTP_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("MYSQL_EXPORTER_HTTP_PORT")
	if port == "" {
		port = "9122"
	}
	if (host != "127.0.0.1" && host != "::1") || port != "9122" {
		return Config{}, ErrConfig
	}
	c.Listen = net.JoinHostPort(host, port)
	return c, nil
}

type silentLogger struct{}

func (silentLogger) Print(...any) {}

func Open(c Config) (*sql.DB, error) {
	cfg := mysql.NewConfig()
	cfg.User = c.Username
	cfg.Passwd = c.Password
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(c.Host, strconv.Itoa(3306))
	// Database is a validated product configuration field, not a reason to grant
	// access to business tables. Server/global status uses no default schema.
	cfg.Timeout = c.ConnectTimeout
	cfg.ReadTimeout = c.ScrapeTimeout
	cfg.WriteTimeout = c.ScrapeTimeout
	cfg.Logger = silentLogger{}
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		return nil, ErrConfig
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Minute)
	return db, nil
}

func Handler(db *sql.DB, timeout time.Duration) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","service":"mysql-exporter"}`))
	})
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		body, err := collector.Collect(ctx, db)
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			body = collector.Unavailable
		}
		w.Write([]byte(body))
	})
	return mux
}

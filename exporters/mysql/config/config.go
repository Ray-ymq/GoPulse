package config

import (
	"errors"

	"net"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type Config struct {
	Host, Username, Password, Database, Listen string
	Collectors                                 string
	Port                                       int
	ConnectTimeout, ScrapeTimeout              time.Duration
	ShutdownTimeout                            time.Duration
}

const CollectorNames = "innodb_metrics,transactions,redo_capacity,buffer_pool,lock_waits,statement_digest,statement_events,errors,file_io,wait_events,table_io,index_io,performance_schema,consumers,instruments,digest_lost"

var ErrConfig = errors.New("invalid_configuration")

func Load() (Config, error) { return load(os.Getenv) }

func load(get func(string) string) (Config, error) {
	if err := validateEnvironment(get); err != nil {
		return Config{}, err
	}

	c := Config{Host: get("MYSQL_HOST"), Username: get("MYSQL_USERNAME"), Password: get("MYSQL_PASSWORD"), Database: get("MYSQL_DATABASE")}
	mode := get("GOPULSE_RUNTIME_MODE")
	if !validTarget(mode, c.Host, get("MYSQL_PORT")) {
		return Config{}, ErrConfig
	}
	c.Port, _ = strconv.Atoi(get("MYSQL_PORT"))
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
	c.Collectors = get("MYSQL_EXPORTER_COLLECTORS")
	if c.Collectors != "" && c.Collectors != "none" {
		seen := map[string]bool{}
		for _, name := range strings.Split(c.Collectors, ",") {
			if !strings.Contains(","+CollectorNames+",", ","+name+",") || name == "" || seen[name] {
				return Config{}, ErrConfig
			}
			seen[name] = true
		}
	}
	var err error
	c.ConnectTimeout, err = time.ParseDuration(get("MYSQL_EXPORTER_CONNECT_TIMEOUT"))
	if err != nil {
		return Config{}, ErrConfig
	}
	c.ScrapeTimeout, err = time.ParseDuration(get("MYSQL_EXPORTER_SCRAPE_TIMEOUT"))
	if err != nil || c.ConnectTimeout < 100*time.Millisecond || c.ConnectTimeout > c.ScrapeTimeout || c.ScrapeTimeout > 10*time.Second {
		return Config{}, ErrConfig
	}
	host := get("MYSQL_EXPORTER_HTTP_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := get("MYSQL_EXPORTER_HTTP_PORT")
	if port == "" {
		port = "9122"
	}
	if !validListen(mode, host, port) {
		return Config{}, ErrConfig
	}
	c.Listen = net.JoinHostPort(host, port)
	c.ShutdownTimeout = 5 * time.Second
	if raw := get("MYSQL_EXPORTER_SHUTDOWN_TIMEOUT"); raw != "" {
		c.ShutdownTimeout, err = time.ParseDuration(raw)
		if err != nil || c.ShutdownTimeout <= 0 || c.ShutdownTimeout > 5*time.Second {
			return Config{}, ErrConfig
		}
	}
	return c, nil
}

func validTarget(mode, host, port string) bool {
	parsed, err := strconv.Atoi(port)
	if err != nil || parsed < 1 || parsed > 65535 || host == "" || strings.ContainsAny(host, "\x00\r\n \t/@") {
		return false
	}
	ip := net.ParseIP(host)
	if mode == "host" || mode == "" {
		return host == "localhost" || ip != nil && (ip.IsLoopback() || ip.IsPrivate())
	}
	return mode == "container" && ip == nil
}

func validListen(mode, host, port string) bool {
	ip := net.ParseIP(host)
	parsed, err := strconv.Atoi(port)
	if err != nil || parsed < 1 || parsed > 65535 || ip == nil {
		return false
	}
	return (mode == "container" && (ip.IsLoopback() || ip.IsUnspecified())) || ((mode == "host" || mode == "") && ip.IsLoopback())
}

func (c Config) Redacted() Config { c.Password = "<redacted>"; return c }

package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxConfigBytes = 64 << 10

var ErrConfig = errors.New("invalid_configuration")

type Config struct {
	Host            string        `json:"host"`
	Port            int           `json:"port"`
	User            string        `json:"username"`
	ConnectTimeout  time.Duration `json:"-"`
	ScrapeTimeout   time.Duration `json:"-"`
	ShutdownTimeout time.Duration `json:"-"`
	Password        string        `json:"-"`
	Listen          string        `json:"-"`
	Mode            string        `json:"-"`
}

type Runtime struct {
	Mode            string `json:"mode"`
	ListenHost      string `json:"listen_host"`
	ListenPort      int    `json:"listen_port"`
	ConnectTimeout  string `json:"connect_timeout"`
	ScrapeTimeout   string `json:"scrape_timeout"`
	ShutdownTimeout string `json:"shutdown_timeout"`
}

type fields struct {
	Host           string `json:"host"`
	Port           int    `json:"port"`
	User           string `json:"username"`
	ConnectTimeout string `json:"connect_timeout"`
	ScrapeTimeout  string `json:"scrape_timeout"`
}

type envelope struct {
	Config  *fields `json:"config"`
	Secrets struct {
		Password string `json:"password"`
	} `json:"secrets"`
	Runtime Runtime `json:"runtime"`
}

func LoadFile(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, ErrConfig
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return Config{}, ErrConfig
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxConfigBytes+1))
	if err != nil {
		return Config{}, ErrConfig
	}
	return Parse(data)
}

func Parse(data []byte) (Config, error) {
	if len(data) == 0 || len(data) > MaxConfigBytes || !utf8.Valid(data) || !uniqueJSON(data) {
		return Config{}, ErrConfig
	}
	var env envelope
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if dec.Decode(&env) != nil || env.Config == nil {
		return Config{}, ErrConfig
	}
	f, r := env.Config, env.Runtime
	if r.Mode == "" {
		r.Mode = "host"
	}
	if r.ListenHost == "" {
		r.ListenHost = "127.0.0.1"
	}
	if r.ListenPort == 0 {
		r.ListenPort = 9601
	}
	c := Config{Host: f.Host, Port: f.Port, User: f.User, Password: env.Secrets.Password, Mode: r.Mode,
		Listen: net.JoinHostPort(r.ListenHost, strconv.Itoa(r.ListenPort))}
	if !validHost(c.Mode, c.Host) || c.Port < 1 || c.Port > 65535 || !validText(c.User, 64) || !validText(c.Password, 256) {
		return Config{}, ErrConfig
	}
	ip := net.ParseIP(r.ListenHost)
	if ip == nil || r.ListenPort < 1 || r.ListenPort > 65535 || !ip.IsLoopback() && (r.Mode != "container" || !ip.IsUnspecified()) {
		return Config{}, ErrConfig
	}
	var err error
	c.ConnectTimeout, err = time.ParseDuration(f.ConnectTimeout)
	if err != nil {
		return Config{}, ErrConfig
	}
	c.ScrapeTimeout, err = time.ParseDuration(f.ScrapeTimeout)
	if err != nil || c.ConnectTimeout < 100*time.Millisecond || c.ConnectTimeout > c.ScrapeTimeout || c.ScrapeTimeout > 10*time.Second {
		return Config{}, ErrConfig
	}
	// Monitor 的运行时预算限制插件配置的超时。
	if r.ConnectTimeout != "" {
		d, err := time.ParseDuration(r.ConnectTimeout)
		if err != nil || d < 100*time.Millisecond || d > 10*time.Second {
			return Config{}, ErrConfig
		}
		c.ConnectTimeout = min(c.ConnectTimeout, d)
	}
	if r.ScrapeTimeout != "" {
		d, err := time.ParseDuration(r.ScrapeTimeout)
		if err != nil || d < 100*time.Millisecond || d > 10*time.Second {
			return Config{}, ErrConfig
		}
		c.ScrapeTimeout = min(c.ScrapeTimeout, d)
	}
	c.ConnectTimeout = min(c.ConnectTimeout, c.ScrapeTimeout)
	c.ShutdownTimeout = 5 * time.Second
	if r.ShutdownTimeout != "" {
		c.ShutdownTimeout, err = time.ParseDuration(r.ShutdownTimeout)
		if err != nil || c.ShutdownTimeout < 100*time.Millisecond || c.ShutdownTimeout > 30*time.Second {
			return Config{}, ErrConfig
		}
	}
	return c, nil
}

func validHost(mode, host string) bool {
	if mode != "host" && mode != "container" || host == "" || len(host) > 253 {
		return false
	}
	ip := net.ParseIP(host)
	if mode == "host" {
		return host == "localhost" || ip != nil && ip.IsLoopback()
	}
	if ip != nil {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}
	return true
}

func validText(value string, max int) bool {
	return utf8.ValidString(value) && len(value) > 0 && len(value) <= max && !strings.ContainsAny(value, "\x00\r\n")
}

func (c Config) Redacted() Config { c.Password = "<redacted>"; return c }

// uniqueJSON 同时拒绝重复键、null 和多份 JSON 文档。
func uniqueJSON(data []byte) bool {
	dec := json.NewDecoder(bytes.NewReader(data))
	var value func() bool
	value = func() bool {
		token, err := dec.Token()
		if err != nil || token == nil {
			return false
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return true
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for dec.More() {
				key, err := dec.Token()
				if err != nil {
					return false
				}
				name, ok := key.(string)
				if !ok || seen[name] || !value() {
					return false
				}
				seen[name] = true
			}
			end, err := dec.Token()
			return err == nil && end == json.Delim('}')
		case '[':
			for dec.More() {
				if !value() {
					return false
				}
			}
			end, err := dec.Token()
			return err == nil && end == json.Delim(']')
		default:
			return false
		}
	}
	if !value() {
		return false
	}
	_, err := dec.Token()
	return errors.Is(err, io.EOF)
}

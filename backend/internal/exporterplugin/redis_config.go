package exporterplugin

import (
	"bytes"
	"encoding/json"
	"time"
	"unicode/utf8"
)

// RedisConfig contains no credential and can be stored independently of Secret.
// It is internal configuration, not a public status DTO (it contains an origin).
type RedisConfig struct {
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Database       int    `json:"database"`
	ConnectTimeout string `json:"connect_timeout"`
	ScrapeTimeout  string `json:"scrape_timeout"`
}

// RedisSecret must never be logged or returned by the management API.
type RedisSecret struct {
	Password string `json:"password"`
}

// ParseRedisConfiguration accepts the schema-defined object and separates its
// secret. An omitted password retains an existing secret only for replacement;
// explicit null/empty strings, unknown fields and duplicate fields are rejected.
// The caller owns persistence and must not persist connection-test candidates.
func ParseRedisConfiguration(data []byte, mode string, previous *RedisSecret) (RedisConfig, RedisSecret, error) {
	fail := func() (RedisConfig, RedisSecret, error) { return RedisConfig{}, RedisSecret{}, invalidConfig() }
	if !uniqueJSON(data) {
		return fail()
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields == nil {
		return fail()
	}
	for key, raw := range fields {
		switch key {
		case "host", "port", "database", "connect_timeout", "scrape_timeout", "password":
		default:
			return fail()
		}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return fail()
		}
	}
	for _, key := range []string{"host", "port", "database", "connect_timeout", "scrape_timeout"} {
		if _, ok := fields[key]; !ok {
			return fail()
		}
	}
	var config RedisConfig
	configData := make(map[string]json.RawMessage, len(fields))
	for key, raw := range fields {
		if key != "password" {
			configData[key] = raw
		}
	}
	raw, _ := json.Marshal(configData)
	if json.Unmarshal(raw, &config) != nil {
		return fail()
	}
	switch mode {
	case "host":
		if (config.Host != "127.0.0.1" && config.Host != "::1") || config.Port != 6379 {
			return fail()
		}
	case "container":
		if config.Host != "redis" || config.Port != 6379 {
			return fail()
		}
	default:
		return fail()
	}
	if config.Database < 0 || config.Database > 15 {
		return fail()
	}
	connect, err := time.ParseDuration(config.ConnectTimeout)
	if err != nil {
		return fail()
	}
	scrape, err := time.ParseDuration(config.ScrapeTimeout)
	if err != nil {
		return fail()
	}
	if connect < 100*time.Millisecond || scrape > 10*time.Second || connect > scrape {
		return fail()
	}
	secret := RedisSecret{}
	if password, ok := fields["password"]; ok {
		if json.Unmarshal(password, &secret.Password) != nil {
			return fail()
		}
	} else if previous != nil {
		secret = *previous
	} else {
		return fail()
	}
	if len(secret.Password) < 1 || len(secret.Password) > 256 || !utf8.ValidString(secret.Password) {
		return fail()
	}
	// Environment injection cannot represent NUL. Reject control characters rather
	// than letting an adapter or upstream error reveal the candidate credential.
	for _, character := range secret.Password {
		if character < 32 || character == 127 {
			return fail()
		}
	}
	return config, secret, nil
}

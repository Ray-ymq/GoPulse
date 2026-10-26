package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Ray-ymq/GoPulse/exporters/mysql/common"
)

const MaxConfigBytes = 64 << 10

type Runtime struct {
	Mode            string `json:"mode"`
	ListenHost      string `json:"listen_host"`
	ListenPort      int    `json:"listen_port"`
	ConnectTimeout  string `json:"connect_timeout"`
	ScrapeTimeout   string `json:"scrape_timeout"`
	ShutdownTimeout string `json:"shutdown_timeout"`
}
type envelope struct {
	Config  json.RawMessage `json:"config"`
	Secrets json.RawMessage `json:"secrets"`
	Runtime Runtime         `json:"runtime"`
}

func readFile(path string) (envelope, error) {
	f, err := os.Open(path)
	if err != nil {
		return envelope{}, errors.New("invalid_configuration")
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return envelope{}, errors.New("invalid_configuration")
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxConfigBytes+1))
	if err != nil || len(data) > MaxConfigBytes || !utf8.Valid(data) || !uniqueJSON(data) {
		return envelope{}, errors.New("invalid_configuration")
	}
	var env envelope
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if dec.Decode(&env) != nil {
		return envelope{}, errors.New("invalid_configuration")
	}
	if _, err := object(env.Config); err != nil {
		return envelope{}, err
	}
	if len(env.Secrets) == 0 {
		env.Secrets = json.RawMessage(`{}`)
	}
	if _, err := object(env.Secrets); err != nil {
		return envelope{}, err
	}
	return env, nil
}
func object(data json.RawMessage) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if len(data) == 0 || data[0] != '{' || json.Unmarshal(data, &fields) != nil || fields == nil {
		return nil, errors.New("invalid_configuration")
	}
	return fields, nil
}
func stringField(fields map[string]json.RawMessage, name string, required bool) (string, error) {
	data, ok := fields[name]
	if !ok {
		if required {
			return "", errors.New("invalid_configuration")
		}
		return "", nil
	}
	var value string
	if json.Unmarshal(data, &value) != nil {
		return "", errors.New("invalid_configuration")
	}
	return value, nil
}
func integerField(fields map[string]json.RawMessage, name string, required bool) (int, error) {
	data, ok := fields[name]
	if !ok {
		if required {
			return 0, errors.New("invalid_configuration")
		}
		return 0, nil
	}
	var value int
	if json.Unmarshal(data, &value) != nil {
		return 0, errors.New("invalid_configuration")
	}
	return value, nil
}
func runtimeEnvironment(prefix string, r Runtime) map[string]string {
	if r.Mode == "" {
		r.Mode = "host"
	}
	values := map[string]string{"GOPULSE_RUNTIME_MODE": r.Mode}
	if r.ListenHost != "" {
		values[prefix+"_EXPORTER_HTTP_HOST"] = r.ListenHost
	}
	if r.ListenPort != 0 {
		values[prefix+"_EXPORTER_HTTP_PORT"] = strconv.Itoa(r.ListenPort)
	}
	// 数据源的连接与采集超时由 config 段声明，保持现有语义。
	if r.ShutdownTimeout != "" {
		values[prefix+"_EXPORTER_SHUTDOWN_TIMEOUT"] = r.ShutdownTimeout
	}
	return values
}
func loadValues(values map[string]string) (Config, error) {
	return load(func(key string) string { return values[key] })
}

var identityPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)

func validateEnvironment(get func(string) string) error {
	fail := func(field string) error { return errors.New("invalid_configuration: " + field) }
	mode := get("GOPULSE_RUNTIME_MODE")
	if mode != "" && mode != "host" && mode != "container" {
		return fail("GOPULSE_RUNTIME_MODE")
	}
	if value := strings.TrimSpace(get("GOPULSE_INSTANCE_ID")); value != "" && !identityPattern.MatchString(value) {
		return fail("GOPULSE_INSTANCE_ID")
	}
	if value := strings.TrimSpace(get("GOPULSE_REPLICA_COUNT")); value != "" {
		count, err := strconv.Atoi(value)
		if err != nil || count < 1 || count > 8 {
			return fail("GOPULSE_REPLICA_COUNT")
		}
	}
	prefix := strings.ToUpper(strings.ReplaceAll(common.PluginID, "-", "_"))
	if value := get(prefix + "_SHUTDOWN_TIMEOUT"); value != "" {
		duration, err := time.ParseDuration(value)
		if err != nil || duration <= 0 || duration > 5*time.Second {
			return fail(prefix + "_SHUTDOWN_TIMEOUT")
		}
	}
	if value := get(prefix + "_METRICS_TOKEN"); value != "" && (len(value) < 32 || strings.ContainsAny(value, " \t\r\n")) {
		return fail(prefix + "_METRICS_TOKEN")
	}
	return nil
}

// uniqueJSON 拒绝重复键、null 和连续多份 JSON 文档。
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

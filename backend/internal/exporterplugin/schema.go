package exporterplugin

import (
	"bytes"
	"encoding/json"
	"github.com/Ray-ymq/GoPulse/backend/internal/apperror"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// ConfigSchema is a closed, declarative contract, not a general JSON Schema.
// Runtime adapters must enforce origins and cross-field constraints separately.
type ConfigSchema struct {
	SchemaVersion int           `json:"schema_version"`
	PluginID      string        `json:"plugin_id"`
	Fields        []ConfigField `json:"fields"`
}

type ConfigField struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Secret   bool     `json:"secret"`
	Minimum  *int     `json:"minimum,omitempty"`
	Maximum  *int     `json:"maximum,omitempty"`
	Enum     []string `json:"enum,omitempty"`
}

func boundedField(name, kind string, minimum, maximum int) ConfigField {
	return ConfigField{Name: name, Type: kind, Required: true, Secret: kind == "secret", Minimum: &minimum, Maximum: &maximum}
}

// OfficialSchema returns a fresh ordered schema; duration bounds use milliseconds,
// secret bounds use bytes and other string bounds use Unicode code points.
func OfficialSchema(id string) (ConfigSchema, error) {
	entry, ok := LookupOfficial(id)
	if !ok {
		return ConfigSchema{}, invalidConfig()
	}
	fields := []ConfigField{{Name: "host", Type: "hostname", Required: true}}
	port := "port"
	if entry.Source == "rabbitmq" {
		port = "management_port"
	}
	fields = append(fields, ConfigField{Name: port, Type: "port", Required: true})
	switch entry.Source {
	case "redis":
		fields = append(fields, boundedField("database", "integer", 0, 15))
	case "mysql":
		fields = append(fields, boundedField("database", "string", 1, 64), boundedField("username", "string", 1, 64))
	case "rabbitmq":
		fields = append(fields, ConfigField{Name: "vhost", Type: "string", Required: true, Enum: []string{"/"}}, boundedField("username", "string", 1, 64))
	case "kafka":
		fields = append(fields, ConfigField{Name: "topic", Type: "string", Required: true, Enum: []string{"gopulse-observability-metrics"}}, ConfigField{Name: "consumer_group", Type: "string", Required: true, Enum: []string{"gopulse-marshaller-metrics"}})
	case "elasticsearch":
		f := boundedField("username", "string", 1, 64)
		f.Required = false
		fields = append(fields, f)
	case "victoriametrics":
		fields = append(fields, boundedField("username", "string", 1, 64))
	}
	fields = append(fields, boundedField("connect_timeout", "duration", 100, 10000), boundedField("scrape_timeout", "duration", 100, 10000))
	if entry.Source != "kafka" {
		f := boundedField("password", "secret", 1, 256)
		if entry.Source == "elasticsearch" {
			f.Required = false
		}
		fields = append(fields, f)
	}
	return ConfigSchema{SchemaVersion: 1, PluginID: id, Fields: fields}, nil
}

// OfficialSchemaJSON is the canonical package representation.
func OfficialSchemaJSON(id string) ([]byte, error) {
	schema, err := OfficialSchema(id)
	if err != nil {
		return nil, err
	}
	return json.Marshal(schema)
}

func invalidConfig() error {
	return apperror.New(apperror.CodePluginPackageInvalid, "plugin configuration is invalid")
}

// uniqueJSON rejects repeated keys at every depth before typed decoding. Go's
// standard decoder alone accepts duplicate fields, including nested fields.
func uniqueJSON(data []byte) bool {
	if len(data) == 0 || len(data) > 64<<10 || !utf8.Valid(data) || !json.Valid(data) {
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	var value func() bool
	value = func() bool {
		token, err := dec.Token()
		if err != nil {
			return false
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return true
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for dec.More() {
				key, err := dec.Token()
				if err != nil {
					return false
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return false
				}
				seen[name] = true
				if !value() {
					return false
				}
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
	return err == io.EOF
}

// validateConfigurationRequest checks the source-independent DTO boundary.
// Endpoint restrictions, authentication combinations and target reachability
// belong to the Exporter and Monitor runtime contracts.
func validateConfigurationRequest(data []byte, schema ConfigSchema, preserveSecrets bool) bool {
	if !uniqueJSON(data) || schema.SchemaVersion != 1 || len(schema.Fields) == 0 {
		return false
	}
	var request map[string]json.RawMessage
	if decodeStrict(data, &request) != nil || request == nil {
		return false
	}
	for key := range request {
		if key != "config" && key != "secrets" {
			return false
		}
	}
	var config map[string]json.RawMessage
	if raw, ok := request["config"]; !ok || decodeStrict(raw, &config) != nil || config == nil {
		return false
	}
	secrets := map[string]json.RawMessage{}
	if raw, ok := request["secrets"]; ok {
		if decodeStrict(raw, &secrets) != nil || secrets == nil {
			return false
		}
	}
	fields := map[string]ConfigField{}
	for _, field := range schema.Fields {
		if field.Name == "" || fields[field.Name].Name != "" || (field.Secret && field.Type != "secret") || (!field.Secret && field.Type == "secret") {
			return false
		}
		fields[field.Name] = field
	}
	for name, raw := range config {
		field, ok := fields[name]
		if !ok || field.Secret || !validateSchemaValue(raw, field) {
			return false
		}
	}
	for name, raw := range secrets {
		field, ok := fields[name]
		if !ok || !field.Secret || !validateSchemaValue(raw, field) {
			return false
		}
	}
	for _, field := range schema.Fields {
		if field.Required {
			if field.Secret {
				if _, ok := secrets[field.Name]; !ok && !preserveSecrets {
					return false
				}
			} else if _, ok := config[field.Name]; !ok {
				return false
			}
		}
	}
	return true
}

func validateSchemaValue(raw json.RawMessage, field ConfigField) bool {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return false
	}
	switch field.Type {
	case "integer", "port":
		var value int
		if json.Unmarshal(raw, &value) != nil {
			return false
		}
		if field.Type == "port" && (value < 1 || value > 65535) {
			return false
		}
		return withinInt(int64(value), field.Minimum, field.Maximum)
	case "duration":
		var value string
		if json.Unmarshal(raw, &value) != nil {
			return false
		}
		duration, err := time.ParseDuration(value)
		return err == nil && withinInt(duration.Milliseconds(), field.Minimum, field.Maximum)
	default:
		var value string
		if json.Unmarshal(raw, &value) != nil || !utf8.ValidString(value) {
			return false
		}
		if field.Type == "hostname" && strings.TrimSpace(value) == "" {
			return false
		}
		if field.Type == "string" || field.Type == "secret" || field.Type == "hostname" {
			length := utf8.RuneCountInString(value)
			if !withinInt(int64(length), field.Minimum, field.Maximum) {
				return false
			}
		}
		if len(field.Enum) > 0 {
			found := false
			for _, allowed := range field.Enum {
				found = found || value == allowed
			}
			if !found {
				return false
			}
		}
		for _, r := range value {
			if unicode.IsControl(r) {
				return false
			}
		}
		return true
	}
}

func withinInt(value int64, minimum, maximum *int) bool {
	return (minimum == nil || value >= int64(*minimum)) && (maximum == nil || value <= int64(*maximum))
}

// ValidateConfigSchema checks both byte integrity and the exact official shape,
// including explicit false fields, field order, bounds and absence of nulls.

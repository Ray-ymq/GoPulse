package plugin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
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
		fields = append(fields, ConfigField{Name: "topic", Type: "string", Required: true, Enum: []string{"gopulse-observability-v1"}}, ConfigField{Name: "consumer_group", Type: "string", Required: true, Enum: []string{"gopulse-marshaller-metrics-v1"}})
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

func invalidConfig() error { return NewError(CodePackageInvalid, "plugin configuration is invalid") }

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

// ValidateConfigSchema checks both byte integrity and the exact official shape,
// including explicit false fields, field order, bounds and absence of nulls.
func ValidateConfigSchema(data []byte, manifest Manifest) error {
	if manifest.SchemaVersion != 2 || manifest.ConfigSchemaPath != "config.schema.json" || !uniqueJSON(data) {
		return invalidConfig()
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != manifest.ConfigSchemaSHA256 {
		return invalidConfig()
	}
	var typed ConfigSchema
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&typed) != nil {
		return invalidConfig()
	}
	expected, err := OfficialSchemaJSON(manifest.ID)
	if err != nil {
		return err
	}
	var actualValue, expectedValue any
	if json.Unmarshal(data, &actualValue) != nil || json.Unmarshal(expected, &expectedValue) != nil {
		return invalidConfig()
	}
	actual, _ := json.Marshal(actualValue)
	canonical, _ := json.Marshal(expectedValue)
	if !bytes.Equal(actual, canonical) {
		return invalidConfig()
	}
	return nil
}

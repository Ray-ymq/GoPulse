package exporterplugin

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// clusterAdapter enforces the closed official plugin configuration contract.
// It shares the existing revision/Secret transaction store, not a new manager.
type clusterAdapter struct{ id string }

func (a clusterAdapter) Parse(data []byte, mode string, previous json.RawMessage) (json.RawMessage, json.RawMessage, error) {
	fail := func() (json.RawMessage, json.RawMessage, error) { return nil, nil, invalidConfig() }
	if len(data) > 16<<10 || !uniqueJSON(data) {
		return fail()
	}
	var request map[string]json.RawMessage
	if json.Unmarshal(data, &request) != nil || request == nil {
		return fail()
	}
	for key := range request {
		if key != "config" && key != "secrets" {
			return fail()
		}
	}
	var config map[string]json.RawMessage
	if json.Unmarshal(request["config"], &config) != nil || config == nil {
		return fail()
	}
	private := map[string]string{}
	if previous != nil && json.Unmarshal(previous, &private) != nil {
		return fail()
	}
	if raw, ok := request["secrets"]; ok {
		var secret map[string]json.RawMessage
		if json.Unmarshal(raw, &secret) != nil || secret == nil {
			return fail()
		}
		for key, value := range secret {
			if key != "password" || string(value) == "null" {
				return fail()
			}
			var password string
			if json.Unmarshal(value, &password) != nil {
				return fail()
			}
			private[key] = password
		}
	} else if previous == nil {
		return fail()
	}
	if len(private) > 1 || (a.id == "kafka-exporter" && len(private) != 0) {
		return fail()
	}
	if password, ok := private["password"]; ok {
		if len(password) < 1 || len(password) > 256 || !utf8.ValidString(password) || strings.ContainsRune(password, 0) {
			return fail()
		}
	} else if a.id != "kafka-exporter" && a.id != "elasticsearch-exporter" {
		return fail()
	}
	schema, err := OfficialSchema(a.id)
	if err != nil {
		return fail()
	}
	allowed := map[string]bool{}
	for _, field := range schema.Fields {
		if !field.Secret {
			allowed[field.Name] = true
		}
	}
	for key := range config {
		if !allowed[key] {
			return fail()
		}
	}
	values := map[string]string{}
	for _, field := range schema.Fields {
		if field.Secret {
			continue
		}
		raw, ok := config[field.Name]
		if !ok && !field.Required {
			continue
		}
		if !ok || string(raw) == "null" {
			return fail()
		}
		if field.Type == "port" {
			var port int
			if json.Unmarshal(raw, &port) != nil {
				return fail()
			}
			values[field.Name] = strconv.Itoa(port)
			continue
		}
		var value string
		if json.Unmarshal(raw, &value) != nil {
			return fail()
		}
		values[field.Name] = value
		if field.Type == "string" {
			if len(field.Enum) > 0 {
				if value != field.Enum[0] {
					return fail()
				}
			} else {
				if utf8.RuneCountInString(value) < *field.Minimum || utf8.RuneCountInString(value) > *field.Maximum {
					return fail()
				}
				for _, r := range value {
					if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("._-", r) {
						return fail()
					}
				}
			}
		}
	}
	if a.id == "elasticsearch-exporter" && (values["username"] == "") != (private["password"] == "") {
		return fail()
	}
	entry, ok := LookupOfficial(a.id)
	if !ok {
		return fail()
	}
	portKey := "port"
	if entry.Source == "rabbitmq" {
		portKey = "management_port"
	}
	switch mode {
	case "host":
		if (values["host"] != "127.0.0.1" && values["host"] != "::1") || values[portKey] != strconv.Itoa(entry.HostPort) {
			return fail()
		}
	case "container":
		if values["host"] != entry.Source || values[portKey] != strconv.Itoa(entry.ContainerPort) {
			return fail()
		}
	default:
		return fail()
	}
	connect, e1 := time.ParseDuration(values["connect_timeout"])
	scrape, e2 := time.ParseDuration(values["scrape_timeout"])
	if e1 != nil || e2 != nil || connect < 100*time.Millisecond || scrape > 10*time.Second || connect > scrape {
		return fail()
	}
	public, _ := json.Marshal(config)
	secret, _ := json.Marshal(private)
	return public, secret, nil
}

func (a clusterAdapter) Environment(public, private json.RawMessage) map[string]string {
	var fields map[string]json.RawMessage
	var secret map[string]string
	if json.Unmarshal(public, &fields) != nil || json.Unmarshal(private, &secret) != nil {
		return nil
	}
	entry, ok := LookupOfficial(a.id)
	if !ok {
		return nil
	}
	prefix := strings.ToUpper(entry.Source)
	out := map[string]string{prefix + "_PASSWORD": secret["password"]}
	for key, raw := range fields {
		var value string
		if json.Unmarshal(raw, &value) != nil {
			var number int
			if json.Unmarshal(raw, &number) != nil {
				return nil
			}
			value = strconv.Itoa(number)
		}
		name := prefix + "_" + strings.ToUpper(key)
		if key == "connect_timeout" || key == "scrape_timeout" {
			name = prefix + "_EXPORTER_" + strings.ToUpper(key)
		}
		out[name] = value
	}
	return out
}

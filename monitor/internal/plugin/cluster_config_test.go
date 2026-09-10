package plugin

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClusterConfigurationBoundary(t *testing.T) {
	for _, source := range []string{"mysql", "rabbitmq"} {
		t.Run(source, func(t *testing.T) {
			fields := `"port":3306,"database":"gopulse"`
			if source == "rabbitmq" {
				fields = `"management_port":15672,"vhost":"/"`
			}
			input := `{"config":{"host":"` + source + `",` + fields + `,"username":"metrics","connect_timeout":"100ms","scrape_timeout":"1s"},"secrets":{"password":"candidate-secret"}}`
			adapter, ok := adapterFor(source + "-exporter")
			if !ok {
				t.Fatal("missing adapter")
			}
			public, private, err := adapter.Parse([]byte(input), "container", nil)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(public), "candidate-secret") || adapter.Environment(public, private)[strings.ToUpper(source)+"_PASSWORD"] != "candidate-secret" {
				t.Fatal("secret boundary")
			}
			retained := `{"config":` + string(public) + `}`
			_, next, err := adapter.Parse([]byte(retained), "container", private)
			if err != nil || string(next) != string(private) {
				t.Fatal("secret not retained")
			}
			var invalid map[string]json.RawMessage
			json.Unmarshal(public, &invalid)
			invalid["password"] = json.RawMessage(`"candidate-secret"`)
			bad, _ := json.Marshal(invalid)
			if _, _, err := adapter.Parse([]byte(`{"config":`+string(bad)+`,"secrets":{"password":"candidate-secret"}}`), "container", nil); err == nil {
				t.Fatal("public secret accepted")
			}
			for _, bad := range []string{strings.Replace(input, `"host":"`+source+`"`, `"host":"attacker"`, 1), strings.Replace(input, `"100ms"`, `"2s"`, 1), strings.Replace(input, `"candidate-secret"`, `null`, 1), strings.Replace(input, `"username":"metrics"`, `"username":"metrics","username":"other"`, 1)} {
				if _, _, err := adapter.Parse([]byte(bad), "container", nil); err == nil {
					t.Fatal("invalid configuration accepted")
				}
			}
		})
	}
}

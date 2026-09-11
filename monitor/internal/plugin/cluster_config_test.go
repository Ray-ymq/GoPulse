package plugin

import (
	"bytes"
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

func TestTopologyConfigurationSecrets(t *testing.T) {
	kafka := []byte(`{"config":{"host":"kafka","port":19092,"topic":"gopulse-observability-v1","consumer_group":"gopulse-marshaller-metrics-v1","connect_timeout":"1s","scrape_timeout":"3s"},"secrets":{}}`)
	a, _ := adapterFor("kafka-exporter")
	public, secret, err := a.Parse(kafka, "container", nil)
	if err != nil || string(secret) != "{}" {
		t.Fatalf("no-secret Kafka: %s %v", secret, err)
	}
	bad := []byte(`{"config":` + string(public) + `,"secrets":{"password":"not-supported"}}`)
	if _, _, err := a.Parse(bad, "container", nil); err == nil {
		t.Fatal("Kafka Secret accepted")
	}
	a, _ = adapterFor("elasticsearch-exporter")
	config := `{"host":"elasticsearch","port":9200,"connect_timeout":"1s","scrape_timeout":"3s"}`
	if _, secret, err = a.Parse([]byte(`{"config":`+config+`,"secrets":{}}`), "container", nil); err != nil || string(secret) != "{}" {
		t.Fatal("optional authentication rejected", err)
	}
	authenticated := strings.Replace(config, `"port":9200`, `"port":9200,"username":"metrics"`, 1)
	public, secret, err = a.Parse([]byte(`{"config":`+authenticated+`,"secrets":{"password":"candidate"}}`), "container", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, next, err := a.Parse([]byte(`{"config":`+string(public)+`}`), "container", secret)
	if err != nil || string(next) != string(secret) {
		t.Fatal("authentication not retained", err)
	}
	if _, _, err = a.Parse([]byte(`{"config":`+config+`}`), "container", secret); err == nil {
		t.Fatal("username removed with preserved password")
	}
}

func TestVictoriaMetricsControlledConfiguration(t *testing.T) {
	a, ok := adapterFor("victoriametrics-exporter")
	if !ok {
		t.Fatal("missing adapter")
	}
	request := []byte(`{"config":{"host":"victoriametrics","port":8428,"username":"metrics","connect_timeout":"1s","scrape_timeout":"2s"},"secrets":{"password":"private-canary"}}`)
	public, private, err := a.Parse(request, "container", nil)
	if err != nil {
		t.Fatal(err)
	}
	env := a.Environment(public, private)
	if env["VICTORIAMETRICS_HOST"] != "victoriametrics" || env["VICTORIAMETRICS_PASSWORD"] != "private-canary" {
		t.Fatal("wrong scoped environment")
	}
	bad := bytes.Replace(request, []byte(`"victoriametrics"`), []byte(`"http://victoriametrics/metrics"`), 1)
	if _, _, err = a.Parse(bad, "container", nil); err == nil {
		t.Fatal("arbitrary URL accepted")
	}
}

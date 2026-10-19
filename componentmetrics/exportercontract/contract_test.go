package exportercontract

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestManagementRequestAndRuntimeFile(t *testing.T) {
	config, secrets, hasSecrets, err := ReadManagement([]byte(`{"config":{"host":"127.0.0.1"},"secrets":{"password":"private"}}`))
	if err != nil || !hasSecrets || string(config) != `{"host":"127.0.0.1"}` || string(secrets) != `{"password":"private"}` {
		t.Fatalf("unexpected management request: %s %s %t %v", config, secrets, hasSecrets, err)
	}
	data, err := Encode(config, secrets, Runtime{ListenHost: "127.0.0.1", ListenPort: 9121})
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := Read(data)
	if err != nil || !envelope.HasRuntime || envelope.Runtime.ListenPort != 9121 {
		t.Fatalf("unexpected runtime envelope: %+v %v", envelope, err)
	}
}

func TestRejectsAmbiguousOrSourceOwnedRuntime(t *testing.T) {
	for _, data := range []string{
		`{"config":{},"config":{}}`,
		`{"config":{},"runtime":{"listen_port":1,"extra":true}}`,
		`{"config":{},"runtime":null}`,
		`{"config":null}`,
	} {
		if _, err := Read([]byte(data)); err == nil {
			t.Fatalf("accepted invalid config %s", data)
		}
	}
	if _, _, _, err := ReadManagement([]byte(`{"config":{},"runtime":{}}`)); err == nil {
		t.Fatal("management request supplied runtime")
	}
	if _, err := Read([]byte(`{"config":{},"secrets":{},"runtime":{"listen_host":"127.0.0.1","listen_port":1}}`)); err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(`{"config":{},"secrets":{}}`), &raw); err != nil || strings.TrimSpace(string(mustJSON(raw))) == "" {
		t.Fatal("json helper failed")
	}
}

func mustJSON(value any) []byte {
	data, _ := json.Marshal(value)
	return data
}

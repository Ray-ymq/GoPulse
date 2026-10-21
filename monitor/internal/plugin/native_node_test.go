package plugin

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNativeNodeConfigurationAndArguments(t *testing.T) {
	for _, request := range []string{`{"config":{}}`, `{"config":{"rootfs":"/host","procfs":"/host/proc","sysfs":"/host/sys"},"secrets":{}}`} {
		config, secret, err := parseNativeNodeRequest([]byte(request))
		if err != nil || string(secret) != `{}` {
			t.Fatal(err)
		}
		args, err := nativeNodeArguments(config, 19127)
		if err != nil {
			t.Fatal(err)
		}
		command := strings.Join(args, " ")
		for _, expected := range []string{"--web.listen-address=127.0.0.1:19127", "--web.disable-exporter-metrics", "--collector.cpu", "--collector.filesystem"} {
			if !strings.Contains(command, expected) {
				t.Fatal(command)
			}
		}
		if strings.Contains(command, "serve") || strings.Contains(command, "--config") {
			t.Fatal(command)
		}
	}
	for _, invalid := range []string{
		`{"config":null}`, `{"config":{"rootfs":null}}`, `{"config":{"host":"localhost"}}`,
		`{"config":{"rootfs":"relative"}}`, `{"config":{"rootfs":"/a/../etc"}}`,
		`{"config":{"rootfs":"/host\n--bad"}}`, `{"config":{"rootfs":"/","rootfs":"/host"}}`,
		`{"config":{},"secrets":{"password":"private"}}`, `{"config":{},"runtime":{}}`,
	} {
		if _, _, err := parseNativeNodeRequest([]byte(invalid)); err == nil {
			t.Fatal("接受了非法配置", invalid)
		}
	}
	if _, err := nativeNodeArguments(json.RawMessage(`{}`), 0); err == nil {
		t.Fatal("接受了非法端口")
	}
}

func TestNativeNodeHealthAndSchema(t *testing.T) {
	entry, ok := LookupOfficial(nativeNodeID)
	if !ok || entry.HealthPath != "/" || entry.Port != 9127 || entry.Entrypoint != "bin/node_exporter" {
		t.Fatal(entry)
	}
	manifest := Manifest{SchemaVersion: 2, ID: nativeNodeID, Source: "node", Entrypoint: entry.Entrypoint}
	if !nativeNodeManifest(manifest) || !validProcessHealth(manifest, []byte(`<html><title>Node Exporter</title></html>`)) || validProcessHealth(manifest, []byte(`{"status":"ok"}`)) {
		t.Fatal("健康检查未使用原生协议")
	}
	schema, err := OfficialSchemaJSON(nativeNodeID)
	if err != nil || strings.Contains(string(schema), `"host"`) || strings.Contains(string(schema), `"password"`) {
		t.Fatal(string(schema), err)
	}
}

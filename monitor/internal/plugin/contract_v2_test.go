package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func v2Manifest(t *testing.T) (Manifest, []byte) {
	t.Helper()
	var m Manifest
	if err := json.Unmarshal(validManifestJSON(), &m); err != nil {
		t.Fatal(err)
	}
	m.SchemaVersion = 2
	m.RuntimeContractVersion = 1
	m.MetricsContractVersion = 2
	m.ConfigSchemaPath = "config.schema.json"
	schema, err := OfficialSchemaJSON(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(schema)
	m.ConfigSchemaSHA256 = hex.EncodeToString(sum[:])
	return m, schema
}

func TestOfficialCatalogIdentityAndAvailability(t *testing.T) {
	sources := []string{"redis", "mysql", "rabbitmq", "kafka", "elasticsearch", "victoriametrics"}
	catalog := OfficialCatalog()
	if len(catalog) != len(sources) {
		t.Fatal("catalog size")
	}
	for i, entry := range catalog {
		if entry.ID != sources[i]+"-exporter" || entry.Source != sources[i] || entry.TargetID != entry.ID+"-local" || entry.Entrypoint != "bin/gopulse-"+entry.ID || entry.Port != 9121+i || !entry.Available {
			t.Fatalf("incorrect identity: %+v", entry)
		}
		schema, err := OfficialSchema(entry.ID)
		if err != nil || schema.PluginID != entry.ID {
			t.Fatal("missing schema")
		}
	}
	catalog[0].ID = "attacker"
	if _, ok := LookupOfficial("redis-exporter"); !ok {
		t.Fatal("caller mutated catalog")
	}
	if _, ok := LookupOfficial("custom-exporter"); ok {
		t.Fatal("unknown ID accepted")
	}
}

func TestManifestV2Contract(t *testing.T) {
	manifest, _ := v2Manifest(t)
	data, _ := json.Marshal(manifest)
	if _, err := ParseManifestV2(data); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseManifest(data); err == nil {
		t.Fatal("v2 entered legacy runtime")
	}
	if _, err := ParseManifestV2(validManifestJSON()); err == nil {
		t.Fatal("v1 accepted as v2")
	}
	for _, field := range []string{"runtime_contract_version", "metrics_contract_version", "config_schema_path", "config_schema_sha256"} {
		t.Run("required_"+field, func(t *testing.T) {
			var values map[string]json.RawMessage
			_ = json.Unmarshal(data, &values)
			delete(values, field)
			candidate, _ := json.Marshal(values)
			if _, err := ParseManifestV2(candidate); err == nil {
				t.Fatal("missing field accepted")
			}
		})
	}
	mutations := map[string]func(*Manifest){
		"schema":        func(m *Manifest) { m.SchemaVersion = 3 },
		"runtime":       func(m *Manifest) { m.RuntimeContractVersion = 2 },
		"metrics":       func(m *Manifest) { m.MetricsContractVersion = 1 },
		"id":            func(m *Manifest) { m.ID = "custom-exporter" },
		"source":        func(m *Manifest) { m.Source = "mysql" },
		"entrypoint":    func(m *Manifest) { m.Entrypoint = "bin/script" },
		"schema_path":   func(m *Manifest) { m.ConfigSchemaPath = "../schema.json" },
		"schema_digest": func(m *Manifest) { m.ConfigSchemaSHA256 = strings.Repeat("A", 64) },
		"platform":      func(m *Manifest) { m.OS = "windows" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			m := manifest
			mutate(&m)
			candidate, _ := json.Marshal(m)
			if _, err := ParseManifestV2(candidate); err == nil {
				t.Fatal("invalid contract accepted")
			}
		})
	}
	for _, prefix := range []string{`{"hook":"run",`, `{"runtime_contract_version":1,`} {
		if _, err := ParseManifestV2([]byte(prefix + string(data[1:]))); err == nil {
			t.Fatal("extra or repeated field accepted")
		}
	}
}

func TestOfficialSchemaValidation(t *testing.T) {
	m, schema := v2Manifest(t)
	if err := ValidateConfigSchema(schema, m); err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct{ name, old, new string }{
		{"duplicate", "\"required\":true", "\"required\":true,\"required\":true"},
		{"unknown", "\"required\":true", "\"required\":true,\"hook\":\"run\""},
		{"missing", "\"secret\":false,", ""},
		{"bounds", "\"maximum\":15", "\"maximum\":16"},
		{"type", "\"schema_version\":1", "\"schema_version\":1.0"},
	} {
		t.Run(change.name, func(t *testing.T) {
			candidate := []byte(strings.Replace(string(schema), change.old, change.new, 1))
			if string(candidate) == string(schema) {
				t.Fatal("fixture did not mutate")
			}
			sum := sha256.Sum256(candidate)
			altered := m
			altered.ConfigSchemaSHA256 = hex.EncodeToString(sum[:])
			if err := ValidateConfigSchema(candidate, altered); err == nil {
				t.Fatal("self-consistent nonofficial schema accepted")
			}
		})
	}
	m.ConfigSchemaSHA256 = strings.Repeat("0", 64)
	if err := ValidateConfigSchema(schema, m); err == nil {
		t.Fatal("digest mismatch accepted")
	}
}

func TestRedisConfigurationSeparationAndReplacement(t *testing.T) {
	data := []byte(`{"host":"redis","port":6379,"database":0,"connect_timeout":"1s","scrape_timeout":"2s","password":"phase14-secret-canary"}`)
	config, secret, err := ParseRedisConfiguration(data, "container", nil)
	if err != nil || secret.Password != "phase14-secret-canary" {
		t.Fatal("valid config rejected", err)
	}
	serialized, _ := json.Marshal(config)
	if strings.Contains(string(serialized), secret.Password) || strings.Contains(string(serialized), "password") {
		t.Fatal("secret entered config")
	}
	replacement := []byte(strings.Replace(string(data), `,"password":"phase14-secret-canary"`, "", 1))
	next, retained, err := ParseRedisConfiguration(replacement, "container", &secret)
	if err != nil || !reflect.DeepEqual(next, config) || retained != secret {
		t.Fatal("replacement did not retain secret")
	}
	if _, _, err = ParseRedisConfiguration(replacement, "container", nil); err == nil {
		t.Fatal("initial missing password accepted")
	}
	for _, change := range []struct{ name, old, new string }{
		{"duplicate", `"port":6379`, `"port":6379,"port":6379`},
		{"unknown", `"database":0`, `"database":0,"url":"redis://unsafe"`},
		{"origin", `"redis"`, `"host.docker.internal"`},
		{"port", `6379`, `6380`},
		{"database", `"database":0`, `"database":16`},
		{"timeout", `"1s"`, `"3s"`},
		{"null", `"phase14-secret-canary"`, `null`},
		{"empty", `"phase14-secret-canary"`, `""`},
		{"array", `"phase14-secret-canary"`, `["secret"]`},
	} {
		t.Run(change.name, func(t *testing.T) {
			bad := []byte(strings.Replace(string(data), change.old, change.new, 1))
			got, credential, err := ParseRedisConfiguration(bad, "container", &secret)
			if err == nil {
				t.Fatal("invalid config accepted")
			}
			if got != (RedisConfig{}) || credential != (RedisSecret{}) || strings.Contains(err.Error(), secret.Password) {
				t.Fatal("failure exposes candidate")
			}
		})
	}
	host := []byte(strings.Replace(string(data), `"redis"`, `"127.0.0.1"`, 1))
	if _, _, err := ParseRedisConfiguration(host, "host", nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ParseRedisConfiguration(data, "host", nil); err == nil {
		t.Fatal("container DNS allowed in host mode")
	}
}

func TestExtractV2PackageSchemaAndAllowedFiles(t *testing.T) {
	m, schema := v2Manifest(t)
	binary := []byte("non-executed-test-fixture")
	sum := sha256.Sum256(binary)
	m.EntrypointSHA256 = hex.EncodeToString(sum[:])
	manifest, _ := json.Marshal(m)
	entries := map[string][]byte{"plugin.json": manifest, "config.schema.json": schema, m.Entrypoint: binary}
	if _, err := extractPackageV2(writeArchive(t, entries), t.TempDir()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"config.schema.json", "extra-script", "../escape"} {
		t.Run(name, func(t *testing.T) {
			bad := map[string][]byte{}
			for key, value := range entries {
				bad[key] = value
			}
			bad[name] = []byte("not official")
			if _, err := extractPackageV2(writeArchive(t, bad), t.TempDir()); err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
	delete(entries, "config.schema.json")
	if _, err := extractPackageV2(writeArchive(t, entries), t.TempDir()); err == nil {
		t.Fatal("missing schema accepted")
	}
}

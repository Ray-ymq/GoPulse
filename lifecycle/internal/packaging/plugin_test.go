package packaging

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"strings"
	"testing"
)

// pluginArchive builds one plugin package: the metadata, an ELF entry point of
// the requested architecture and, for current packages, a config schema.
func pluginArchive(t *testing.T, id, version, arch string, schema bool) []byte {
	t.Helper()
	machine := elfMachines[arch]
	entrypoint := make([]byte, 32)
	copy(entrypoint, []byte{0x7f, 'E', 'L', 'F'})
	entrypoint[18] = byte(machine)
	entrypoint[19] = byte(machine >> 8)
	manifest := map[string]any{
		"schema_version": 1, "id": id, "name": id, "version": version,
		"kind": "metrics-exporter", "source": strings.TrimSuffix(id, "-exporter"),
		"os": "linux", "arch": arch, "entrypoint": "bin/" + id,
		"entrypoint_sha256": sumHex(entrypoint),
		"health_path":       "/health", "metrics_path": "/metrics",
	}
	members := map[string][]byte{
		"plugin.json": mustJSON(t, manifest),
		"bin/" + id:   entrypoint,
	}
	if schema {
		schemaBytes := []byte("{\"type\":\"object\"}\n")
		members["config.schema.json"] = schemaBytes
		manifest["schema_version"] = 2
		manifest["config_schema_path"] = "config.schema.json"
		manifest["config_schema_sha256"] = sumHex(schemaBytes)
		members["plugin.json"] = mustJSON(t, manifest)
	}
	var buffer bytes.Buffer
	compressed := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(compressed)
	for _, name := range sortedNames(members) {
		data := members[name]
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestPluginRecordValidatesArchiveIdentity protects the plugin contract:
// recorded identity comes from the archive itself and a foreign architecture
// or a mismatched entry point is rejected without executing anything.
func TestPluginRecordValidatesArchiveIdentity(t *testing.T) {
	archive := pluginArchive(t, "redis-exporter", "2.5.4", "amd64", true)
	record, err := pluginRecord(archive, "amd64")
	if err != nil {
		t.Fatalf("a valid current plugin must be recorded: %v", err)
	}
	if record.ID != "redis-exporter" || record.Version != "2.5.4" || record.Arch != "amd64" || record.Purpose != "current" {
		t.Fatalf("unexpected plugin identity: %+v", record)
	}
	if record.Archive != Sum(archive) || record.Schema == "" {
		t.Fatalf("plugin digests must be recorded: %+v", record)
	}
	if _, err := pluginRecord(archive, "arm64"); err == nil {
		t.Error("a plugin built for another architecture must be rejected")
	}
	if _, err := pluginRecord(archive[:len(archive)/2], "amd64"); err == nil {
		t.Error("a truncated plugin archive must be rejected")
	}
	legacy := pluginArchive(t, "redis-exporter", "1.9.4", "amd64", false)
	upgrade, err := pluginRecord(legacy, "amd64")
	if err != nil {
		t.Fatalf("the legacy upgrade input must be recorded: %v", err)
	}
	if upgrade.Purpose != "upgrade-only" || upgrade.Schema != "" {
		t.Fatalf("a package without a config schema is upgrade-only: %+v", upgrade)
	}
}

package plugin

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWritePackageMetadataCanonicalV2(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "bin"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin/gopulse-redis-exporter"), []byte("not-executed"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := WritePackageMetadata(root, "1.11.1", runtime.GOARCH, "redis"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := ParseManifestV2(raw)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := os.ReadFile(filepath.Join(root, "config.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateConfigSchema(schema, manifest); err != nil {
		t.Fatal(err)
	}
	if err := WritePackageMetadata(root, "invalid-version", runtime.GOARCH, "redis"); err == nil {
		t.Fatal("invalid release version accepted")
	}
}

// The legacy manifest bytes are pinned by published archive digests, so this test
// freezes the sorted-key encoding instead of a struct-derived one.
func TestLegacyManifestKeepsPublishedBytes(t *testing.T) {
	const expected = `{"arch":"amd64","entrypoint":"bin/gopulse-redis-exporter","entrypoint_sha256":"a622e287226d7859c475512e3e142fe015d206a3049a0b85301c8b65eb420f50","health_path":"/health","id":"redis-exporter","kind":"metrics-exporter","metrics_path":"/metrics","name":"GoPulse Redis Exporter","os":"linux","schema_version":1,"source":"redis","version":"1.10.6"}` + "\n"
	data, err := LegacyManifest("1.10.6", "amd64", "bin/gopulse-redis-exporter", "a622e287226d7859c475512e3e142fe015d206a3049a0b85301c8b65eb420f50")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != expected {
		t.Fatalf("legacy manifest bytes changed:\n got %s\nwant %s", data, expected)
	}
	if _, err := LegacyManifest("1.10", "amd64", "bin/gopulse-redis-exporter", ""); err == nil {
		t.Fatal("invalid release version accepted")
	}
}

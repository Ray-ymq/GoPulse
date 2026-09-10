package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Ray-ymq/GoPulse/monitor/internal/plugin"
)

func TestGenerateCanonicalV2Metadata(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "bin"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin/gopulse-redis-exporter"), []byte("not-executed"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := generate(root, "1.11.1", runtime.GOARCH); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := plugin.ParseManifestV2(raw)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := os.ReadFile(filepath.Join(root, "config.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := plugin.ValidateConfigSchema(schema, manifest); err != nil {
		t.Fatal(err)
	}
	if err := generate(root, "invalid-version", runtime.GOARCH); err == nil {
		t.Fatal("invalid release version accepted")
	}
}

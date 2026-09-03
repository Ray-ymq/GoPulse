package plugin

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeArchive(t *testing.T, entries map[string][]byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "plugin.tar.gz")
	file, _ := os.Create(path)
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	for name, data := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()
	file.Close()
	return path
}
func TestExtractPackageAcceptsValidAndRejectsTraversal(t *testing.T) {
	binary := []byte("binary")
	sum := sha256.Sum256(binary)
	manifest := Manifest{SchemaVersion: 1, ID: PluginID, Name: "GoPulse Redis Exporter", Version: "1.0.0", Kind: "metrics-exporter", Source: "redis", OS: "linux", Arch: runtime.GOARCH, Entrypoint: "bin/gopulse-redis-exporter", EntrypointSHA256: hex.EncodeToString(sum[:]), HealthPath: "/health", MetricsPath: "/metrics"}
	data, _ := json.Marshal(manifest)
	staging := t.TempDir()
	if _, err := extractPackage(writeArchive(t, map[string][]byte{"plugin.json": data, "bin/gopulse-redis-exporter": binary}), staging); err != nil {
		t.Fatalf("valid archive rejected: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if _, err := extractPackage(writeArchive(t, map[string][]byte{"../outside": []byte("bad"), "plugin.json": data, "bin/gopulse-redis-exporter": binary}), t.TempDir()); err == nil {
		t.Fatal("traversal archive accepted")
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatal("archive wrote outside staging")
	}
}

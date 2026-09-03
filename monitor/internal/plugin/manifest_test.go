package plugin

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

func validManifestJSON() []byte {
	value := Manifest{SchemaVersion: 1, ID: PluginID, Name: "GoPulse Redis Exporter", Version: "1.2.3", Kind: "metrics-exporter", Source: "redis", OS: "linux", Arch: runtime.GOARCH, Entrypoint: "bin/gopulse-redis-exporter", EntrypointSHA256: strings.Repeat("a", 64), HealthPath: "/health", MetricsPath: "/metrics"}
	data, _ := json.Marshal(value)
	return data
}
func TestParseManifestStrictness(t *testing.T) {
	if _, err := ParseManifest(validManifestJSON()); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	duplicate := strings.Replace(string(validManifestJSON()), `"id":"redis-exporter"`, `"id":"redis-exporter","id":"redis-exporter"`, 1)
	if _, err := ParseManifest([]byte(duplicate)); err == nil {
		t.Fatal("duplicate key accepted")
	}
	unknown := strings.Replace(string(validManifestJSON()), `{`, `{"hook":"run",`, 1)
	if _, err := ParseManifest([]byte(unknown)); err == nil {
		t.Fatal("unknown field accepted")
	}
}
func TestCompareSemver(t *testing.T) {
	result, err := CompareSemver("1.10.0", "1.9.9")
	if err != nil || result <= 0 {
		t.Fatalf("CompareSemver() = %d, %v", result, err)
	}
}

package release

import (
	"encoding/json"
	"strings"
	"testing"
)

func fixture(t *testing.T) *Manifest {
	t.Helper()
	image := Image{Ref: "registry.example/product@sha256:" + strings.Repeat("1", 64), Platforms: map[string]string{"linux/amd64": "sha256:" + strings.Repeat("2", 64), "linux/arm64": "sha256:" + strings.Repeat("3", 64)}}
	m := &Manifest{SchemaVersion: 1, Version: "1.13.1", Revision: strings.Repeat("a", 40), Compose: Asset{Path: "deploy/product/compose.yaml", SHA256: "sha256:" + strings.Repeat("4", 64)}, BundleSHA256: "sha256:" + strings.Repeat("5", 64), Images: map[string]Image{}, ThirdParty: map[string]Image{}, Lifecycle: image, SupportedUpgradeSources: []string{}}
	for _, name := range products {
		m.Images[name] = image
	}
	for _, name := range sources {
		m.ThirdParty[name] = image
		for _, arch := range []string{"amd64", "arm64"} {
			m.Plugins = append(m.Plugins, Plugin{ID: name + "-exporter", Version: m.Version, OS: "linux", Arch: arch, Purpose: "current", Archive: m.BundleSHA256, Entrypoint: m.BundleSHA256, Schema: m.BundleSHA256})
		}
	}
	m.Plugins = append(m.Plugins, Plugin{ID: "redis-exporter", Version: "1.9.4", OS: "linux", Arch: "amd64", Purpose: "upgrade-only", Archive: m.BundleSHA256, Entrypoint: m.BundleSHA256})
	return m
}
func TestManifestAndServerBoundary(t *testing.T) {
	m := fixture(t)
	data, _ := json.Marshal(m)
	if _, err := Parse(data); err != nil {
		t.Fatal(err)
	}
	if err := m.CheckTool(m.Version, m.Revision, "amd64", "linux", "amd64"); err != nil {
		t.Fatal(err)
	}
	if err := m.CheckTool(m.Version, m.Revision, "arm64", "linux", "amd64"); err == nil {
		t.Fatal("wrong server arch accepted")
	}
	if err := m.CheckTool("1.13.0", m.Revision, "amd64", "linux", "amd64"); err == nil {
		t.Fatal("wrong tool version accepted")
	}
	duplicate := append([]byte(`{"version":"1.13.1",`), data[1:]...)
	if _, err := Parse(duplicate); err == nil {
		t.Fatal("duplicate field accepted")
	}
	m.Plugins[1] = m.Plugins[0]
	data, _ = json.Marshal(m)
	if _, err := Parse(data); err == nil {
		t.Fatal("duplicate plugin accepted")
	}
}

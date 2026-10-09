package packaging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ray-ymq/GoPulse/lifecycle/internal/release"
)

// fixtureImage builds one immutable candidate reference for the manifest
// fixtures. Unit tests never talk to a registry.
func fixtureImage(name string) release.Image {
	return release.Image{
		Ref:       "registry.example/gopulse/" + name + "@sha256:" + strings.Repeat("1", 64),
		Platforms: map[string]string{"linux/amd64": "sha256:" + strings.Repeat("2", 64)},
	}
}

// fixtureManifest satisfies the closed manifest contract for one platform.
func fixtureManifest(version string) *release.Manifest {
	m := &release.Manifest{
		SchemaVersion: 1, Version: version, Revision: strings.Repeat("a", 40),
		Images: map[string]release.Image{}, ThirdParty: map[string]release.Image{},
		Lifecycle: fixtureImage("lifecycle"), Plugins: []release.Plugin{},
		SupportedUpgradeSources: []string{},
	}
	for _, name := range Products {
		m.Images[name] = fixtureImage(name)
	}
	for _, name := range Sources {
		m.ThirdParty[name] = fixtureImage(name)
	}
	for _, name := range Sources {
		m.Plugins = append(m.Plugins, release.Plugin{
			ID: name + "-exporter", Version: version, OS: "linux", Arch: "amd64", Purpose: "current",
			Archive: "sha256:" + strings.Repeat("3", 64), Entrypoint: "sha256:" + strings.Repeat("4", 64),
			Schema: "sha256:" + strings.Repeat("5", 64),
		})
	}
	m.Plugins = append(m.Plugins, release.Plugin{
		ID: "redis-exporter", Version: "1.9.4", OS: "linux", Arch: "amd64", Purpose: "upgrade-only",
		Archive: "sha256:" + strings.Repeat("6", 64), Entrypoint: "sha256:" + strings.Repeat("7", 64),
	})
	return m
}

// productTopology stands in for the resolved Compose model in unit tests.
func productTopology() []byte {
	return []byte("{\n  \"services\": {}\n}\n")
}

// fixtureRepo materializes the bundle inputs WriteBundle reads from the tree.
func fixtureRepo(t *testing.T) *Repo {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"deploy/release/BUNDLE-README.md":      "# bundle\n",
		"deploy/runtime-contracts.json":        "{\"contract_version\":\"2\"}\n",
		"deploy/runtime-contracts.schema.json": "{\"type\":\"object\"}\n",
	}
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &Repo{Root: root}
}

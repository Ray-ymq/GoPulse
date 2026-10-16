package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestExternalCatalogLoadsDescriptorAndPackage(t *testing.T) {
	entry := OfficialCatalog()[0]
	entry.SchemaVersion = 1
	entry.Schema, _ = OfficialSchema(entry.ID)
	m, schema := v2Manifest(t)
	binary := []byte("external-catalog-fixture")
	binarySum := sha256.Sum256(binary)
	m.EntrypointSHA256 = hex.EncodeToString(binarySum[:])
	descriptor := struct {
		SchemaVersion          int    `json:"schema_version"`
		ID                     string `json:"id"`
		Name                   string `json:"name"`
		Source                 string `json:"source"`
		TargetID               string `json:"target_id"`
		Entrypoint             string `json:"entrypoint"`
		HealthPath             string `json:"health_path"`
		MetricsPath            string `json:"metrics_path"`
		ListenPort             int    `json:"listen_port"`
		HostPort               int    `json:"host_port"`
		ContainerPort          int    `json:"container_port"`
		RuntimeContractVersion int    `json:"runtime_contract_version"`
		MetricsContractVersion int    `json:"metrics_contract_version"`
		ConfigSchemaPath       string `json:"config_schema_path"`
	}{1, entry.ID, entry.Name, entry.Source, entry.TargetID, entry.Entrypoint, entry.HealthPath, entry.MetricsPath, entry.Port, entry.HostPort, entry.ContainerPort, entry.RuntimeContractVersion, entry.MetricsContractVersion, entry.ConfigSchemaPath}
	manifest, _ := json.Marshal(m)
	descriptorData, _ := json.Marshal(descriptor)
	archive := writeArchive(t, map[string][]byte{"plugin.json": manifest, "config.schema.json": schema, "descriptor.json": descriptorData, m.Entrypoint: binary})
	images := t.TempDir()
	packagePath := filepath.Join(images, "redis-current.tar.gz")
	archiveData, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(packagePath, archiveData, 0600); err != nil {
		t.Fatal(err)
	}
	digest, err := archiveDigest(packagePath)
	if err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(images, "catalog.json")
	data, err := json.Marshal(CatalogSnapshot{SchemaVersion: 1, Modules: []CatalogEntry{entry}, Releases: []Release{{Manifest: m, ArchiveSHA256: digest, Purpose: "current", PackageFile: filepath.Base(packagePath)}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(catalogPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(t.Context(), ManagerConfig{Root: t.TempDir(), PackagesRoot: images, CatalogPath: catalogPath, ExporterEnv: map[string]string{"GOPULSE_RUNTIME_MODE": "host"}})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Shutdown(t.Context())
	entries := manager.ExecutionCatalog()
	if len(entries) != 1 || entries[0].ID != entry.ID || entries[0].Schema.PluginID != entry.ID {
		t.Fatalf("unexpected external catalog: %+v", entries)
	}
	if _, err = manager.core.catalog.extractVerified(entry.ID, m.Version, "", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		t.Fatal("test requires a supported runtime architecture")
	}
}

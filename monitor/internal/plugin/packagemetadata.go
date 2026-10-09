package plugin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// WritePackageMetadata writes the canonical v2 manifest and configuration schema
// into directory for one official source. The entrypoint executable must already
// exist at its catalog path. It does not register the resulting archive as a
// trusted official release.
func WritePackageMetadata(directory, version, arch, source string) error {
	if directory == "" || arch == "" {
		return fmt.Errorf("missing package parameters")
	}
	if _, err := CompareSemver(version, version); err != nil {
		return err
	}
	entry, ok := LookupOfficial(source + "-exporter")
	if !ok || !entry.Available {
		return fmt.Errorf("unsupported source")
	}
	binary, err := os.ReadFile(filepath.Join(directory, entry.Entrypoint))
	if err != nil {
		return err
	}
	schema, err := OfficialSchemaJSON(entry.ID)
	if err != nil {
		return err
	}
	schemaSum, binarySum := sha256.Sum256(schema), sha256.Sum256(binary)
	manifest := Manifest{
		SchemaVersion: 2, ID: entry.ID, Name: "GoPulse " + source + " Exporter", Version: version,
		Kind: "metrics-exporter", Source: entry.Source, OS: "linux", Arch: arch,
		Entrypoint: entry.Entrypoint, EntrypointSHA256: hex.EncodeToString(binarySum[:]),
		HealthPath: "/health", MetricsPath: "/metrics", RuntimeContractVersion: 1, MetricsContractVersion: 2,
		ConfigSchemaPath: "config.schema.json", ConfigSchemaSHA256: hex.EncodeToString(schemaSum[:]),
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(directory, "config.schema.json"), schema, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory, "plugin.json"), append(data, '\n'), 0o644)
}

// LegacyManifest renders the frozen v1 manifest for a migrated Redis release.
// Archives built from these bytes are pinned by published digests, so the encoding
// must keep sorted keys, compact separators, an integer schema_version and a
// trailing newline. It must not reuse the Manifest struct field order.
func LegacyManifest(version, arch, entrypoint, entrypointSHA256 string) ([]byte, error) {
	if _, err := CompareSemver(version, version); err != nil {
		return nil, err
	}
	value := map[string]any{
		"schema_version": 1, "id": PluginID, "name": "GoPulse Redis Exporter",
		"version": version, "kind": "metrics-exporter", "source": "redis", "os": "linux",
		"arch": arch, "entrypoint": entrypoint, "entrypoint_sha256": entrypointSHA256,
		"health_path": "/health", "metrics_path": "/metrics",
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

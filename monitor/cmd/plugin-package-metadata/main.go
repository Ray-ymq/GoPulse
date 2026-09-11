// Command plugin-package-metadata emits the canonical v2 manifest and schema.
// It does not register the resulting archive as a trusted official release.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Ray-ymq/GoPulse/monitor/internal/plugin"
)

func main() {
	directory := flag.String("directory", "", "package directory containing the executable")
	version := flag.String("version", "", "three-part release version")
	arch := flag.String("arch", "", "Linux architecture")
	source := flag.String("source", "redis", "official source")
	flag.Parse()
	if err := generateSource(*directory, *version, *arch, *source); err != nil {
		fmt.Fprintln(os.Stderr, "package metadata generation failed")
		os.Exit(1)
	}
}

func generate(directory, version, arch string) error {
	return generateSource(directory, version, arch, "redis")
}
func generateSource(directory, version, arch, source string) error {
	if directory == "" || arch == "" {
		return fmt.Errorf("missing package parameters")
	}
	if _, err := plugin.CompareSemver(version, version); err != nil {
		return err
	}
	entry, ok := plugin.LookupOfficial(source + "-exporter")
	if !ok || !entry.Available {
		return fmt.Errorf("unsupported source")
	}
	binary, err := os.ReadFile(filepath.Join(directory, entry.Entrypoint))
	if err != nil {
		return err
	}
	schema, err := plugin.OfficialSchemaJSON(entry.ID)
	if err != nil {
		return err
	}
	schemaSum, binarySum := sha256.Sum256(schema), sha256.Sum256(binary)
	manifest := plugin.Manifest{
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
	if err = os.WriteFile(filepath.Join(directory, "config.schema.json"), schema, 0644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory, "plugin.json"), append(data, '\n'), 0644)
}

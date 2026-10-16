package packaging

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Ray-ymq/GoPulse/lifecycle/internal/release"
)

// elfMachines maps the supported plugin architectures to their ELF machine.
var elfMachines = map[string]uint16{"amd64": 62, "arm64": 183}

// pluginManifest is the closed package metadata inside one plugin archive.
type pluginManifest struct {
	SchemaVersion      int    `json:"schema_version"`
	ID                 string `json:"id"`
	Version            string `json:"version"`
	OS                 string `json:"os"`
	Arch               string `json:"arch"`
	Entrypoint         string `json:"entrypoint"`
	EntrypointSHA256   string `json:"entrypoint_sha256"`
	ConfigSchemaSHA256 string `json:"config_schema_sha256"`
}

// PluginRecords reads independently published Exporter packages from the
// deployment input directory and records their identity. It only copies and
// validates package bytes; foreign-architecture binaries are never executed.
// The image argument remains for source compatibility with callers that also
// validate the Monitor image in the same loop.
func PluginRecords(image release.Image, platform, directory string, packageRoots ...string) ([]release.Plugin, error) {
	_ = image
	if len(packageRoots) == 0 || packageRoots[0] == "" {
		return nil, errors.New("Exporter package input directory is required")
	}
	packageRoot, err := filepath.Abs(packageRoots[0])
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, err
	}
	arch := platform[strings.LastIndexByte(platform, '/')+1:]
	names := make([]string, 0, len(Sources)+1)
	for _, source := range Sources {
		name, findErr := currentPackage(packageRoot, source, arch)
		if findErr != nil {
			return nil, findErr
		}
		names = append(names, name)
	}
	if platform == "linux/amd64" {
		// The 1.9.4 upgrade input is amd64-only and never a current package.
		names = append(names, "redis-1.9.4.tar.gz")
	}
	records := make([]release.Plugin, 0, len(names))
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(packageRoot, name))
		if err != nil {
			return nil, fmt.Errorf("Exporter package input has no %s", name)
		}
		record, err := pluginRecord(data, arch)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if err = os.WriteFile(filepath.Join(directory, name), data, 0o644); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := copyCatalog(packageRoot, directory, arch); err != nil {
		return nil, err
	}
	return records, nil
}

func copyCatalog(root, directory, arch string) error {
	candidates := []string{
		filepath.Join(root, "catalog-linux-"+arch+".json"),
		filepath.Join(root, "catalog-"+arch+".json"),
		filepath.Join(root, "catalog.json"),
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err != nil {
			continue
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("Exporter catalog input is not a regular file: %s", filepath.Base(candidate))
		}
		data, err := os.ReadFile(candidate)
		if err != nil || len(data) == 0 || len(data) > 64<<10 || !json.Valid(data) {
			return errors.New("Exporter catalog input is invalid")
		}
		if err := os.WriteFile(filepath.Join(directory, "catalog.json"), data, 0o644); err != nil {
			return err
		}
		return nil
	}
	return errors.New("Exporter catalog input is missing")
}

func currentPackage(root, source, arch string) (string, error) {
	patterns := []string{
		filepath.Join(root, "gopulse-"+source+"-exporter-linux-"+arch+".tar.gz"),
		filepath.Join(root, "gopulse-"+source+"-exporter.tar.gz"),
	}
	for _, path := range patterns {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return filepath.Base(path), nil
		}
	}
	candidates, err := filepath.Glob(filepath.Join(root, "gopulse-"+source+"-exporter-*-linux-"+arch+".tar.gz"))
	if err != nil || len(candidates) == 0 {
		return "", fmt.Errorf("Exporter package input has no current %s package for %s", source, arch)
	}
	sort.Strings(candidates)
	return filepath.Base(candidates[len(candidates)-1]), nil
}

// pluginRecord validates one plugin archive against its own metadata without
// executing anything from the archive.
func pluginRecord(data []byte, arch string) (release.Plugin, error) {
	members, err := archiveMembers(data)
	if err != nil {
		return release.Plugin{}, err
	}
	var manifest pluginManifest
	if err := json.Unmarshal(members["plugin.json"], &manifest); err != nil {
		return release.Plugin{}, errors.New("invalid plugin manifest")
	}
	entrypoint, ok := members[manifest.Entrypoint]
	if !ok || manifest.Entrypoint == "" || len(entrypoint) < 20 {
		return release.Plugin{}, errors.New("plugin archive has no entrypoint")
	}
	if Sum(entrypoint)[7:] != manifest.EntrypointSHA256 {
		return release.Plugin{}, errors.New("plugin entrypoint checksum mismatch")
	}
	machine := binary.LittleEndian.Uint16(entrypoint[18:20])
	if !bytes.HasPrefix(entrypoint, []byte{0x7f, 'E', 'L', 'F'}) || machine != elfMachines[arch] || manifest.Arch != arch || manifest.OS != "linux" {
		return release.Plugin{}, errors.New("plugin ELF/manifest architecture mismatch")
	}
	schemaDigest := ""
	if manifest.SchemaVersion == 2 {
		schema, ok := members["config.schema.json"]
		if !ok {
			return release.Plugin{}, errors.New("plugin archive has no config schema")
		}
		schemaDigest = Sum(schema)
		if schemaDigest[7:] != manifest.ConfigSchemaSHA256 {
			return release.Plugin{}, errors.New("plugin schema digest mismatch")
		}
	}
	purpose := "upgrade-only"
	if schemaDigest != "" {
		purpose = "current"
	}
	return release.Plugin{
		ID: manifest.ID, Version: manifest.Version, OS: manifest.OS, Arch: manifest.Arch,
		Purpose: purpose, Archive: Sum(data), Entrypoint: Sum(entrypoint), Schema: schemaDigest,
	}, nil
}

// archiveMembers reads every regular member of a gzipped tar archive. Plugin
// packages are small, so keeping them in memory is cheaper than two passes.
func archiveMembers(data []byte) (map[string][]byte, error) {
	archive, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("plugin archive is not gzip")
	}
	defer archive.Close()
	members := map[string][]byte{}
	reader := tar.NewReader(archive)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return members, nil
		}
		if err != nil {
			return nil, errors.New("invalid plugin archive")
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		member, err := io.ReadAll(reader)
		if err != nil {
			return nil, errors.New("invalid plugin archive")
		}
		members[header.Name] = member
	}
}

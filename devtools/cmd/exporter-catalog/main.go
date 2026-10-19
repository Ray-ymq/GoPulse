// Command exporter-catalog creates the read-only deployment catalog consumed by
// Monitor. It inspects already-built packages and therefore does not compile
// any Exporter or import Monitor's internal packages.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
)

type descriptor struct {
	SchemaVersion          int          `json:"schema_version"`
	ID                     string       `json:"id"`
	Name                   string       `json:"name"`
	Source                 string       `json:"source"`
	TargetID               string       `json:"target_id"`
	Entrypoint             string       `json:"entrypoint"`
	HealthPath             string       `json:"health_path"`
	MetricsPath            string       `json:"metrics_path"`
	ListenPort             int          `json:"listen_port"`
	HostPort               int          `json:"host_port"`
	ContainerPort          int          `json:"container_port"`
	RuntimeContractVersion int          `json:"runtime_contract_version"`
	MetricsContractVersion int          `json:"metrics_contract_version"`
	ConfigSchemaPath       string       `json:"config_schema_path"`
	Available              bool         `json:"available,omitempty"`
	Schema                 configSchema `json:"schema,omitempty"`
}

type configSchema struct {
	SchemaVersion int           `json:"schema_version"`
	PluginID      string        `json:"plugin_id"`
	Fields        []configField `json:"fields"`
}
type configField struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Secret   bool     `json:"secret"`
	Minimum  *int     `json:"minimum,omitempty"`
	Maximum  *int     `json:"maximum,omitempty"`
	Enum     []string `json:"enum,omitempty"`
}

type manifest struct {
	SchemaVersion          int    `json:"schema_version"`
	ID                     string `json:"id"`
	Name                   string `json:"name"`
	Version                string `json:"version"`
	Kind                   string `json:"kind"`
	Source                 string `json:"source"`
	OS                     string `json:"os"`
	Arch                   string `json:"arch"`
	Entrypoint             string `json:"entrypoint"`
	EntrypointSHA256       string `json:"entrypoint_sha256"`
	HealthPath             string `json:"health_path"`
	MetricsPath            string `json:"metrics_path"`
	RuntimeContractVersion int    `json:"runtime_contract_version"`
	MetricsContractVersion int    `json:"metrics_contract_version"`
	ConfigSchemaPath       string `json:"config_schema_path"`
	ConfigSchemaSHA256     string `json:"config_schema_sha256"`
}

type release struct {
	Manifest      manifest `json:"manifest"`
	ArchiveSHA256 string   `json:"archive_sha256"`
	Purpose       string   `json:"purpose"`
	PackageFile   string   `json:"package_file"`
}
type catalog struct {
	SchemaVersion int          `json:"schema_version"`
	Modules       []descriptor `json:"modules"`
	Releases      []release    `json:"releases"`
}

type packageArgument []string

func (p *packageArgument) String() string { return strings.Join(*p, ",") }
func (p *packageArgument) Set(value string) error {
	if !strings.Contains(value, "=") {
		return errors.New("package must use purpose=path")
	}
	*p = append(*p, value)
	return nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "exporter-catalog:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("exporter-catalog", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	output := flags.String("output", "", "catalog output path")
	packagesRoot := flags.String("packages-root", "", "directory containing approved packages")
	arch := flags.String("arch", runtime.GOARCH, "target Linux architecture")
	var inputs packageArgument
	flags.Var(&inputs, "package", "approved package in purpose=path form; may repeat")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if len(inputs) == 0 || *arch != "amd64" && *arch != "arm64" || *output == "" || *packagesRoot == "" {
		return errors.New("output, packages-root, architecture and at least one package are required")
	}
	root, err := filepath.Abs(*packagesRoot)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	result := catalog{SchemaVersion: 1, Modules: []descriptor{}, Releases: []release{}}
	modules := map[string]descriptor{}
	current := map[string]bool{}
	seenRelease := map[string]bool{}
	type inspectedPackage struct {
		purpose string
		path    string
		meta    descriptor
		m       manifest
		schema  configSchema
	}
	inspected := make([]inspectedPackage, 0, len(inputs))
	for _, input := range inputs {
		purpose, packagePath, _ := strings.Cut(input, "=")
		if purpose != "current" && purpose != "retained" && purpose != "legacy-v1" {
			return errors.New("unsupported release purpose")
		}
		packagePath, err = filepath.Abs(packagePath)
		if err != nil {
			return err
		}
		meta, m, schema, err := inspectPackage(packagePath, *arch)
		if err != nil {
			return err
		}
		if purpose == "legacy-v1" {
			if m.SchemaVersion != 1 {
				return errors.New("legacy release has the wrong contract")
			}
		} else {
			if m.SchemaVersion != 2 {
				return errors.New("current release has the wrong contract")
			}
			if previous, ok := modules[meta.ID]; ok && !reflect.DeepEqual(previous, meta) {
				return fmt.Errorf("module descriptor mismatch for %s", meta.ID)
			}
			meta.Available = true
			meta.Schema = schema
			modules[meta.ID] = meta
		}
		inspected = append(inspected, inspectedPackage{purpose: purpose, path: packagePath, meta: meta, m: m, schema: schema})
	}
	for _, item := range inspected {
		if item.purpose == "legacy-v1" {
			if _, ok := modules[item.m.ID]; !ok {
				return fmt.Errorf("legacy release %s has no current module descriptor", item.m.ID)
			}
		}
		m, purpose, packagePath := item.m, item.purpose, item.path
		name := filepath.Base(packagePath)
		destination := filepath.Join(root, name)
		if packagePath != destination {
			data, readErr := os.ReadFile(packagePath)
			if readErr != nil {
				return readErr
			}
			if writeErr := os.WriteFile(destination, data, 0o644); writeErr != nil {
				return writeErr
			}
		}
		digest, err := fileDigest(destination)
		if err != nil {
			return err
		}
		key := m.ID + "/" + m.Version + "/" + m.Arch
		if seenRelease[key] || purpose == "current" && current[m.ID] {
			return errors.New("duplicate release")
		}
		seenRelease[key] = true
		if purpose == "current" {
			current[m.ID] = true
		}
		result.Releases = append(result.Releases, release{Manifest: m, ArchiveSHA256: digest, Purpose: purpose, PackageFile: name})
	}
	for _, item := range modules {
		result.Modules = append(result.Modules, item)
	}
	sort.Slice(result.Modules, func(i, j int) bool { return result.Modules[i].ID < result.Modules[j].ID })
	sort.Slice(result.Releases, func(i, j int) bool {
		if result.Releases[i].Manifest.ID != result.Releases[j].Manifest.ID {
			return result.Releases[i].Manifest.ID < result.Releases[j].Manifest.ID
		}
		return result.Releases[i].Manifest.Version < result.Releases[j].Manifest.Version
	})
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	outputPath, err := filepath.Abs(*output)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(outputPath, append(data, '\n'), 0o644)
}

func inspectPackage(path string, arch string) (descriptor, manifest, configSchema, error) {
	file, err := os.Open(path)
	if err != nil {
		return descriptor{}, manifest{}, configSchema{}, err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return descriptor{}, manifest{}, configSchema{}, err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	files := map[string][]byte{}
	for count := 0; ; count++ {
		header, nextErr := reader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		cleanName := filepath.ToSlash(filepath.Clean(header.Name))
		_, duplicate := files[header.Name]
		if nextErr != nil || count > 16 || header.Name == "" || cleanName != header.Name || strings.HasPrefix(cleanName, "../") || cleanName == ".." || strings.HasPrefix(cleanName, "/") || header.Typeflag != tar.TypeReg || header.Size < 0 || header.Size > 64<<20 || duplicate {
			return descriptor{}, manifest{}, configSchema{}, errors.New("invalid exporter package")
		}
		value, readErr := io.ReadAll(io.LimitReader(reader, header.Size+1))
		if readErr != nil || int64(len(value)) != header.Size || len(value) > 64<<20 {
			return descriptor{}, manifest{}, configSchema{}, errors.New("invalid exporter package")
		}
		files[header.Name] = value
	}
	var meta descriptor
	var m manifest
	var schema configSchema
	if err = decode(files["plugin.json"], &m); err != nil {
		return descriptor{}, manifest{}, configSchema{}, errors.New("invalid exporter package metadata")
	}
	if m.SchemaVersion == 1 {
		binary, ok := files[m.Entrypoint]
		if len(files) != 2 || m.ID != "redis-exporter" || m.Source != "redis" || m.OS != "linux" || m.Arch != arch || m.Entrypoint != "bin/gopulse-redis-exporter" || !ok || !validDigest(m.EntrypointSHA256, binary) {
			return descriptor{}, manifest{}, configSchema{}, errors.New("legacy exporter package metadata does not match")
		}
		return descriptor{}, m, configSchema{}, nil
	}
	if err = decode(files["descriptor.json"], &meta); err != nil {
		return descriptor{}, manifest{}, configSchema{}, errors.New("invalid exporter package metadata")
	}
	if err = decode(files["config.schema.json"], &schema); err != nil {
		return descriptor{}, manifest{}, configSchema{}, errors.New("invalid exporter package metadata")
	}
	if len(files) != 4 || !validDescriptor(meta) || m.SchemaVersion != 2 || m.ID != meta.ID || m.Name != meta.Name || m.Source != meta.Source || m.Arch != arch || m.Entrypoint != meta.Entrypoint || m.HealthPath != meta.HealthPath || m.MetricsPath != meta.MetricsPath || m.RuntimeContractVersion != meta.RuntimeContractVersion || m.MetricsContractVersion != meta.MetricsContractVersion || files[m.Entrypoint] == nil || schema.SchemaVersion != 1 || schema.PluginID != meta.ID || !validConfigSchema(schema) {
		return descriptor{}, manifest{}, configSchema{}, errors.New("exporter package metadata does not match")
	}
	binarySum := sha256.Sum256(files[m.Entrypoint])
	schemaSum := sha256.Sum256(files["config.schema.json"])
	if m.EntrypointSHA256 != hex.EncodeToString(binarySum[:]) || m.ConfigSchemaSHA256 != hex.EncodeToString(schemaSum[:]) {
		return descriptor{}, manifest{}, configSchema{}, errors.New("exporter package digest does not match")
	}
	return meta, m, schema, nil
}

func validDigest(expected string, data []byte) bool {
	if len(expected) != 64 {
		return false
	}
	sum := sha256.Sum256(data)
	return expected == hex.EncodeToString(sum[:])
}

func decode(data []byte, value any) error {
	if len(data) == 0 {
		return errors.New("missing metadata")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

func validDescriptor(meta descriptor) bool {
	clean := filepath.ToSlash(filepath.Clean(meta.Entrypoint))
	return meta.SchemaVersion == 1 && validModuleID(meta.ID) && validSafeName(meta.Name, 80) && validSource(meta.Source) && validModuleID(meta.TargetID) &&
		validPackagePath(clean, meta.Entrypoint) && validHTTPPath(meta.HealthPath) && validHTTPPath(meta.MetricsPath) &&
		meta.ListenPort >= 1 && meta.ListenPort <= 65535 && meta.HostPort >= 0 && meta.HostPort <= 65535 &&
		meta.ContainerPort >= 0 && meta.ContainerPort <= 65535 && meta.RuntimeContractVersion == 1 &&
		meta.MetricsContractVersion == 2 && meta.ConfigSchemaPath == "config.schema.json"
}

func validModuleID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for index, r := range value {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
		if index == 0 && (r < 'a' || r > 'z') {
			return false
		}
	}
	return true
}

func validSource(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for index, r := range value {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' && r != '-' {
			return false
		}
		if index == 0 && (r < 'a' || r > 'z') {
			return false
		}
	}
	return true
}

func validSafeName(value string, limit int) bool {
	if value == "" || len(value) > limit || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func validPackagePath(clean, original string) bool {
	return strings.HasPrefix(clean, "bin/") && clean == original && !strings.Contains(original, "\\")
}

func validHTTPPath(value string) bool {
	clean := filepath.ToSlash(filepath.Clean(value))
	return strings.HasPrefix(value, "/") && value == clean && !strings.ContainsAny(value, "\x00\r\n") && !strings.Contains(value, "//")
}

func validConfigSchema(schema configSchema) bool {
	seen := map[string]bool{}
	for _, field := range schema.Fields {
		if !validConfigField(field) || seen[field.Name] {
			return false
		}
		seen[field.Name] = true
	}
	return len(schema.Fields) > 0
}

func validConfigField(field configField) bool {
	if !validFieldName(field.Name) || (field.Type == "secret") != field.Secret {
		return false
	}
	switch field.Type {
	case "hostname", "port", "integer", "string", "secret", "duration", "url":
	default:
		return false
	}
	if field.Minimum != nil && field.Maximum != nil && *field.Minimum > *field.Maximum {
		return false
	}
	seen := map[string]bool{}
	for _, item := range field.Enum {
		if item == "" || seen[item] {
			return false
		}
		seen[item] = true
	}
	return true
}

func validFieldName(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for index, r := range value {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
		if index == 0 && (r < 'a' || r > 'z') {
			return false
		}
	}
	return true
}

func fileDigest(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

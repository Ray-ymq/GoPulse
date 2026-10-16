// Command exporter-package builds one self-contained Exporter package. It has
// no dependency on Monitor, so a module can publish its binary and metadata
// without rebuilding or importing the process manager.
package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

var identifier = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

type descriptor struct {
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

const (
	legacyContractVersion  = 1
	currentContractVersion = 2
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "exporter-package:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("exporter-package", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	source := flags.String("source", "", "exporter module name")
	version := flags.String("version", "", "module version")
	output := flags.String("output", "", "package output path")
	binary := flags.String("binary", "", "prebuilt executable")
	arch := flags.String("arch", runtime.GOARCH, "target Linux architecture")
	repoRoot := flags.String("repo-root", "", "repository root")
	contractVersion := flags.Int("contract-version", currentContractVersion, "package contract version")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected argument")
	}
	root, err := findRoot(*repoRoot)
	if err != nil {
		return err
	}
	if !identifier.MatchString(*source) || *arch != "amd64" && *arch != "arm64" {
		return errors.New("source and architecture are invalid")
	}
	if *contractVersion != legacyContractVersion && *contractVersion != currentContractVersion {
		return errors.New("contract version must be 1 or 2")
	}
	if *contractVersion == legacyContractVersion && *source != "redis" {
		return errors.New("only Redis publishes a legacy package")
	}
	module := filepath.Join(root, "exporters", *source)
	var meta descriptor
	var descriptorData, schema []byte
	if *contractVersion == currentContractVersion {
		descriptorData, err = os.ReadFile(filepath.Join(module, "descriptor.json"))
		if err != nil {
			return err
		}
		if err = decode(descriptorData, &meta); err != nil || !validDescriptor(meta, *source) {
			return errors.New("invalid exporter descriptor")
		}
		schema, err = os.ReadFile(filepath.Join(module, meta.ConfigSchemaPath))
		if err != nil {
			return err
		}
		if len(schema) == 0 || len(schema) > 64<<10 || !json.Valid(schema) {
			return errors.New("invalid exporter config schema")
		}
	}
	if *version == "" {
		versionBytes, readErr := os.ReadFile(filepath.Join(module, "VERSION"))
		if readErr != nil {
			return readErr
		}
		*version = strings.TrimSpace(string(versionBytes))
	}
	if !semver(*version) {
		return errors.New("version must be a three-part SemVer")
	}
	if *binary == "" {
		binary = new(string)
		*binary = filepath.Join(os.TempDir(), fmt.Sprintf("gopulse-%s-exporter-%d", *source, time.Now().UnixNano()))
		defer os.Remove(*binary)
		command := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-ldflags=-buildid=", "-o", *binary, "./cmd/"+*source+"-exporter")
		command.Dir = module
		command.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+*arch)
		command.Stdout, command.Stderr = os.Stderr, os.Stderr
		if err = command.Run(); err != nil {
			return fmt.Errorf("build failed: %w", err)
		}
	}
	binaryData, err := os.ReadFile(*binary)
	if err != nil {
		return err
	}
	if len(binaryData) == 0 {
		return errors.New("exporter executable is empty")
	}
	entrypoint := "bin/gopulse-" + *source + "-exporter"
	if *contractVersion == currentContractVersion {
		entrypoint = filepath.ToSlash(meta.Entrypoint)
	}
	if !strings.HasPrefix(entrypoint, "bin/") || filepath.Base(entrypoint) == "bin" {
		return errors.New("invalid exporter entrypoint")
	}
	binarySum := sha256.Sum256(binaryData)
	manifestData := []byte{}
	if *contractVersion == legacyContractVersion {
		manifestData, err = legacyManifest(*version, *arch, entrypoint, hex.EncodeToString(binarySum[:]))
	} else {
		schemaSum := sha256.Sum256(schema)
		value := manifest{SchemaVersion: currentContractVersion, ID: meta.ID, Name: meta.Name, Version: *version, Kind: "metrics-exporter", Source: meta.Source, OS: "linux", Arch: *arch, Entrypoint: entrypoint, EntrypointSHA256: hex.EncodeToString(binarySum[:]), HealthPath: meta.HealthPath, MetricsPath: meta.MetricsPath, RuntimeContractVersion: meta.RuntimeContractVersion, MetricsContractVersion: meta.MetricsContractVersion, ConfigSchemaPath: meta.ConfigSchemaPath, ConfigSchemaSHA256: hex.EncodeToString(schemaSum[:])}
		manifestData, err = json.Marshal(value)
	}
	if err != nil {
		return err
	}
	if *output == "" {
		*output = filepath.Join(root, ".run", "packages", fmt.Sprintf("gopulse-%s-%s-linux-%s.tar.gz", *source, *version, *arch))
	}
	outputPath, err := filepath.Abs(*output)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	members := []member{{"plugin.json", manifestData, 0o644}}
	if *contractVersion == currentContractVersion {
		members = append(members, member{meta.ConfigSchemaPath, schema, 0o644}, member{"descriptor.json", descriptorData, 0o644})
	}
	members = append(members, member{entrypoint, binaryData, 0o755})
	return writePackage(outputPath, members)
}

type member struct {
	name string
	data []byte
	mode int64
}

func writePackage(output string, members []member) error {
	archivePath := output + ".tar.tmp"
	file, err := os.Create(archivePath)
	if err != nil {
		return err
	}
	tarWriter := tar.NewWriter(file)
	for _, item := range members {
		header := &tar.Header{Name: item.name, Mode: item.mode, Size: int64(len(item.data)), ModTime: time.Unix(0, 0), AccessTime: time.Time{}, ChangeTime: time.Time{}, Uid: 0, Gid: 0, Format: tar.FormatUSTAR}
		if err = tarWriter.WriteHeader(header); err == nil {
			_, err = tarWriter.Write(item.data)
		}
		if err != nil {
			file.Close()
			os.Remove(archivePath)
			return err
		}
	}
	if err = tarWriter.Close(); err == nil {
		err = file.Close()
	} else {
		_ = file.Close()
	}
	if err != nil {
		os.Remove(archivePath)
		return err
	}
	if err = normalizeTarHeaders(archivePath); err != nil {
		os.Remove(archivePath)
		return err
	}
	defer os.Remove(archivePath)
	compressed, err := os.Create(output + ".tmp")
	if err != nil {
		return err
	}
	command := exec.Command("gzip", "-n", "-9", "-c", archivePath)
	command.Stdout = compressed
	command.Stderr = os.Stderr
	err = command.Run()
	if closeErr := compressed.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(output + ".tmp")
		return err
	}
	return os.Rename(output+".tmp", output)
}

// normalizeTarHeaders matches the historical package bytes. The old producer
// used GNU tar, which left regular-file device fields empty and padded every
// archive to its 10 KiB record boundary.
func normalizeTarHeaders(archive string) error {
	data, err := os.ReadFile(archive)
	if err != nil {
		return err
	}
	for offset := 0; offset+512 <= len(data); {
		header := data[offset : offset+512]
		allZero := true
		for _, value := range header {
			if value != 0 {
				allZero = false
				break
			}
		}
		if allZero {
			break
		}
		for index := 329; index < 345; index++ {
			header[index] = 0
		}
		for index := 148; index < 156; index++ {
			header[index] = ' '
		}
		var checksum int
		for _, value := range header {
			checksum += int(value)
		}
		copy(header[148:156], []byte(fmt.Sprintf("%06o\x00 ", checksum)))
		rawSize := strings.Trim(string(header[124:136]), " \x00")
		size, parseErr := parseTarSize(rawSize)
		if parseErr != nil || size < 0 {
			return errors.New("invalid USTAR member size")
		}
		offset += 512 + ((int(size) + 511) / 512 * 512)
	}
	if remainder := len(data) % 10240; remainder != 0 {
		data = append(data, make([]byte, 10240-remainder)...)
	}
	return os.WriteFile(archive, data, 0o600)
}

func parseTarSize(value string) (int64, error) {
	var size int64
	for _, r := range value {
		if r < '0' || r > '7' {
			return 0, errors.New("invalid octal size")
		}
		size = size*8 + int64(r-'0')
	}
	return size, nil
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

func validDescriptor(meta descriptor, source string) bool {
	clean := filepath.ToSlash(filepath.Clean(meta.Entrypoint))
	return meta.SchemaVersion == 1 && meta.ID != "" && meta.Name != "" && meta.Source == source && meta.TargetID != "" &&
		clean == meta.Entrypoint && strings.HasPrefix(clean, "bin/") && meta.HealthPath != "" && meta.MetricsPath != "" &&
		meta.ListenPort >= 1 && meta.ListenPort <= 65535 && meta.HostPort >= 0 && meta.HostPort <= 65535 &&
		meta.ContainerPort >= 0 && meta.ContainerPort <= 65535 && meta.RuntimeContractVersion == 1 &&
		meta.MetricsContractVersion == 2 && meta.ConfigSchemaPath == "config.schema.json"
}

func legacyManifest(version, arch, entrypoint, digest string) ([]byte, error) {
	value := map[string]any{
		"schema_version": 1, "id": "redis-exporter", "name": "GoPulse Redis Exporter", "version": version,
		"kind": "metrics-exporter", "source": "redis", "os": "linux", "arch": arch,
		"entrypoint": entrypoint, "entrypoint_sha256": digest, "health_path": "/health", "metrics_path": "/metrics",
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func semver(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

func findRoot(explicit string) (string, error) {
	if explicit != "" {
		return filepath.Abs(explicit)
	}
	directory, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if info, statErr := os.Stat(filepath.Join(directory, "exporters")); statErr == nil && info.IsDir() {
			return directory, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", errors.New("repository root cannot be located")
		}
		directory = parent
	}
}

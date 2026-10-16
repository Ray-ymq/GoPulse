package plugin

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

const MaxPackageBytes int64 = 64 << 20
const maxEntries = 32
const maxTotalBytes int64 = 128 << 20
const maxFileBytes int64 = 96 << 20

func extractPackage(archivePath, staging string) (Manifest, error) {
	return extractPackageContract(archivePath, staging, 1)
}

// extractPackageV2 verifies internal integrity only. It deliberately does not
// authorize execution: callers must also verify the image-owned release catalog.
func extractPackageV2(archivePath, staging string) (Manifest, error) {
	return extractPackageContract(archivePath, staging, 2)
}

func extractPackageContract(archivePath, staging string, schemaVersion int) (Manifest, error) {
	return extractPackageContractForArch(archivePath, staging, schemaVersion, runtime.GOARCH)
}

func extractPackageContractForArch(archivePath, staging string, schemaVersion int, arch string) (Manifest, error) {
	return extractPackageContractForEntry(archivePath, staging, schemaVersion, arch, nil)
}

func extractPackageContractForEntry(archivePath, staging string, schemaVersion int, arch string, expected *CatalogEntry) (Manifest, error) {
	info, err := os.Stat(archivePath)
	if err != nil || info.Size() > MaxPackageBytes {
		return Manifest{}, NewError(CodePackageInvalid, "plugin package is invalid")
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return Manifest{}, wrap(CodePackageInvalid, "plugin package is invalid", err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(io.LimitReader(file, MaxPackageBytes+1))
	if err != nil {
		return Manifest{}, NewError(CodePackageInvalid, "plugin package is invalid")
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	seen := map[string]bool{}
	var total int64
	entries := 0
	for {
		h, e := tr.Next()
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			return Manifest{}, NewError(CodePackageInvalid, "plugin package is invalid")
		}
		entries++
		if entries > maxEntries {
			return Manifest{}, NewError(CodePackageInvalid, "plugin package is invalid")
		}
		name := h.Name
		clean := path.Clean(name)
		if name == "" || len(name) > 240 || clean != name || clean == "." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) || seen[clean] {
			return Manifest{}, NewError(CodePackageInvalid, "plugin package is invalid")
		}
		if schemaVersion == 2 {
			if h.Mode & ^int64(0777) != 0 {
				return Manifest{}, NewError(CodePackageInvalid, "plugin package is invalid")
			}
			if h.Typeflag == tar.TypeDir {
				if clean != "bin" {
					return Manifest{}, NewError(CodePackageInvalid, "plugin package is invalid")
				}
			} else {
				allowed := clean == "plugin.json" || clean == "config.schema.json" || clean == "descriptor.json"
				if expected != nil {
					allowed = allowed || clean == expected.Entrypoint
				} else {
					for _, item := range OfficialCatalog() {
						if item.Available && clean == item.Entrypoint {
							allowed = true
						}
					}
				}
				if !allowed {
					return Manifest{}, NewError(CodePackageInvalid, "plugin package is invalid")
				}
			}
		}
		seen[clean] = true
		target := filepath.Join(staging, filepath.FromSlash(clean))
		rel, e := filepath.Rel(staging, target)
		if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return Manifest{}, NewError(CodePackageInvalid, "plugin package is invalid")
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if e = os.MkdirAll(target, 0750); e != nil {
				return Manifest{}, wrap(CodeFailed, "plugin operation failed", e)
			}
		case tar.TypeReg, tar.TypeRegA:
			if h.Size < 0 || h.Size > maxFileBytes {
				return Manifest{}, NewError(CodePackageInvalid, "plugin package is invalid")
			}
			total += h.Size
			if total > maxTotalBytes {
				return Manifest{}, NewError(CodePackageInvalid, "plugin package is invalid")
			}
			if (clean == "plugin.json" || (schemaVersion == 2 && clean == "config.schema.json")) && h.Size > 64<<10 {
				return Manifest{}, NewError(CodePackageInvalid, "plugin package is invalid")
			}
			if e = os.MkdirAll(filepath.Dir(target), 0750); e != nil {
				return Manifest{}, wrap(CodeFailed, "plugin operation failed", e)
			}
			out, e := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if e != nil {
				return Manifest{}, NewError(CodePackageInvalid, "plugin package is invalid")
			}
			_, copyErr := io.CopyN(out, tr, h.Size)
			syncErr := out.Sync()
			closeErr := out.Close()
			if copyErr != nil || syncErr != nil || closeErr != nil {
				return Manifest{}, NewError(CodePackageInvalid, "plugin package is invalid")
			}
		default:
			return Manifest{}, NewError(CodePackageInvalid, "plugin package is invalid")
		}
	}
	if !seen["plugin.json"] || (schemaVersion == 1 && !seen["bin/gopulse-redis-exporter"]) {
		return Manifest{}, NewError(CodePackageInvalid, "plugin package is invalid")
	}
	data, err := os.ReadFile(filepath.Join(staging, "plugin.json"))
	if err != nil {
		return Manifest{}, NewError(CodePackageInvalid, "plugin package is invalid")
	}
	manifest, err := parseManifestForArchWithEntry(data, schemaVersion, arch, expected)
	if err != nil {
		return Manifest{}, err
	}
	if expected != nil {
		manifest.TargetID = expected.TargetID
	}
	if schemaVersion == 2 {
		if expected == nil {
			official, known := LookupOfficial(manifest.ID)
			if !known || !official.Available {
				return Manifest{}, NewError(CodePackageInvalid, "plugin package is invalid")
			}
		} else if manifest.ID != expected.ID || !expected.Available {
			return Manifest{}, NewError(CodePackageInvalid, "plugin package is invalid")
		}
		if !seen[manifest.Entrypoint] || !seen["config.schema.json"] || (expected != nil && len(expected.Schema.Fields) > 0 && !seen["descriptor.json"]) {
			return Manifest{}, NewError(CodePackageInvalid, "plugin package is invalid")
		}
		for name := range seen {
			if name != "bin" && name != "plugin.json" && name != "config.schema.json" && name != "descriptor.json" && name != manifest.Entrypoint {
				return Manifest{}, NewError(CodePackageInvalid, "plugin package is invalid")
			}
		}
		schema, readErr := os.ReadFile(filepath.Join(staging, "config.schema.json"))
		validateSchema := ValidateConfigSchema
		if expected != nil && len(expected.Schema.Fields) > 0 {
			validateSchema = ValidateExternalConfigSchema
		}
		if readErr != nil || validateSchema(schema, manifest) != nil {
			return Manifest{}, NewError(CodePackageInvalid, "plugin package is invalid")
		}
		if expected != nil && len(expected.Schema.Fields) > 0 {
			descriptor, readErr := os.ReadFile(filepath.Join(staging, "descriptor.json"))
			if readErr != nil || !descriptorMatches(descriptor, *expected) {
				return Manifest{}, NewError(CodePackageInvalid, "plugin package is invalid")
			}
		}
	}
	entry := filepath.Join(staging, filepath.FromSlash(manifest.Entrypoint))
	entryInfo, err := os.Lstat(entry)
	if err != nil || !entryInfo.Mode().IsRegular() {
		return Manifest{}, NewError(CodePackageInvalid, "plugin package is invalid")
	}
	ef, err := os.Open(entry)
	if err != nil {
		return Manifest{}, NewError(CodePackageInvalid, "plugin package is invalid")
	}
	sum := sha256.New()
	_, err = io.Copy(sum, ef)
	closeErr := ef.Close()
	if err != nil || closeErr != nil || hex.EncodeToString(sum.Sum(nil)) != manifest.EntrypointSHA256 {
		return Manifest{}, NewError(CodePackageInvalid, "plugin package is invalid")
	}
	if err = os.Chmod(entry, 0750); err != nil {
		return Manifest{}, wrap(CodeFailed, "plugin operation failed", err)
	}
	return manifest, nil
}

func descriptorMatches(data []byte, expected CatalogEntry) bool {
	var descriptor struct {
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
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&descriptor) != nil {
		return false
	}
	return descriptor.SchemaVersion == 1 && descriptor.ID == expected.ID && descriptor.Name == expected.Name && descriptor.Source == expected.Source && descriptor.TargetID == expected.TargetID && descriptor.Entrypoint == expected.Entrypoint && descriptor.HealthPath == expected.HealthPath && descriptor.MetricsPath == expected.MetricsPath && descriptor.ListenPort == expected.Port && descriptor.HostPort == expected.HostPort && descriptor.ContainerPort == expected.ContainerPort && descriptor.RuntimeContractVersion == expected.RuntimeContractVersion && descriptor.MetricsContractVersion == expected.MetricsContractVersion && descriptor.ConfigSchemaPath == expected.ConfigSchemaPath
}

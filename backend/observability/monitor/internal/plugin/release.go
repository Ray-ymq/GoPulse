package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// Release pins content supplied by the image build, never by an API request or
// the plugin volume. Legacy releases deliberately have no schema digest.
type Release struct {
	Manifest      Manifest `json:"manifest"`
	ArchiveSHA256 string   `json:"archive_sha256"`
	Purpose       string   `json:"purpose"`
	PackageFile   string   `json:"package_file"`
}

type releaseCatalog struct {
	root     string
	releases []Release
	entries  []CatalogEntry
}

// CatalogSnapshot is the read-only deployment input consumed by Monitor. It
// contains only package pins and module descriptors; it is never written to
// the plugin state volume.
type CatalogSnapshot struct {
	SchemaVersion int            `json:"schema_version"`
	Modules       []CatalogEntry `json:"modules"`
	Releases      []Release      `json:"releases"`
}

// newReleaseCatalog is the runtime boundary for a compile-time generated list.
// In particular, it is not a loader for a writable on-volume catalog.
func newReleaseCatalog(root string, releases []Release) (*releaseCatalog, error) {
	return newReleaseCatalogForArch(root, releases, runtime.GOARCH)
}

func newReleaseCatalogForArch(root string, releases []Release, arch string) (*releaseCatalog, error) {
	return newReleaseCatalogForArchWithEntries(root, releases, arch, nil)
}

func newReleaseCatalogForArchWithEntries(root string, releases []Release, arch string, entries []CatalogEntry) (*releaseCatalog, error) {
	fail := func() (*releaseCatalog, error) {
		return nil, NewError(CodePackageInvalid, "official plugin release is invalid")
	}
	if !filepath.IsAbs(root) || rejectSymlinkComponents(root) != nil || requireDirectory(root) != nil {
		return fail()
	}
	seen := map[string]bool{}
	currents := map[string]bool{}
	for _, release := range releases {
		m := release.Manifest
		if !digestPattern.MatchString(release.ArchiveSHA256) || release.PackageFile != filepath.Base(release.PackageFile) || release.PackageFile == "." || release.PackageFile == "" {
			return fail()
		}
		key := m.ID + "/" + m.Version + "/" + m.OS + "/" + m.Arch
		if seen[key] {
			return fail()
		}
		seen[key] = true
		switch release.Purpose {
		case "legacy-v1":
			if m.SchemaVersion != 1 || m.ConfigSchemaSHA256 != "" || m.ConfigSchemaPath != "" || m.RuntimeContractVersion != 0 || m.MetricsContractVersion != 0 {
				return fail()
			}
		case "current", "retained":
			if m.SchemaVersion != 2 {
				return fail()
			}
			if release.Purpose == "current" {
				if currents[m.ID] {
					return fail()
				}
				currents[m.ID] = true
			}
		default:
			return fail()
		}
		// Reuse the closed manifest shape and identity checks for catalog metadata.
		var expected *CatalogEntry
		for i := range entries {
			if entries[i].ID == m.ID {
				expected = &entries[i]
				break
			}
		}
		if !validReleaseManifestForArchWithEntry(m, arch, expected) {
			return fail()
		}
	}
	seenEntries := map[string]bool{}
	for _, entry := range entries {
		if !validCatalogEntry(entry) || seenEntries[entry.ID] {
			return fail()
		}
		seenEntries[entry.ID] = true
	}
	owned := append([]Release(nil), releases...)
	for i := range owned {
		entry, ok := catalogEntryFor(entries, owned[i].Manifest.ID)
		if !ok {
			entry, ok = LookupOfficial(owned[i].Manifest.ID)
		}
		if ok {
			owned[i].Manifest.TargetID = entry.TargetID
		}
	}
	return &releaseCatalog{root: filepath.Clean(root), releases: owned, entries: append([]CatalogEntry(nil), entries...)}, nil
}

func catalogEntryFor(entries []CatalogEntry, id string) (CatalogEntry, bool) {
	for _, entry := range entries {
		if entry.ID == id {
			return entry, true
		}
	}
	return CatalogEntry{}, false
}

func validCatalogEntry(entry CatalogEntry) bool {
	cleanEntrypoint := path.Clean(entry.Entrypoint)
	if entry.SchemaVersion != 1 || !validModuleID(entry.ID) || !validSafeName(entry.Name, 80) || !validSource(entry.Source) || !validModuleID(entry.TargetID) || !validPackagePath(cleanEntrypoint, entry.Entrypoint) || !validHTTPPath(entry.HealthPath) || !validHTTPPath(entry.MetricsPath) || entry.Port < 1 || entry.Port > 65535 || entry.HostPort < 0 || entry.HostPort > 65535 || entry.ContainerPort < 0 || entry.ContainerPort > 65535 || entry.RuntimeContractVersion != 1 || entry.MetricsContractVersion != 2 || entry.ConfigSchemaPath != "config.schema.json" || entry.Schema.SchemaVersion != 1 || entry.Schema.PluginID != entry.ID || len(entry.Schema.Fields) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, field := range entry.Schema.Fields {
		if !validConfigField(field) || seen[field.Name] {
			return false
		}
		seen[field.Name] = true
	}
	return true
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
	clean := path.Clean(value)
	return strings.HasPrefix(value, "/") && value == clean && !strings.ContainsAny(value, "\x00\r\n") && !strings.Contains(value, "//")
}

// verifyArchive selects only an image-owned package. A supplied upload may select
// an identical registered archive, but its path is never returned for execution.
func (c *releaseCatalog) verifyArchive(id, version, upload string) (Release, string, error) {
	fail := func() (Release, string, error) {
		return Release{}, "", NewError(CodePackageInvalid, "official plugin release is invalid")
	}
	for _, release := range c.releases {
		if release.Manifest.ID != id || release.Manifest.Version != version {
			continue
		}
		path := filepath.Join(c.root, release.PackageFile)
		digest, err := archiveDigest(path)
		if err != nil || digest != release.ArchiveSHA256 {
			return fail()
		}
		if upload != "" {
			digest, err = archiveDigest(upload)
			if err != nil || digest != release.ArchiveSHA256 {
				return fail()
			}
		}
		return release, path, nil
	}
	return fail()
}

func archiveDigest(path string) (string, error) {
	if err := rejectSymlinkComponents(path); err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > MaxPackageBytes {
		return "", NewError(CodePackageInvalid, "plugin package is invalid")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(file, MaxPackageBytes+1))
	if err != nil || n > MaxPackageBytes {
		return "", NewError(CodePackageInvalid, "plugin package is invalid")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// extractVerified rechecks the complete archive and the pinned manifest after
// extraction, so a self-consistent archive cannot authorize its own executable.
func (c *releaseCatalog) extractVerified(id, version, upload, staging string) (Manifest, error) {
	release, path, err := c.verifyArchive(id, version, upload)
	if err != nil {
		return Manifest{}, err
	}
	entry, _ := c.entry(id)
	m, err := extractPackageContractForEntry(path, staging, release.Manifest.SchemaVersion, runtime.GOARCH, &entry)
	if err != nil {
		return Manifest{}, err
	}
	if m != release.Manifest {
		return Manifest{}, NewError(CodePackageInvalid, "official plugin release is invalid")
	}
	return m, nil
}

func (c *releaseCatalog) entry(id string) (CatalogEntry, bool) {
	for _, entry := range c.entries {
		if entry.ID == id {
			return entry, true
		}
	}
	return LookupOfficial(id)
}

func validReleaseManifest(m Manifest) bool {
	return validReleaseManifestForArch(m, runtime.GOARCH)
}

func validReleaseManifestForArch(m Manifest, arch string) bool {
	return validReleaseManifestForArchWithEntry(m, arch, nil)
}

func validReleaseManifestForArchWithEntry(m Manifest, arch string, expected *CatalogEntry) bool {
	// TargetID is deployment metadata attached from the catalog, not a package
	// manifest field. Strip it before reusing the package parser.
	m.TargetID = ""
	data, err := json.Marshal(m)
	if err != nil {
		return false
	}
	_, err = parseManifestForArchWithEntry(data, m.SchemaVersion, arch, expected)
	return err == nil
}

// InspectBuildRelease is used only by the build tool to validate image inputs.
// It is not a runtime registration API.
func InspectBuildRelease(path, purpose string) (Release, error) {
	return InspectBuildReleaseForArch(path, purpose, runtime.GOARCH)
}

// InspectBuildReleaseForArch validates cross-built inputs; runtime never uses this API.
func InspectBuildReleaseForArch(path, purpose, arch string) (Release, error) {
	if arch != "amd64" && arch != "arm64" {
		return Release{}, NewError(CodePackageInvalid, "unsupported build architecture")
	}
	digest, err := archiveDigest(path)
	if err != nil {
		return Release{}, err
	}
	stage, err := os.MkdirTemp("", "gopulse-release-build-")
	if err != nil {
		return Release{}, err
	}
	defer os.RemoveAll(stage)
	schema := 2
	if purpose == "legacy-v1" {
		schema = 1
	}
	manifest, err := extractPackageContractForArch(path, stage, schema, arch)
	if err != nil {
		return Release{}, err
	}
	r := Release{Manifest: manifest, ArchiveSHA256: digest, Purpose: purpose, PackageFile: filepath.Base(path)}
	if _, err = newReleaseCatalogForArch(filepath.Dir(path), []Release{r}, arch); err != nil {
		return Release{}, err
	}
	return r, nil
}

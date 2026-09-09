package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
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
}

// newReleaseCatalog is the runtime boundary for a compile-time generated list.
// In particular, it is not a loader for a writable on-volume catalog.
func newReleaseCatalog(root string, releases []Release) (*releaseCatalog, error) {
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
		if !validReleaseManifest(m) {
			return fail()
		}
	}
	return &releaseCatalog{root: filepath.Clean(root), releases: append([]Release(nil), releases...)}, nil
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
	m, err := extractPackageContract(path, staging, release.Manifest.SchemaVersion)
	if err != nil {
		return Manifest{}, err
	}
	if m != release.Manifest {
		return Manifest{}, NewError(CodePackageInvalid, "official plugin release is invalid")
	}
	return m, nil
}

func validReleaseManifest(m Manifest) bool {
	data, err := json.Marshal(m)
	if err != nil {
		return false
	}
	_, err = parseManifest(data, m.SchemaVersion)
	return err == nil
}

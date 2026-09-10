package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReleaseCatalogAuthenticatesImagePackage(t *testing.T) {
	m, schema := v2Manifest(t)
	binary := []byte("never-executed-catalog-fixture")
	sum := sha256.Sum256(binary)
	m.EntrypointSHA256 = hex.EncodeToString(sum[:])
	manifest, _ := json.Marshal(m)
	archive := writeArchive(t, map[string][]byte{"plugin.json": manifest, "config.schema.json": schema, m.Entrypoint: binary})
	raw, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	image := filepath.Join(root, "redis.tar.gz")
	if err := os.WriteFile(image, raw, 0600); err != nil {
		t.Fatal(err)
	}
	digest, err := archiveDigest(image)
	if err != nil {
		t.Fatal(err)
	}
	release := Release{Manifest: m, ArchiveSHA256: digest, Purpose: "current", PackageFile: "redis.tar.gz"}
	c, err := newReleaseCatalog(root, []Release{release})
	if err != nil {
		t.Fatal(err)
	}
	_, selected, err := c.verifyArchive(m.ID, m.Version, archive)
	if err != nil || selected != image {
		t.Fatal("upload was not resolved to image-owned package", err)
	}
	if _, err = c.extractVerified(m.ID, m.Version, archive, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	// A valid manifest and executable checksum do not confer official provenance.
	altered := append(append([]byte(nil), raw...), 0)
	if err = os.WriteFile(archive, altered, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = c.verifyArchive(m.ID, m.Version, archive); err == nil {
		t.Fatal("unregistered upload accepted")
	}
	if _, _, err = c.verifyArchive(m.ID, "99.0.0", ""); err == nil {
		t.Fatal("unregistered release accepted")
	}
	if _, err = newReleaseCatalog(root, []Release{release, release}); err == nil {
		t.Fatal("duplicate release accepted")
	}
	// Pinning the archive does not excuse a mismatching entrypoint/schema manifest.
	wrong := release
	wrong.Manifest.Name = "different pinned manifest"
	c, err = newReleaseCatalog(root, []Release{wrong})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.extractVerified(m.ID, m.Version, "", t.TempDir()); err == nil {
		t.Fatal("catalog manifest mismatch accepted")
	}
	if err = os.Remove(image); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(archive, image); err != nil {
		t.Fatal(err)
	}
	if _, _, err = c.verifyArchive(m.ID, m.Version, ""); err == nil {
		t.Fatal("image package symlink accepted")
	}
}

package packaging

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestBundleRoundTripIsDeterministicAndTamperProof protects the transport
// contract: the same inputs must produce byte-identical assets, and any
// modified byte must be rejected before a candidate is promoted.
func TestBundleRoundTripIsDeterministicAndTamperProof(t *testing.T) {
	repo := fixtureRepo(t)
	first, second := t.TempDir(), t.TempDir()
	if err := WriteBundle(repo, fixtureManifest("2.5.4"), productTopology(), first); err != nil {
		t.Fatalf("bundle assembly failed: %v", err)
	}
	if err := WriteBundle(repo, fixtureManifest("2.5.4"), productTopology(), second); err != nil {
		t.Fatalf("second bundle assembly failed: %v", err)
	}
	for _, name := range []string{
		"release-manifest.json", "checksums",
		"gopulse-2.5.4-bundle.tar.gz", "gopulse-2.5.4-bundle.tar.gz.sha256",
	} {
		left, err := os.ReadFile(filepath.Join(first, name))
		if err != nil {
			t.Fatalf("%s is missing: %v", name, err)
		}
		right, err := os.ReadFile(filepath.Join(second, name))
		if err != nil {
			t.Fatalf("%s is missing from the second bundle: %v", name, err)
		}
		if !bytes.Equal(left, right) {
			t.Errorf("%s is not reproducible for the same inputs", name)
		}
	}
	if _, err := VerifyBundle(filepath.Join(first, "release-manifest.json")); err != nil {
		t.Fatalf("a freshly written bundle must verify: %v", err)
	}
	if err := os.WriteFile(filepath.Join(first, "deploy/product/compose.yaml"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyBundle(filepath.Join(first, "release-manifest.json")); err == nil {
		t.Error("a bundle asset changed after assembly must be rejected")
	}
	archive := filepath.Join(second, "gopulse-2.5.4-bundle.tar.gz")
	content, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, append(content, 'x'), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyBundle(filepath.Join(second, "release-manifest.json")); err == nil {
		t.Error("an archive changed after assembly must be rejected")
	}
}

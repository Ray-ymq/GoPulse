package scenario

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Ray-ymq/GoPulse/acceptance/internal/harness"
	"github.com/Ray-ymq/GoPulse/acceptance/internal/receipt"
)

type candidateManifest struct {
	Version    string                    `json:"version"`
	Revision   string                    `json:"revision"`
	Images     map[string]candidateImage `json:"images"`
	ThirdParty map[string]candidateImage `json:"third_party"`
}

type candidateImage struct {
	Ref       string            `json:"ref"`
	Platforms map[string]string `json:"platforms"`
}

type candidateAsset struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type candidatePlugin struct {
	ID         string `json:"id"`
	Version    string `json:"version"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	Purpose    string `json:"purpose"`
	Archive    string `json:"archive_sha256"`
	Entrypoint string `json:"entrypoint_sha256"`
	Schema     string `json:"schema_sha256"`
}

type pinnedManifest struct {
	SchemaVersion           int                       `json:"schema_version"`
	Version                 string                    `json:"version"`
	Revision                string                    `json:"revision"`
	Compose                 candidateAsset            `json:"compose"`
	RuntimeContract         *candidateAsset           `json:"runtime_contract,omitempty"`
	RuntimeContractSchema   *candidateAsset           `json:"runtime_contract_schema,omitempty"`
	BundleSHA256            string                    `json:"bundle_sha256"`
	Images                  map[string]candidateImage `json:"images"`
	ThirdParty              map[string]candidateImage `json:"third_party"`
	Lifecycle               candidateImage            `json:"lifecycle"`
	Plugins                 []candidatePlugin         `json:"plugins"`
	SupportedUpgradeSources []string                  `json:"supported_upgrade_sources"`
}

var (
	pinnedDigest = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	pinnedRef    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]*@sha256:[0-9a-f]{64}$`)
	pinnedSemver = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	pinnedRev    = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// configureCandidate validates and exposes the immutable platform references
// through the same Compose environment variables used by the source stack.
// Pulling is explicit because the product Compose contract intentionally uses
// pull_policy=never for local development.
func configureCandidate(session *harness.Session, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read candidate manifest: %w", err)
	}
	var manifest candidateManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("parse candidate manifest: %w", err)
	}
	if manifest.Version != session.Version || manifest.Revision != session.Revision {
		return fmt.Errorf("candidate identity does not match VERSION and HEAD")
	}
	images := make(map[string]string, len(manifest.Images)+len(manifest.ThirdParty))
	for name, image := range manifest.Images {
		images[name] = platformReference(image)
	}
	for name, image := range manifest.ThirdParty {
		images[name] = platformReference(image)
	}
	for name, ref := range images {
		if ref == "" {
			return fmt.Errorf("candidate image %s has no linux/amd64 reference", name)
		}
		key := "GOPULSE_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_IMAGE"
		session.SetValue(key, ref)
		if result := session.Run("docker", "pull", ref); result.ExitCode != 0 {
			return fmt.Errorf("pull candidate image %s failed with exit code %d", name, result.ExitCode)
		}
	}
	return nil
}

// configurePinnedCandidate is the V1 boundary. It validates the complete
// release bundle before pulling any image and exposes only linux/amd64 digest
// references to Compose. It intentionally does not fall back to source images.
func configurePinnedCandidate(session *harness.Session, path string) (receipt.CandidateIdentity, error) {
	manifest, data, err := readPinnedManifest(path, session.Version, session.Revision)
	if err != nil {
		return receipt.CandidateIdentity{}, err
	}
	identity, err := pinnedCandidateIdentity(path, manifest, data)
	if err != nil {
		return receipt.CandidateIdentity{}, err
	}
	images := make(map[string]string, len(manifest.Images)+len(manifest.ThirdParty)+1)
	for name, image := range manifest.Images {
		images[name] = platformReference(image)
	}
	for name, image := range manifest.ThirdParty {
		images[name] = platformReference(image)
	}
	images["lifecycle"] = platformReference(manifest.Lifecycle)
	for name, ref := range images {
		if ref == "" {
			return receipt.CandidateIdentity{}, fmt.Errorf("candidate image %s has no linux/amd64 digest", name)
		}
		key := "GOPULSE_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_IMAGE"
		session.SetValue(key, ref)
		if result := session.Run("docker", "pull", ref); result.ExitCode != 0 {
			return receipt.CandidateIdentity{}, fmt.Errorf("pull candidate image %s failed with exit code %d", name, result.ExitCode)
		}
	}
	return identity, nil
}

func pinnedCandidateIdentity(path string, manifest pinnedManifest, data []byte) (receipt.CandidateIdentity, error) {
	root := filepath.Dir(path)
	if err := verifyBundleAssets(root, manifest); err != nil {
		return receipt.CandidateIdentity{}, err
	}
	bundlePath := filepath.Join(root, fmt.Sprintf("gopulse-%s-bundle.tar.gz", manifest.Version))
	bundleData, err := os.ReadFile(bundlePath)
	if err != nil {
		return receipt.CandidateIdentity{}, fmt.Errorf("read candidate Bundle: %w", err)
	}
	bundleHash := digestBytes(bundleData)
	detached, err := os.ReadFile(bundlePath + ".sha256")
	if err != nil || strings.TrimSpace(string(detached)) != bundleHash[7:]+"  "+filepath.Base(bundlePath) {
		return receipt.CandidateIdentity{}, errorsf("candidate Bundle detached checksum does not match")
	}
	configHash, err := hashConfiguredAssets(root, manifest)
	if err != nil {
		return receipt.CandidateIdentity{}, err
	}
	return receipt.CandidateIdentity{
		Version: manifest.Version, Revision: manifest.Revision,
		ManifestPath: path, ManifestSHA256: digestBytes(data),
		// CandidateIdentity records the transported Bundle archive digest;
		// manifest.BundleSHA256 remains the separate payload digest validated
		// by verifyBundleAssets above.
		BundlePath: bundlePath, BundleSHA256: bundleHash,
		SourceRevision: manifest.Revision, ContractSHA256: assetDigest(manifest.RuntimeContract),
		Tool: "gopulse-acceptance", ToolRevision: manifest.Revision, ConfigSHA256: configHash,
	}, nil
}

func readPinnedManifest(path, version, revision string) (pinnedManifest, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return pinnedManifest{}, nil, fmt.Errorf("read candidate manifest: %w", err)
	}
	if err := uniqueJSON(data); err != nil {
		return pinnedManifest{}, nil, fmt.Errorf("parse candidate manifest: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest pinnedManifest
	if err := decoder.Decode(&manifest); err != nil {
		return pinnedManifest{}, nil, fmt.Errorf("parse candidate manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return pinnedManifest{}, nil, errorsf("candidate manifest has trailing JSON")
	}
	if manifest.SchemaVersion != 1 || manifest.Version != version || manifest.Revision != revision || !pinnedSemver.MatchString(manifest.Version) || !pinnedRev.MatchString(manifest.Revision) || !pinnedDigest.MatchString(manifest.BundleSHA256) {
		return pinnedManifest{}, nil, errorsf("candidate identity does not match VERSION and HEAD")
	}
	if manifest.Compose.Path != "deploy/product/compose.yaml" || !pinnedDigest.MatchString(manifest.Compose.SHA256) || len(manifest.Images) != 9 || len(manifest.ThirdParty) != 6 || len(manifest.SupportedUpgradeSources) != 0 {
		return pinnedManifest{}, nil, errorsf("candidate manifest has an incomplete bundle contract")
	}
	products := []string{"backend", "business-worker", "search-indexer", "frontend", "admin-frontend", "router", "marshaller", "monitor", "redis-exporter"}
	for _, name := range products {
		if err := validatePinnedImage(manifest.Images[name]); err != nil {
			return pinnedManifest{}, nil, fmt.Errorf("candidate image %s: %w", name, err)
		}
	}
	for name := range manifest.Images {
		if !containsString(products, name) {
			return pinnedManifest{}, nil, fmt.Errorf("candidate image %s is not in the closed product set", name)
		}
	}
	thirdParty := []string{"redis", "mysql", "rabbitmq", "kafka", "elasticsearch", "victoriametrics"}
	for _, name := range thirdParty {
		if err := validatePinnedImage(manifest.ThirdParty[name]); err != nil {
			return pinnedManifest{}, nil, fmt.Errorf("candidate third-party image %s: %w", name, err)
		}
	}
	for name := range manifest.ThirdParty {
		if !containsString(thirdParty, name) {
			return pinnedManifest{}, nil, fmt.Errorf("candidate third-party image %s is not in the closed set", name)
		}
	}
	if err := validatePinnedImage(manifest.Lifecycle); err != nil {
		return pinnedManifest{}, nil, fmt.Errorf("candidate lifecycle image: %w", err)
	}
	if manifest.RuntimeContract == nil || manifest.RuntimeContractSchema == nil || manifest.RuntimeContract.Path != "deploy/runtime-contracts.json" || manifest.RuntimeContractSchema.Path != "deploy/runtime-contracts.schema.json" || !pinnedDigest.MatchString(manifest.RuntimeContract.SHA256) || !pinnedDigest.MatchString(manifest.RuntimeContractSchema.SHA256) {
		return pinnedManifest{}, nil, errorsf("candidate runtime contract assets are incomplete")
	}
	seenPlugins := map[string]bool{}
	for _, plugin := range manifest.Plugins {
		key := plugin.ID + "/" + plugin.Version + "/" + plugin.Arch + "/" + plugin.Purpose
		if seenPlugins[key] || plugin.ID == "" || plugin.OS != "linux" || (plugin.Arch != "amd64" && plugin.Arch != "arm64") || !pinnedDigest.MatchString(plugin.Archive) || !pinnedDigest.MatchString(plugin.Entrypoint) {
			return pinnedManifest{}, nil, errorsf("candidate plugin catalog is invalid")
		}
		seenPlugins[key] = true
		switch plugin.Purpose {
		case "current":
			if plugin.Version != manifest.Version || !pinnedDigest.MatchString(plugin.Schema) {
				return pinnedManifest{}, nil, errorsf("candidate current plugin identity is invalid")
			}
		case "upgrade-only":
			if plugin.ID != "redis-exporter" || plugin.Version != "1.9.4" || plugin.Arch != "amd64" || plugin.Schema != "" {
				return pinnedManifest{}, nil, errorsf("candidate legacy plugin identity is invalid")
			}
		default:
			return pinnedManifest{}, nil, errorsf("candidate plugin purpose is invalid")
		}
	}
	if len(manifest.Plugins) != len(manifest.Lifecycle.Platforms)*6+1 {
		return pinnedManifest{}, nil, errorsf("candidate plugin catalog is incomplete")
	}
	return manifest, data, nil
}

func uniqueJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := uniqueValue(decoder); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errorsf("candidate manifest has trailing JSON")
	}
	return nil
}

func uniqueValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return errorsf("candidate manifest contains duplicate JSON identity")
			}
			seen[name] = true
			if err := uniqueValue(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := uniqueValue(decoder); err != nil {
				return err
			}
		}
	default:
		return errorsf("candidate manifest has an invalid JSON delimiter")
	}
	_, err = decoder.Token()
	return err
}

func validatePinnedImage(image candidateImage) error {
	if !pinnedRef.MatchString(image.Ref) || len(image.Platforms) < 1 || len(image.Platforms) > 2 || !pinnedDigest.MatchString(image.Platforms["linux/amd64"]) {
		return errorsf("image reference or linux/amd64 digest is invalid")
	}
	for platform, digest := range image.Platforms {
		if platform != "linux/amd64" && platform != "linux/arm64" || !pinnedDigest.MatchString(digest) {
			return errorsf("image platform digest is invalid")
		}
	}
	if len(image.Platforms) == 2 && image.Platforms["linux/amd64"] == image.Platforms["linux/arm64"] {
		return errorsf("image platform digests must differ")
	}
	return nil
}

func verifyBundleAssets(root string, manifest pinnedManifest) error {
	assets := map[string]string{
		manifest.Compose.Path:                  manifest.Compose.SHA256,
		"deploy/runtime-contracts.json":        manifest.RuntimeContract.SHA256,
		"deploy/runtime-contracts.schema.json": manifest.RuntimeContractSchema.SHA256,
	}
	for path, expected := range assets {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil || digestBytes(data) != expected {
			return fmt.Errorf("candidate asset checksum mismatch: %s", path)
		}
	}
	for _, path := range []string{"README.md", "compose.yaml"} {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			return fmt.Errorf("candidate Bundle asset is missing: %s", path)
		}
	}
	if err := verifyBundleChecksums(root); err != nil {
		return err
	}
	files := map[string][]byte{}
	for path := range assets {
		data, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		files[path] = data
	}
	for _, path := range []string{"README.md", "compose.yaml"} {
		data, _ := os.ReadFile(filepath.Join(root, path))
		files[path] = data
	}
	if payloadDigest(files) != manifest.BundleSHA256 {
		return errorsf("candidate Bundle payload checksum mismatch")
	}
	return nil
}

func verifyBundleChecksums(root string) error {
	data, err := os.ReadFile(filepath.Join(root, "checksums"))
	if err != nil {
		return fmt.Errorf("candidate Bundle checksums are missing: %w", err)
	}
	checksums := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "  ", 2)
		if len(parts) != 2 || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(parts[0]) || parts[1] == "" {
			return errorsf("candidate Bundle checksums are malformed")
		}
		if _, exists := checksums[parts[1]]; exists {
			return errorsf("candidate Bundle checksums contain duplicates")
		}
		checksums[parts[1]] = parts[0]
	}
	expected := []string{"README.md", "compose.yaml", "deploy/product/compose.yaml", "deploy/runtime-contracts.json", "deploy/runtime-contracts.schema.json", "release-manifest.json"}
	for _, path := range expected {
		var content []byte
		if path == "release-manifest.json" {
			content, err = os.ReadFile(filepath.Join(root, "release-manifest.json"))
		} else {
			content, err = os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		}
		if err != nil || checksums[path] != digestBytes(content)[7:] {
			return fmt.Errorf("candidate Bundle checksum mismatch: %s", path)
		}
	}
	if len(checksums) != len(expected) {
		return errorsf("candidate Bundle checksums contain an unexpected asset")
	}
	return nil
}

func hashConfiguredAssets(root string, manifest pinnedManifest) (string, error) {
	var data bytes.Buffer
	for _, path := range []string{manifest.Compose.Path, manifest.RuntimeContract.Path, manifest.RuntimeContractSchema.Path} {
		value, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			return "", fmt.Errorf("read candidate configuration asset %s: %w", path, err)
		}
		data.WriteString(path)
		data.WriteByte('\x00')
		data.Write(value)
		data.WriteByte('\x00')
	}
	return digestBytes(data.Bytes()), nil
}

func payloadDigest(files map[string][]byte) string {
	keys := make([]string, 0, len(files))
	for key := range files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var canonical strings.Builder
	for _, key := range keys {
		canonical.WriteString(digestBytes(files[key])[7:])
		canonical.WriteString("  ")
		canonical.WriteString(key)
		canonical.WriteByte('\n')
	}
	return digestBytes([]byte(canonical.String()))
}

func digestBytes(data []byte) string {
	hash := sha256.Sum256(data)
	return "sha256:" + fmt.Sprintf("%x", hash[:])
}

func assetDigest(asset *candidateAsset) string {
	if asset == nil {
		return ""
	}
	return asset.SHA256
}

func errorsf(message string) error { return fmt.Errorf("%s", message) }

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func platformReference(image candidateImage) string {
	digest := image.Platforms["linux/amd64"]
	if image.Ref == "" || digest == "" {
		return ""
	}
	base := strings.SplitN(image.Ref, "@", 2)[0]
	return base + "@" + digest
}

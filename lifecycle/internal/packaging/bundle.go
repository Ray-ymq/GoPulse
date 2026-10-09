package packaging

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Ray-ymq/GoPulse/lifecycle/internal/release"
)

// payloadDigest hashes canonical payload entries, excluding the manifest and
// checksums, so the recorded bundle digest can never reference itself.
func payloadDigest(files map[string][]byte) string {
	var canonical strings.Builder
	for _, name := range sortedNames(files) {
		canonical.WriteString(sumHex(files[name]) + "  " + name + "\n")
	}
	return Sum([]byte(canonical.String()))
}

// atLeast reports whether a major.minor.patch version reached the given floor.
func atLeast(version string, major, minor, patch int) bool {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return false
	}
	values := make([]int, 3)
	for i, part := range parts {
		parsed, err := strconv.Atoi(part)
		if err != nil {
			return false
		}
		values[i] = parsed
	}
	for i, floor := range []int{major, minor, patch} {
		if values[i] != floor {
			return values[i] > floor
		}
	}
	return true
}

func checksumsContent(files map[string][]byte) []byte {
	var content strings.Builder
	for _, name := range sortedNames(files) {
		if name == "checksums" {
			continue
		}
		content.WriteString(sumHex(files[name]) + "  " + name + "\n")
	}
	return []byte(content.String())
}

func archiveName(version string) string { return "gopulse-" + version + "-bundle.tar.gz" }

// WriteBundle assembles the transport bundle from the resolved product
// topology and verifies what it just wrote. The manifest is published by the
// caller last so an interrupted build leaves no complete candidate.
func WriteBundle(repo *Repo, m *release.Manifest, product []byte, out string) error {
	tool, err := ToolCompose(m)
	if err != nil {
		return err
	}
	readme, err := repo.File("deploy/release/BUNDLE-README.md")
	if err != nil {
		return errors.New("bundle README is missing")
	}
	files := map[string][]byte{
		"deploy/product/compose.yaml": product,
		"compose.yaml":                tool,
		"README.md":                   readme,
	}
	if atLeast(m.Version, 1, 14, 3) {
		for _, asset := range []string{"deploy/runtime-contracts.json", "deploy/runtime-contracts.schema.json"} {
			data, err := repo.File(asset)
			if err != nil {
				return fmt.Errorf("runtime contract asset %s is missing", asset)
			}
			files[asset] = data
		}
		m.RuntimeContract = &release.Asset{Path: "deploy/runtime-contracts.json", SHA256: Sum(files["deploy/runtime-contracts.json"])}
		m.RuntimeContractSchema = &release.Asset{Path: "deploy/runtime-contracts.schema.json", SHA256: Sum(files["deploy/runtime-contracts.schema.json"])}
	}
	m.Compose = release.Asset{Path: "deploy/product/compose.yaml", SHA256: Sum(files["deploy/product/compose.yaml"])}
	m.BundleSHA256 = payloadDigest(files)
	if err := m.Validate(); err != nil {
		return fmt.Errorf("assembled manifest is invalid: %w", err)
	}
	manifest, err := encodeJSON(m)
	if err != nil {
		return err
	}
	files["release-manifest.json"] = manifest
	files["checksums"] = checksumsContent(files)
	for _, name := range sortedNames(files) {
		path := filepath.Join(out, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, files[name], 0o644); err != nil {
			return err
		}
	}
	name := archiveName(m.Version)
	if err := writeArchive(filepath.Join(out, name), files); err != nil {
		return err
	}
	archive, err := os.ReadFile(filepath.Join(out, name))
	if err != nil {
		return err
	}
	detached := sumHex(archive) + "  " + name + "\n"
	if err := os.WriteFile(filepath.Join(out, name+".sha256"), []byte(detached), 0o644); err != nil {
		return err
	}
	_, err = VerifyBundle(filepath.Join(out, "release-manifest.json"))
	return err
}

// writeArchive writes a deterministic USTAR archive: fixed member order,
// zero modification times and no owner identity.
func writeArchive(path string, files map[string][]byte) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	compressed, err := gzip.NewWriterLevel(file, gzip.BestCompression)
	if err != nil {
		return err
	}
	compressed.ModTime = time.Unix(0, 0)
	writer := tar.NewWriter(compressed)
	for _, name := range sortedNames(files) {
		data := files[name]
		header := &tar.Header{
			Name: name, Mode: 0o644, Size: int64(len(data)), ModTime: time.Unix(0, 0),
			Typeflag: tar.TypeReg, Format: tar.FormatUSTAR, Uid: 0, Gid: 0,
		}
		if err := writer.WriteHeader(header); err != nil {
			return err
		}
		if _, err := writer.Write(data); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return compressed.Close()
}

// VerifyBundle re-derives every recorded bundle digest from the files on disk
// and rejects anything outside the closed allowlist.
func VerifyBundle(path string) (*release.Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read the release manifest: %w", err)
	}
	m, err := release.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("invalid release manifest: %w", err)
	}
	root := filepath.Dir(path)
	names := []string{"deploy/product/compose.yaml", "README.md"}
	if _, err := os.Stat(filepath.Join(root, "compose.yaml")); err == nil {
		names = append(names, "compose.yaml")
	}
	for _, asset := range []*release.Asset{m.RuntimeContract, m.RuntimeContractSchema} {
		if asset == nil {
			continue
		}
		names = append(names, asset.Path)
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(asset.Path)))
		if err != nil || Sum(content) != asset.SHA256 {
			return nil, errors.New("runtime contract checksum mismatch")
		}
	}
	files := map[string][]byte{}
	for _, name := range names {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return nil, fmt.Errorf("bundle asset %s is missing", name)
		}
		files[name] = content
	}
	compose, ok := files[m.Compose.Path]
	if !ok || Sum(compose) != m.Compose.SHA256 || payloadDigest(files) != m.BundleSHA256 {
		return nil, errors.New("bundle asset checksum mismatch")
	}
	allowed := map[string]bool{"release-manifest.json": true, "checksums": true}
	for name := range files {
		allowed[name] = true
	}
	name := archiveName(m.Version)
	archive, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		return nil, fmt.Errorf("bundle archive %s is missing", name)
	}
	detached, err := os.ReadFile(filepath.Join(root, name+".sha256"))
	if err != nil || string(detached) != sumHex(archive)+"  "+name+"\n" {
		return nil, errors.New("archive checksum mismatch")
	}
	if err := checkArchive(archive, allowed, root); err != nil {
		return nil, err
	}
	// The recorded checksums cover every bundle file except checksums itself,
	// including the manifest that was just read back.
	onDisk := map[string][]byte{"release-manifest.json": data}
	for name, content := range files {
		onDisk[name] = content
	}
	checksums, err := os.ReadFile(filepath.Join(root, "checksums"))
	if err != nil || !bytes.Equal(checksums, checksumsContent(onDisk)) {
		return nil, errors.New("bundle checksums mismatch")
	}
	return m, nil
}

// checkArchive enforces the closed member allowlist and the byte equality
// between the archive and the extracted bundle.
func checkArchive(archive []byte, allowed map[string]bool, root string) error {
	compressed, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return errors.New("bundle archive is not gzip")
	}
	defer compressed.Close()
	reader := tar.NewReader(compressed)
	seen := map[string]bool{}
	for {
		header, err := reader.Next()
		if err != nil {
			break
		}
		if header.Typeflag != tar.TypeReg || header.Mode != 0o644 || header.Uid != 0 || header.Gid != 0 {
			return errors.New("unsafe bundle entry")
		}
		content, err := readAllChecked(reader)
		if err != nil {
			return errors.New("invalid bundle archive")
		}
		stored, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(header.Name)))
		if err != nil || !bytes.Equal(content, stored) || bytes.ContainsRune(content, '\r') || bytes.IndexByte(content, 0) >= 0 {
			return errors.New("bundle bytes or LF mismatch")
		}
		seen[header.Name] = true
	}
	if len(seen) != len(allowed) {
		return errors.New("bundle allowlist mismatch")
	}
	for name := range allowed {
		if !seen[name] {
			return errors.New("bundle allowlist mismatch")
		}
	}
	return nil
}

func readAllChecked(reader *tar.Reader) ([]byte, error) {
	var buffer bytes.Buffer
	if _, err := buffer.ReadFrom(reader); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

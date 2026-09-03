package plugin

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const MaxPackageBytes int64 = 64 << 20
const maxEntries = 32
const maxTotalBytes int64 = 128 << 20
const maxFileBytes int64 = 96 << 20

func extractPackage(archivePath, staging string) (Manifest, error) {
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
			if clean == "plugin.json" && h.Size > 64<<10 {
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
	if !seen["plugin.json"] || !seen["bin/gopulse-redis-exporter"] {
		return Manifest{}, NewError(CodePackageInvalid, "plugin package is invalid")
	}
	data, err := os.ReadFile(filepath.Join(staging, "plugin.json"))
	if err != nil {
		return Manifest{}, NewError(CodePackageInvalid, "plugin package is invalid")
	}
	manifest, err := ParseManifest(data)
	if err != nil {
		return Manifest{}, err
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

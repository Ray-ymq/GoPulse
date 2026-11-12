// Package digest computes the content-addressed identities the local
// lifecycles bind to: source trees, the private environment file, Compose
// files, and the application build inputs.
//
// The byte layout is the one the replaced Python helper used (big-endian
// length-prefixed relative path and content), so a digest recorded by either
// implementation is comparable and unchanged inputs keep reusing builds.
package digest

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// Paths digests files and directory trees relative to root. Only regular files
// (following symlinks) contribute; anything under a .git directory is ignored.
func Paths(root string, items []string) (string, error) {
	type entry struct {
		absolute string
		relative string
	}
	entries := map[string]entry{}
	add := func(absolute string) error {
		relative, err := filepath.Rel(root, absolute)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(relative)
		entries[key] = entry{absolute: absolute, relative: key}
		return nil
	}
	for _, item := range items {
		path := item
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, item)
		}
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		switch {
		case info.Mode().IsRegular():
			if err := add(path); err != nil {
				return "", err
			}
		case info.IsDir():
			walkErr := filepath.WalkDir(path, func(current string, dirEntry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if dirEntry.IsDir() {
					if dirEntry.Name() == ".git" && current != path {
						return fs.SkipDir
					}
					return nil
				}
				resolved, statErr := os.Stat(current)
				if statErr != nil || !resolved.Mode().IsRegular() {
					return nil
				}
				return add(current)
			})
			if walkErr != nil {
				return "", walkErr
			}
		}
	}

	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	hash := sha256.New()
	var length [8]byte
	for _, key := range keys {
		content, err := os.ReadFile(entries[key].absolute)
		if err != nil {
			return "", err
		}
		binary.BigEndian.PutUint64(length[:], uint64(len(key)))
		hash.Write(length[:])
		hash.Write([]byte(key))
		binary.BigEndian.PutUint64(length[:], uint64(len(content)))
		hash.Write(length[:])
		hash.Write(content)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// Source 计算本地业务程序源码的指纹。
func Source(root string, observe bool) (string, error) {
	items := []string{
		filepath.Join(root, "backend/business"),
		filepath.Join(root, "backend/runtimekit"),
	}

	return Paths(root, items)
}

// SourceBuilds digests the sources that produce the locally built binaries.
func SourceBuilds(root string) (string, error) { return Source(root, false) }

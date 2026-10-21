package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type nodeRelease struct {
	Version string            `json:"version"`
	SHA256  map[string]string `json:"sha256"`
}

func downloadNativeNode(module, arch, version, destination string) error {
	data, err := os.ReadFile(filepath.Join(module, "upstream.json"))
	if err != nil {
		return err
	}
	var release nodeRelease
	if decode(data, &release) != nil || !semver(release.Version) || release.Version != version || len(release.SHA256[arch]) != 64 {
		return errors.New("invalid native node release")
	}
	url := fmt.Sprintf("https://github.com/prometheus/node_exporter/releases/download/v%s/node_exporter-%s.linux-%s.tar.gz", version, version, arch)
	client := &http.Client{Timeout: 60 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return errors.New("native node download failed")
	}
	archive, err := io.ReadAll(io.LimitReader(response.Body, (64<<20)+1))
	if err != nil || len(archive) > 64<<20 {
		return errors.New("invalid native node archive")
	}
	binary, err := extractNativeNode(archive, release.SHA256[arch], version, arch)
	if err != nil {
		return err
	}
	return os.WriteFile(destination, binary, 0700)
}

func extractNativeNode(data []byte, digest, version, arch string) ([]byte, error) {
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != digest {
		return nil, errors.New("native node checksum mismatch")
	}
	zipped, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer func() { _ = zipped.Close() }()
	archive := tar.NewReader(io.LimitReader(zipped, 128<<20))
	wanted := fmt.Sprintf("node_exporter-%s.linux-%s/node_exporter", version, arch)
	var binary []byte
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.Name != wanted {
			continue
		}
		if binary != nil || header.Typeflag != tar.TypeReg || header.Size < 1 || header.Size > 64<<20 {
			return nil, errors.New("invalid native node executable")
		}
		binary, err = io.ReadAll(io.LimitReader(archive, (64<<20)+1))
		if err != nil {
			return nil, err
		}
	}
	if len(binary) < 4 || string(binary[:4]) != "\x7fELF" {
		return nil, errors.New("native node executable is missing")
	}
	return binary, nil
}

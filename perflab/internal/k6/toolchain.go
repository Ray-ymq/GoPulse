package k6

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type toolchain struct {
	Version  string                      `json:"version"`
	Release  string                      `json:"release"`
	Archives map[string]toolchainArchive `json:"archives"`
}

type toolchainArchive struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

func ResolveK6(root string, config K6Config) (string, error) {
	tool, err := loadToolchain(root)
	if err != nil {
		return "", err
	}
	if config.Binary != "" {
		path := ResolvePath(root, config.Binary)
		if err := validateK6Binary(path, tool.Version); err != nil {
			return "", err
		}
		return path, nil
	}
	if override := strings.TrimSpace(os.Getenv("K6_BIN")); override != "" {
		if err := validateK6Binary(override, tool.Version); err != nil {
			return "", err
		}
		return override, nil
	}
	directory := filepath.Join(root, ".run", "tools", "k6", tool.Version, runtime.GOOS+"-"+runtime.GOARCH)
	binary := filepath.Join(directory, "k6")
	if valid, err := binaryMatchesVersion(binary, tool.Version); err != nil {
		return "", err
	} else if valid {
		return binary, nil
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("K6_AUTO_INSTALL")), "0") {
		return "", fmt.Errorf("k6 %s is not installed at %s; set K6_BIN or enable K6_AUTO_INSTALL", tool.Version, binary)
	}
	archive, ok := tool.Archives[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		return "", fmt.Errorf("k6 %s has no pinned archive for %s/%s", tool.Version, runtime.GOOS, runtime.GOARCH)
	}
	if err := installK6(directory, tool.Release+"/"+archive.Name, archive); err != nil {
		return "", err
	}
	if err := validateK6Binary(binary, tool.Version); err != nil {
		return "", err
	}
	return binary, nil
}

func loadToolchain(root string) (toolchain, error) {
	encoded, err := os.ReadFile(filepath.Join(root, "perflab", "k6", "toolchain.json"))
	if err != nil {
		return toolchain{}, fmt.Errorf("read k6 toolchain: %w", err)
	}
	var tool toolchain
	if err := json.Unmarshal(encoded, &tool); err != nil {
		return toolchain{}, fmt.Errorf("decode k6 toolchain: %w", err)
	}
	if tool.Version != K6Version || tool.Release == "" {
		return toolchain{}, errors.New("k6 toolchain version is not pinned to the supported release")
	}
	return tool, nil
}

func installK6(directory, url string, archive toolchainArchive) error {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	archivePath := filepath.Join(directory, archive.Name)
	if matches, err := fileMatchesSHA256(archivePath, archive.SHA256); err != nil {
		return err
	} else if !matches {
		if err := download(url, archivePath, archive.SHA256); err != nil {
			return err
		}
	}
	if strings.HasSuffix(archive.Name, ".zip") {
		return extractZip(archivePath, directory)
	}
	return extractTarGzip(archivePath, directory)
}

func download(url, path, expectedSHA string) error {
	response, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("download k6 %s: %w", url, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download k6 %s returned HTTP %d", url, response.StatusCode)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".k6-download-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(temporary, hash), response.Body); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if actual := hex.EncodeToString(hash.Sum(nil)); !strings.EqualFold(actual, expectedSHA) {
		return fmt.Errorf("downloaded k6 archive has SHA256 %s, want %s", actual, expectedSHA)
	}
	return os.Rename(temporaryPath, path)
}

func fileMatchesSHA256(path, expected string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return false, err
	}
	return strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), expected), nil
}

func binaryMatchesVersion(path, version string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if info.IsDir() {
		return false, fmt.Errorf("k6 path is a directory: %s", path)
	}
	output, err := exec.Command(path, "version").CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("validate k6 %s: %w", path, err)
	}
	return strings.Contains(string(output), version), nil
}

func validateK6Binary(path, version string) error {
	valid, err := binaryMatchesVersion(path, version)
	if err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("k6 binary %s does not report version %s", path, version)
	}
	return nil
}

func extractTarGzip(path, directory string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer func() { _ = gzipReader.Close() }()
	reader := tar.NewReader(gzipReader)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg || filepath.Base(header.Name) != "k6" || strings.Contains(filepath.Clean(header.Name), "..") {
			continue
		}
		binary := filepath.Join(directory, "k6")
		if err := writeExtracted(binary, reader, 0o700); err != nil {
			return err
		}
		return nil
	}
	return errors.New("k6 archive did not contain a k6 binary")
}

func extractZip(path, directory string) error {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer func() { _ = archive.Close() }()
	for _, item := range archive.File {
		if filepath.Base(item.Name) != "k6" || strings.Contains(filepath.Clean(item.Name), "..") {
			continue
		}
		reader, err := item.Open()
		if err != nil {
			return err
		}
		errorValue := writeExtracted(filepath.Join(directory, "k6"), reader, 0o700)
		_ = reader.Close()
		return errorValue
	}
	return errors.New("k6 archive did not contain a k6 binary")
}

func writeExtracted(path string, reader io.Reader, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".k6-extract-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if _, err := io.Copy(temporary, reader); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

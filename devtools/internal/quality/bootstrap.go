package quality

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Ray-ymq/GoPulse/devtools/internal/frontenddeps"
)

// Bootstrap prepares the pinned lint tool and, unless toolsOnly is set, the
// requested frontend dependencies.
func Bootstrap(root, application string, toolsOnly bool) error {
	if toolsOnly && application != "" {
		return fmt.Errorf("TOOLS_ONLY=1 cannot be combined with MODULE=%s", application)
	}
	if application != "" && application != "frontend" && application != "admin-frontend" {
		return fmt.Errorf("MODULE must be empty, frontend, or admin-frontend (got %s)", application)
	}
	config, err := loadToolchains(root)
	if err != nil {
		return err
	}
	if _, err := ensureLintTool(root, config); err != nil {
		return err
	}
	if toolsOnly {
		fmt.Println("[bootstrap] Go lint tool is ready")
		return nil
	}
	applications := []string{application}
	if application == "" {
		applications = []string{"frontend", "admin-frontend"}
	}
	for _, name := range applications {
		if err := frontenddeps.Ensure(root, name); err != nil {
			return err
		}
	}
	fmt.Printf("[bootstrap] prepared %s\n", strings.Join(applications, ", "))
	return nil
}

func ensureLintTool(root string, config toolchains) (string, error) {
	platform := runtime.GOOS + "/" + runtime.GOARCH
	archive, ok := config.GolangCILint.Archives[platform]
	if !ok || archive.Name == "" || archive.SHA256 == "" {
		return "", fmt.Errorf("golangci-lint %s has no pinned archive for %s", config.GolangCILint.Version, platform)
	}
	directory := filepath.Join(root, ".run", "tools", "golangci-lint", config.GolangCILint.Version, runtime.GOOS+"-"+runtime.GOARCH)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	archivePath := filepath.Join(directory, archive.Name)
	if info, err := os.Stat(archivePath); err == nil && !info.IsDir() {
		valid, digestErr := fileMatchesSHA256(archivePath, archive.SHA256)
		if digestErr != nil {
			return "", digestErr
		}
		if !valid {
			return "", fmt.Errorf("pinned golangci-lint archive has an unexpected SHA256: %s", archivePath)
		}
	} else if err := downloadFile(config.GolangCILint.Release+"/"+archive.Name, archivePath, archive.SHA256); err != nil {
		return "", err
	}
	binary := filepath.Join(directory, "golangci-lint")
	if valid, err := binaryMatchesVersion(binary, config.GolangCILint.Version); err != nil {
		return "", err
	} else if !valid {
		if err := extractLintBinary(archivePath, directory); err != nil {
			return "", err
		}
	}
	if valid, err := binaryMatchesVersion(binary, config.GolangCILint.Version); err != nil {
		return "", err
	} else if !valid {
		return "", fmt.Errorf("prepared golangci-lint does not report version %s", config.GolangCILint.Version)
	}
	return binary, nil
}

func downloadFile(url, path, expectedSHA string) error {
	response, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s returned HTTP %d", url, response.StatusCode)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".download-*")
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
	actual := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(actual, expectedSHA) {
		return fmt.Errorf("downloaded %s has SHA256 %s, want %s", url, actual, expectedSHA)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func fileMatchesSHA256(path, expected string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
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
	if info, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	} else if info.IsDir() {
		return false, fmt.Errorf("lint tool path is a directory: %s", path)
	}
	output, err := exec.Command(path, "--version").CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("validate %s: %w", path, err)
	}
	return strings.Contains(string(output), version), nil
}

func extractLintBinary(archivePath, directory string) error {
	archive, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = archive.Close() }()
	gzipReader, err := gzip.NewReader(archive)
	if err != nil {
		return err
	}
	defer func() { _ = gzipReader.Close() }()
	tarReader := tar.NewReader(gzipReader)
	found := false
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg || filepath.Base(header.Name) != "golangci-lint" || strings.Contains(filepath.Clean(header.Name), "..") {
			continue
		}
		temporary, err := os.CreateTemp(directory, ".golangci-lint-*")
		if err != nil {
			return err
		}
		temporaryPath := temporary.Name()
		if _, err := io.Copy(temporary, tarReader); err != nil {
			_ = temporary.Close()
			_ = os.Remove(temporaryPath)
			return err
		}
		if err := temporary.Chmod(0o700); err != nil {
			_ = temporary.Close()
			_ = os.Remove(temporaryPath)
			return err
		}
		if err := temporary.Close(); err != nil {
			_ = os.Remove(temporaryPath)
			return err
		}
		if err := os.Rename(temporaryPath, filepath.Join(directory, "golangci-lint")); err != nil {
			_ = os.Remove(temporaryPath)
			return err
		}
		found = true
		break
	}
	if !found {
		return fmt.Errorf("golangci-lint archive does not contain its binary")
	}
	return nil
}

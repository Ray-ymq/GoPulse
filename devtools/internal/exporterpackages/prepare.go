// Package exporterpackages prepares the independent Exporter inputs used by
// local observation lifecycles. The Monitor image never builds or contains
// these archives; Compose mounts this directory as a read-only deployment
// input.
package exporterpackages

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Ray-ymq/GoPulse/devtools/internal/digest"
	"github.com/Ray-ymq/GoPulse/devtools/internal/proc"
)

var sources = []string{"redis", "mysql", "rabbitmq", "kafka", "elasticsearch", "victoriametrics"}

type receipt struct {
	Input string `json:"input"`
}

// Prepare builds only missing or stale module packages and refreshes the
// catalog after every successful package check. The output is safe to mount
// into the read-only Monitor container.
func Prepare(root, output string, environment map[string]string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	output, err = filepath.Abs(output)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(output, 0o755); err != nil {
		return err
	}
	arch := runtime.GOARCH
	if arch != "amd64" && arch != "arm64" {
		return fmt.Errorf("unsupported local architecture: %s", arch)
	}
	for _, source := range sources {
		versionBytes, versionErr := os.ReadFile(filepath.Join(root, "exporters", source, "VERSION"))
		if versionErr != nil {
			return versionErr
		}
		version := strings.TrimSpace(string(versionBytes))
		if version == "" {
			return fmt.Errorf("empty %s Exporter version", source)
		}
		input, digestErr := digest.Paths(root, []string{
			filepath.Join(root, "componentmetrics"),
			filepath.Join(root, "exporters", source),
		})
		if digestErr != nil {
			return digestErr
		}
		archive := filepath.Join(output, fmt.Sprintf("gopulse-%s-exporter-%s-linux-%s.tar.gz", source, version, arch))
		receiptPath := archive + ".input.json"
		if cached, readErr := readReceipt(receiptPath); readErr == nil && cached.Input == input {
			if info, statErr := os.Stat(archive); statErr == nil && info.Mode().IsRegular() && info.Size() > 0 {
				continue
			}
		}
		overrides := cloneEnvironment(environment)
		if work := filepath.Join(root, ".run", "ci", "go.work"); fileExists(work) {
			overrides["GOWORK"] = work
		}
		command := []string{"go", "run", "./cmd/exporter-package", "--repo-root", root, "--source", source, "--version", version, "--arch", arch, "--output", archive}
		if _, runErr := proc.RunCaptured(command, filepath.Join(root, "devtools"), overrides, "build "+source+" Exporter package", 0); runErr != nil {
			return runErr
		}
		if err = writeReceipt(receiptPath, receipt{Input: input}); err != nil {
			return err
		}
	}
	arguments := []string{"go", "run", "./cmd/exporter-catalog", "--packages-root", output, "--output", filepath.Join(output, "catalog.json"), "--arch", arch}
	for _, source := range sources {
		versionBytes, versionErr := os.ReadFile(filepath.Join(root, "exporters", source, "VERSION"))
		if versionErr != nil {
			return versionErr
		}
		version := strings.TrimSpace(string(versionBytes))
		archive := filepath.Join(output, fmt.Sprintf("gopulse-%s-exporter-%s-linux-%s.tar.gz", source, version, arch))
		arguments = append(arguments, "--package", "current="+archive)
	}
	overrides := cloneEnvironment(environment)
	if work := filepath.Join(root, ".run", "ci", "go.work"); fileExists(work) {
		overrides["GOWORK"] = work
	}
	_, err = proc.RunCaptured(arguments, filepath.Join(root, "devtools"), overrides, "build Exporter catalog", 0)
	return err
}

func cloneEnvironment(source map[string]string) map[string]string {
	clone := make(map[string]string, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func readReceipt(path string) (receipt, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return receipt{}, err
	}
	var result receipt
	if err = json.Unmarshal(data, &result); err != nil || result.Input == "" {
		return receipt{}, fmt.Errorf("invalid package receipt")
	}
	return result, nil
}

func writeReceipt(path string, value receipt) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err = os.WriteFile(temporary, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

// Package exporterpackages 为源码开发和产品镜像准备独立插件包及只读目录。
package exporterpackages

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Ray-ymq/GoPulse/devtools/internal/digest"
	"github.com/Ray-ymq/GoPulse/devtools/internal/proc"
)

var sources = []string{"redis", "mysql", "rabbitmq", "kafka", "elasticsearch", "victoriametrics", "node"}

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
		versionBytes, versionErr := os.ReadFile(filepath.Join(root, "backend/observability/exporters", source, "VERSION"))
		if versionErr != nil {
			return versionErr
		}
		version := strings.TrimSpace(string(versionBytes))
		if version == "" {
			return fmt.Errorf("empty %s Exporter version", source)
		}
		input, digestErr := digest.Paths(root, []string{
			filepath.Join(root, "backend/observability/exporters", source),
			filepath.Join(root, "tools/devtools/cmd/exporter-package"),
			filepath.Join(root, "backend/observability/monitor/internal/plugin"),
		})
		if digestErr != nil {
			return digestErr
		}
		input = packageInput(input)
		archive := filepath.Join(output, fmt.Sprintf("gopulse-%s-exporter-%s-linux-%s.tar.gz", source, version, arch))
		receiptPath := archive + ".input.json"
		if cached, readErr := readReceipt(receiptPath); readErr == nil && cached.Input == input {
			if info, statErr := os.Stat(archive); statErr == nil && info.Mode().IsRegular() && info.Size() > 0 {
				continue
			}
		}
		overrides := cloneEnvironment(environment)
		overrides["GOWORK"] = "off"
		command := []string{"go", "run", "./cmd/exporter-package", "--repo-root", root, "--source", source, "--version", version, "--arch", arch, "--output", archive}
		if source == "node" && environment["GOPULSE_NODE_EXPORTER_BINARY"] != "" {
			command = append(command, "--binary", environment["GOPULSE_NODE_EXPORTER_BINARY"])
		}
		if _, runErr := proc.RunCaptured(command, filepath.Join(root, "tools/devtools"), overrides, "build "+source+" Exporter package", 0); runErr != nil {
			return runErr
		}
		if err = writeReceipt(receiptPath, receipt{Input: input}); err != nil {
			return err
		}
	}
	arguments := []string{"go", "run", "./cmd/exporter-catalog", "--packages-root", output, "--output", filepath.Join(output, "catalog.json"), "--arch", arch}
	for _, source := range sources {
		versionBytes, versionErr := os.ReadFile(filepath.Join(root, "backend/observability/exporters", source, "VERSION"))
		if versionErr != nil {
			return versionErr
		}
		version := strings.TrimSpace(string(versionBytes))
		archive := filepath.Join(output, fmt.Sprintf("gopulse-%s-exporter-%s-linux-%s.tar.gz", source, version, arch))
		arguments = append(arguments, "--package", "current="+archive)
	}
	overrides := cloneEnvironment(environment)
	overrides["GOWORK"] = "off"
	_, err = proc.RunCaptured(arguments, filepath.Join(root, "tools/devtools"), overrides, "build Exporter catalog", 0)
	return err
}

// packageInput 将独立模块解析和工具链纳入缓存身份，避免复用工作区依赖构建的包。
func packageInput(source string) string {
	sum := sha256.Sum256([]byte("GOWORK=off\n" + runtime.Version() + "\n" + source))
	return fmt.Sprintf("%x", sum)
}

func cloneEnvironment(source map[string]string) map[string]string {
	clone := make(map[string]string, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
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

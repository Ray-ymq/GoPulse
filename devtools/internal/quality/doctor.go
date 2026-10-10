package quality

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

type toolchains struct {
	Node struct {
		Major   int    `json:"major"`
		Minimum string `json:"minimum"`
	} `json:"node"`
	NPM struct {
		Major   int    `json:"major"`
		Minimum string `json:"minimum"`
	} `json:"npm"`
	Python struct {
		Minimum string `json:"minimum"`
	} `json:"python"`
	GolangCILint struct {
		Version  string `json:"version"`
		Release  string `json:"release"`
		Archives map[string]struct {
			Name   string `json:"name"`
			SHA256 string `json:"sha256"`
		} `json:"archives"`
	} `json:"golangci_lint"`
	Playwright struct {
		Browser string `json:"browser"`
	} `json:"playwright"`
}

var versionPattern = regexp.MustCompile(`([0-9]+)(?:\.([0-9]+))?(?:\.([0-9]+))?`)

// Doctor checks host prerequisites without starting product services.
func Doctor(root string) error {
	config, err := loadToolchains(root)
	if err != nil {
		return err
	}
	issues := []string{}
	printCheck := func(name, value string, ok bool, next string) {
		status := "ok"
		if !ok {
			status = "missing"
			issues = append(issues, name+": "+value+"; "+next)
		}
		fmt.Printf("[doctor] %-18s %-8s %s\n", name, status, value)
	}

	goVersion, goErr := commandOutput("go", "version")
	goMinimum, minimumErr := minimumGoVersion(root)
	goOK := goErr == nil && minimumErr == nil && atLeast(parseVersion(goVersion), goMinimum)
	goValue := strings.TrimSpace(goVersion)
	if goErr != nil {
		goValue = goErr.Error()
	} else if minimumErr != nil {
		goValue += "; " + minimumErr.Error()
	} else if !goOK {
		goValue += fmt.Sprintf("; requires Go >= %s", formatVersion(goMinimum))
	}
	printCheck("go", goValue, goOK, "install a Go version accepted by the repository go.mod files")

	nodeVersion, nodeErr := commandOutput("node", "--version")
	nodeOK := nodeErr == nil && atLeast(parseVersion(nodeVersion), parseVersion(config.Node.Minimum)) && parseVersion(nodeVersion)[0] == config.Node.Major
	printCheck("node", strings.TrimSpace(nodeVersion), nodeOK, "install the pinned Node major/minimum version")

	npmVersion, npmErr := commandOutput("npm", "--version")
	npmOK := npmErr == nil && atLeast(parseVersion(npmVersion), parseVersion(config.NPM.Minimum)) && parseVersion(npmVersion)[0] == config.NPM.Major
	printCheck("npm", strings.TrimSpace(npmVersion), npmOK, "install the pinned npm major/minimum version")

	pythonVersion, pythonErr := commandOutput("python3", "--version")
	pythonOK := pythonErr == nil && atLeast(parseVersion(pythonVersion), parseVersion(config.Python.Minimum))
	printCheck("python3", strings.TrimSpace(pythonVersion), pythonOK, "install Python 3.12 or newer for governance checks")

	dockerVersion, dockerErr := commandOutput("docker", "--version")
	printCheck("docker daemon", strings.TrimSpace(dockerVersion), dockerErr == nil && commandSucceeds(root, "docker", "info"), "start Docker Desktop or the Docker daemon")
	composeVersion, composeErr := commandOutput("docker", "compose", "version")
	printCheck("docker compose", strings.TrimSpace(composeVersion), composeErr == nil && commandSucceeds(root, "docker", "compose", "version"), "install Docker Compose v2 or newer")
	buildxVersion, buildxErr := commandOutput("docker", "buildx", "version")
	buildxOK := buildxErr == nil && commandSucceeds(root, "docker", "buildx", "inspect")
	printCheck("docker buildx", strings.TrimSpace(buildxVersion), buildxOK, "enable Docker buildx and its default builder")

	lintPath := filepath.Join(root, ".run", "tools", "golangci-lint", config.GolangCILint.Version, runtime.GOOS+"-"+runtime.GOARCH, "golangci-lint")
	lintVersion, lintErr := commandOutput(lintPath, "--version")
	lintOK := lintErr == nil && strings.Contains(lintVersion, config.GolangCILint.Version)
	printCheck("golangci-lint", strings.TrimSpace(lintVersion), lintOK, "run make bootstrap or make bootstrap TOOLS_ONLY=1")

	if len(issues) > 0 {
		fmt.Println("[doctor] next steps: resolve the listed prerequisites, then run make doctor again")
		return fmt.Errorf("doctor found %d required prerequisite(s)", len(issues))
	}
	fmt.Println("[doctor] all required prerequisites are available")
	return nil
}

func loadToolchains(root string) (toolchains, error) {
	raw, err := os.ReadFile(filepath.Join(root, "devtools", "toolchains.json"))
	if err != nil {
		return toolchains{}, fmt.Errorf("read toolchains.json: %w", err)
	}
	var config toolchains
	if err := json.Unmarshal(raw, &config); err != nil {
		return toolchains{}, fmt.Errorf("decode toolchains.json: %w", err)
	}
	return config, nil
}

func commandOutput(command string, args ...string) (string, error) {
	if _, err := exec.LookPath(command); err != nil {
		return "", err
	}
	process := exec.Command(command, args...)
	raw, err := process.CombinedOutput()
	if err != nil {
		return strings.TrimSpace(string(raw)), err
	}
	return string(raw), nil
}

func commandSucceeds(root, command string, args ...string) bool {
	process := exec.Command(command, args...)
	process.Dir = root
	process.Stdout = nil
	process.Stderr = nil
	return process.Run() == nil
}

func minimumGoVersion(root string) ([3]int, error) {
	minimum := [3]int{}
	found := false
	var walk func(string) error
	walk = func(directory string) error {
		entries, err := os.ReadDir(directory)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				if entry.Name() == ".git" || entry.Name() == ".run" || entry.Name() == "node_modules" || entry.Name() == "dist" {
					continue
				}
				if err := walk(filepath.Join(directory, entry.Name())); err != nil {
					return err
				}
				continue
			}
			if entry.Name() != "go.mod" {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(directory, entry.Name()))
			if err != nil {
				return err
			}
			for _, line := range strings.Split(string(raw), "\n") {
				fields := strings.Fields(line)
				if len(fields) != 2 || fields[0] != "go" {
					continue
				}
				version := parseVersion(fields[1])
				if !found || greater(version, minimum) {
					minimum, found = version, true
				}
			}
		}
		return nil
	}
	if err := walk(root); err != nil {
		return [3]int{}, err
	}
	if !found {
		return [3]int{}, fmt.Errorf("no go.mod files found")
	}
	return minimum, nil
}

func parseVersion(value string) [3]int {
	match := versionPattern.FindString(value)
	if match == "" {
		return [3]int{}
	}
	parts := strings.Split(match, ".")
	version := [3]int{}
	for index, part := range parts {
		version[index], _ = strconv.Atoi(part)
	}
	return version
}

func atLeast(actual, required [3]int) bool { return !less(actual, required) }

func greater(left, right [3]int) bool { return less(right, left) }

func less(left, right [3]int) bool {
	for index := range left {
		if left[index] != right[index] {
			return left[index] < right[index]
		}
	}
	return false
}

func formatVersion(version [3]int) string {
	return fmt.Sprintf("%d.%d.%d", version[0], version[1], version[2])
}

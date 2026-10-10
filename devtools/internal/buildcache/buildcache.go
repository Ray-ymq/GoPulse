// Package buildcache renders and runs the repository's Compose image build
// definition. It keeps the local and GitHub Actions build entry points on the
// same target list and preserves the optional-cache retry contract.
package buildcache

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const Platform = "linux/amd64"

// Targets is the complete image set used by the authoritative Compose build.
var Targets = []string{
	"backend",
	"business-worker",
	"search-indexer",
	"admin-frontend",
	"frontend",
	"acceptance",
	"router",
	"marshaller",
	"monitor",
	"redis-exporter",
}

// Options controls one build-cache invocation.
type Options struct {
	PrintOnly bool
	NoCache   bool
}

type commandResult struct {
	Code   int
	Stdout string
	Stderr string
}

type commandRunner func(name string, args []string, dir string, environment []string) commandResult

type targetDefinition map[string]json.RawMessage

type composeDefinition struct {
	Target map[string]targetDefinition `json:"target"`
}

type bakeDefinition struct {
	Group  map[string]map[string][]string `json:"group"`
	Target map[string]targetDefinition    `json:"target"`
}

var cacheFailurePattern = regexp.MustCompile(`(?i)(?:configure gha cache|import cache|cache (?:manifest|importer|exporter)|(?:actions|github).*cache)`)

// Run executes the build contract and returns the command exit code. Errors
// returned separately are configuration or process-launch failures and should
// be rendered by the CLI as a bounded setup error.
func Run(root string, options Options) (int, error) {
	return runWith(root, options, runCommand)
}

func runWith(root string, options Options, runner commandRunner) (int, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return 1, err
	}
	version, err := readVersion(filepath.Join(root, "VERSION"))
	if err != nil {
		return 1, err
	}
	revisionResult := runner("git", []string{"rev-parse", "HEAD"}, root, os.Environ())
	if revisionResult.Code != 0 {
		return 1, fmt.Errorf("git rev-parse HEAD failed with exit code %d", revisionResult.Code)
	}
	revision := strings.TrimSpace(revisionResult.Stdout)
	if revision == "" {
		return 1, errors.New("git rev-parse HEAD returned an empty revision")
	}

	environment := environmentWithIdentity(version, revision, os.Environ())
	cached := !options.NoCache && hasCacheRuntime(environment)
	definition, err := composeDefinitionFor(root, environment, cached, runner)
	if err != nil {
		return 1, err
	}
	if options.PrintOnly {
		encoded, err := json.MarshalIndent(definition, "", "  ")
		if err != nil {
			return 1, err
		}
		fmt.Println(string(encoded))
		return 0, nil
	}

	if !cached {
		fmt.Println("Compose build: remote cache unavailable; building normally.")
	}
	path, err := writeBakeFile(definition)
	if err != nil {
		return 1, err
	}
	defer os.Remove(path)

	result := runner("docker", []string{"buildx", "bake", "--file", path, "--load"}, root, environment)
	printResult(result)
	if result.Code == 0 || !cached || !isCacheFailure(result.Stdout+"\n"+result.Stderr) {
		return result.Code, nil
	}

	fallback := removeCacheFields(definition)
	if err := overwriteBakeFile(path, fallback); err != nil {
		return 1, err
	}
	fmt.Println("Compose build: cache service failed; retrying once without remote cache.")
	result = runner("docker", []string{"buildx", "bake", "--file", path, "--load"}, root, environment)
	printResult(result)
	return result.Code, nil
}

func readVersion(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	version := strings.TrimSpace(string(raw))
	parts := strings.Split(version, ".")
	if len(parts) != 3 || anyEmpty(parts) || !allDecimal(parts) {
		return "", fmt.Errorf("VERSION must use major.minor.patch")
	}
	return version, nil
}

func anyEmpty(parts []string) bool {
	for _, part := range parts {
		if part == "" {
			return true
		}
	}
	return false
}

func allDecimal(parts []string) bool {
	for _, part := range parts {
		for _, character := range part {
			if character < '0' || character > '9' {
				return false
			}
		}
	}
	return true
}

func environmentWithIdentity(version, revision string, base []string) []string {
	values := map[string]string{}
	for _, entry := range base {
		key, value, found := strings.Cut(entry, "=")
		if found {
			values[key] = value
		}
	}
	parts := strings.Split(version, ".")
	patch := 0
	for _, character := range parts[2] {
		patch = patch*10 + int(character-'0')
	}
	values["GOPULSE_VERSION"] = version
	values["GOPULSE_REVISION"] = revision
	values["GOPULSE_IMAGE_TAG"] = version
	values["GOPULSE_UPDATE_VERSION"] = fmt.Sprintf("%s.%s.%d", parts[0], parts[1], patch+1)
	return sortedEnvironment(values)
}

func hasCacheRuntime(environment []string) bool {
	values := map[string]string{}
	for _, entry := range environment {
		key, value, found := strings.Cut(entry, "=")
		if found {
			values[key] = value
		}
	}
	return values["ACTIONS_RUNTIME_TOKEN"] != "" && values["ACTIONS_RESULTS_URL"] != ""
}

func sortedEnvironment(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	environment := make([]string, 0, len(keys))
	for _, key := range keys {
		environment = append(environment, key+"="+values[key])
	}
	return environment
}

func composeDefinitionFor(root string, environment []string, cached bool, runner commandRunner) (bakeDefinition, error) {
	args := []string{"compose", "--env-file", filepath.Join(root, ".env.example"), "--file", filepath.Join(root, "deploy", "compose.yaml"), "build", "--print"}
	args = append(args, Targets...)
	result := runner("docker", args, root, environment)
	if result.Code != 0 {
		return bakeDefinition{}, fmt.Errorf("docker compose build --print failed with exit code %d: %s", result.Code, strings.TrimSpace(result.Stderr))
	}
	var compose composeDefinition
	if err := json.Unmarshal([]byte(result.Stdout), &compose); err != nil {
		return bakeDefinition{}, fmt.Errorf("decode Compose build definition: %w", err)
	}
	definition := bakeDefinition{
		Group:  map[string]map[string][]string{"default": {"targets": append([]string(nil), Targets...)}},
		Target: map[string]targetDefinition{},
	}
	for _, name := range Targets {
		target, ok := compose.Target[name]
		if !ok {
			return bakeDefinition{}, fmt.Errorf("Compose build definition has no target %q", name)
		}
		adapted := cloneTarget(target)
		adapted["platforms"] = rawJSON([]string{Platform})
		adapted["output"] = rawJSON([]string{"type=docker"})
		delete(adapted, "cache-from")
		delete(adapted, "cache-to")
		if cached {
			scope := "gopulse-" + name + "-linux-amd64-v1"
			adapted["cache-from"] = rawJSON([]string{"type=gha,scope=" + scope + ",version=2,timeout=2m"})
			adapted["cache-to"] = rawJSON([]string{"type=gha,scope=" + scope + ",version=2,mode=max,timeout=2m,ignore-error=true"})
		}
		definition.Target[name] = adapted
	}
	return definition, nil
}

func cloneTarget(target targetDefinition) targetDefinition {
	clone := make(targetDefinition, len(target))
	for key, value := range target {
		clone[key] = append(json.RawMessage(nil), value...)
	}
	return clone
}

func rawJSON(value any) json.RawMessage {
	raw, _ := json.Marshal(value)
	return raw
}

func removeCacheFields(definition bakeDefinition) bakeDefinition {
	clone := bakeDefinition{Group: definition.Group, Target: map[string]targetDefinition{}}
	for name, target := range definition.Target {
		adapted := cloneTarget(target)
		delete(adapted, "cache-from")
		delete(adapted, "cache-to")
		clone.Target[name] = adapted
	}
	return clone
}

func writeBakeFile(definition bakeDefinition) (string, error) {
	temporary, err := os.CreateTemp("", "gopulse-build-")
	if err != nil {
		return "", err
	}
	path := temporary.Name()
	if err := encodeBake(temporary, definition); err != nil {
		temporary.Close()
		os.Remove(path)
		return "", err
	}
	if err := temporary.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

func overwriteBakeFile(path string, definition bakeDefinition) error {
	temporary, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := encodeBake(temporary, definition); err != nil {
		temporary.Close()
		return err
	}
	return temporary.Close()
}

func encodeBake(writer *os.File, definition bakeDefinition) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(definition)
}

func isCacheFailure(output string) bool {
	lines := strings.Split(output, "\n")
	last := ""
	for _, line := range lines {
		if strings.Contains(strings.ToLower(line), "failed to solve:") {
			last = line
		}
	}
	return last != "" && cacheFailurePattern.MatchString(last)
}

func printResult(result commandResult) {
	if result.Stdout != "" {
		fmt.Print(result.Stdout)
		if !strings.HasSuffix(result.Stdout, "\n") {
			fmt.Println()
		}
	}
	if result.Stderr != "" {
		fmt.Fprint(os.Stderr, result.Stderr)
		if !strings.HasSuffix(result.Stderr, "\n") {
			fmt.Fprintln(os.Stderr)
		}
	}
}

func runCommand(name string, args []string, dir string, environment []string) commandResult {
	command := exec.Command(name, args...)
	command.Dir = dir
	command.Env = environment
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	result := commandResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if err == nil {
		return result
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		result.Code = exit.ExitCode()
		return result
	}
	result.Code = 1
	result.Stderr = strings.TrimSpace(result.Stderr + "\n" + err.Error())
	return result
}

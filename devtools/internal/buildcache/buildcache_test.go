package buildcache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeRunner(repo string, results *[]commandResult, configs *[]bakeDefinition) commandRunner {
	return func(name string, args []string, dir string, environment []string) commandResult {
		if name == "git" {
			return commandResult{Stdout: strings.Repeat("a", 40) + "\n"}
		}
		if len(args) >= 4 && args[0] == "compose" {
			targets := map[string]targetDefinition{}
			for _, target := range Targets {
				targets[target] = targetDefinition{
					"context": rawJSON(repo),
					"target":  rawJSON(target),
				}
			}
			raw, _ := json.Marshal(composeDefinition{Target: targets})
			return commandResult{Stdout: string(raw)}
		}
		if name != "docker" || len(args) < 4 || args[0] != "buildx" {
			return commandResult{Code: 1, Stderr: "unexpected command"}
		}
		path := args[3]
		raw, err := os.ReadFile(path)
		if err != nil {
			return commandResult{Code: 1, Stderr: err.Error()}
		}
		var config bakeDefinition
		if err := json.Unmarshal(raw, &config); err != nil {
			return commandResult{Code: 1, Stderr: err.Error()}
		}
		*configs = append(*configs, config)
		result := (*results)[0]
		*results = (*results)[1:]
		return result
	}
}

func testRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "VERSION"), []byte("2.3.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func runTest(t *testing.T, environment map[string]string, options Options, results []commandResult) (int, []bakeDefinition) {
	t.Helper()
	root := testRepo(t)
	previous := map[string]string{}
	for key := range environment {
		previous[key] = os.Getenv(key)
		_ = os.Setenv(key, environment[key])
	}
	t.Cleanup(func() {
		for key, value := range previous {
			if value == "" {
				_ = os.Unsetenv(key)
			} else {
				_ = os.Setenv(key, value)
			}
		}
	})
	configs := []bakeDefinition{}
	code, err := runWith(root, options, fakeRunner(root, &results, &configs))
	if err != nil {
		t.Fatal(err)
	}
	return code, configs
}

func TestBuildWithoutCache(t *testing.T) {
	code, configs := runTest(t, map[string]string{}, Options{}, []commandResult{{}})
	if code != 0 || len(configs) != 1 {
		t.Fatalf("code=%d configs=%d", code, len(configs))
	}
	if _, ok := configs[0].Target["backend"]["cache-from"]; ok {
		t.Fatal("cold build unexpectedly included cache-from")
	}
}

func TestCacheDefinitionSeparatesTargetsAndKeepsIdentity(t *testing.T) {
	code, configs := runTest(t, map[string]string{
		"ACTIONS_RUNTIME_TOKEN": "private-token",
		"ACTIONS_RESULTS_URL":   "https://cache.invalid",
	}, Options{}, []commandResult{{}})
	if code != 0 || len(configs) != 1 {
		t.Fatalf("code=%d configs=%d", code, len(configs))
	}
	if len(configs[0].Target) != len(Targets) {
		t.Fatalf("targets=%d, want %d", len(configs[0].Target), len(Targets))
	}
	for _, name := range Targets {
		target := configs[0].Target[name]
		if string(target["cache-from"]) == "" || !strings.Contains(string(target["cache-to"]), "ignore-error=true") {
			t.Fatalf("target %s has no cache contract", name)
		}
		if !strings.Contains(string(target["cache-from"]), "gopulse-"+name+"-linux-amd64-v1") {
			t.Fatalf("target %s is not isolated", name)
		}
	}
}

func TestCacheFailureRetriesOnceWithoutRemoteCache(t *testing.T) {
	code, configs := runTest(t, map[string]string{
		"ACTIONS_RUNTIME_TOKEN": "token",
		"ACTIONS_RESULTS_URL":   "https://cache.invalid",
	}, Options{}, []commandResult{
		{Code: 1, Stderr: "ERROR: failed to solve: failed to configure gha cache: HTTP 503"},
		{},
	})
	if code != 0 || len(configs) != 2 {
		t.Fatalf("code=%d configs=%d", code, len(configs))
	}
	if _, ok := configs[1].Target["backend"]["cache-from"]; ok {
		t.Fatal("fallback build retained cache-from")
	}
}

func TestFallbackFailurePropagatesAndDoesNotRetryAgain(t *testing.T) {
	code, configs := runTest(t, map[string]string{
		"ACTIONS_RUNTIME_TOKEN": "token",
		"ACTIONS_RESULTS_URL":   "https://cache.invalid",
	}, Options{}, []commandResult{
		{Code: 1, Stderr: "failed to solve: failed to import cache manifest"},
		{Code: 23, Stderr: "compiler failed"},
	})
	if code != 23 || len(configs) != 2 {
		t.Fatalf("code=%d configs=%d", code, len(configs))
	}
}

func TestCompilerFailureIsNotRetried(t *testing.T) {
	code, configs := runTest(t, map[string]string{
		"ACTIONS_RUNTIME_TOKEN": "token",
		"ACTIONS_RESULTS_URL":   "https://cache.invalid",
	}, Options{}, []commandResult{{Code: 17, Stderr: "warning: failed to import cache manifest\nfailed to solve: go build exited with code 1"}})
	if code != 17 || len(configs) != 1 {
		t.Fatalf("code=%d configs=%d", code, len(configs))
	}
}

func TestNoCacheOverridesAvailableRuntime(t *testing.T) {
	code, configs := runTest(t, map[string]string{
		"ACTIONS_RUNTIME_TOKEN": "token",
		"ACTIONS_RESULTS_URL":   "https://cache.invalid",
	}, Options{NoCache: true}, []commandResult{{}})
	if code != 0 || len(configs) != 1 {
		t.Fatalf("code=%d configs=%d", code, len(configs))
	}
	if _, ok := configs[0].Target["backend"]["cache-from"]; ok {
		t.Fatal("--no-cache did not disable remote cache")
	}
}

func TestPrintOnlyDoesNotRunBake(t *testing.T) {
	code, configs := runTest(t, map[string]string{
		"ACTIONS_RUNTIME_TOKEN": "token",
		"ACTIONS_RESULTS_URL":   "https://cache.invalid",
	}, Options{PrintOnly: true}, []commandResult{})
	if code != 0 || len(configs) != 0 {
		t.Fatalf("code=%d configs=%d", code, len(configs))
	}
}

func TestCacheFailureUsesLastSolveError(t *testing.T) {
	if !isCacheFailure("warning: failed to import cache manifest\nfailed to solve: failed to configure gha cache") {
		t.Fatal("cache configuration failure not detected")
	}
	if isCacheFailure("failed to solve: go build exited with code 1") {
		t.Fatal("compiler failure must not be treated as a cache failure")
	}
}

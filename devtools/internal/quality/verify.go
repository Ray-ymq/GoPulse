package quality

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type verifyScope struct {
	Checks map[string]bool `json:"checks"`
}

// VerifyModule runs the exact per-module closure used by CI. BASE_REF is only
// passed to lint, where it controls baseline classification; it never expands
// the module's test or build scope.
func VerifyModule(root, module, baseRef string) error {
	goModules, npmModules, err := rootModules(root)
	if err != nil {
		return err
	}
	known := append(append([]string{}, goModules...), npmModules...)
	if !contains(known, module) {
		return fmt.Errorf("unknown MODULE %q; expected one of %s", module, strings.Join(known, ", "))
	}
	commands := [][]string{
		{"check", "MODULE=" + module},
		{"lint", "MODULE=" + module},
		{"test", "MODULE=" + module},
	}
	if contains(goModules, module) {
		commands = append(commands, []string{"race", "MODULE=" + module})
	}
	commands = append(commands, []string{"build", "MODULE=" + module})
	for _, command := range commands {
		if baseRef != "" && command[0] == "lint" {
			command = append(command, "BASE_REF="+baseRef)
		}
		if err := runMake(root, command...); err != nil {
			return err
		}
	}
	return nil
}

// Verify selects governance, module, and real checks from the common-ancestor
// scope. PLAN_ONLY prints the selector's JSON and performs no other command.
func Verify(root, baseRef string, planOnly bool) error {
	if strings.TrimSpace(baseRef) == "" {
		return fmt.Errorf("BASE_REF is required for make verify")
	}
	selection, raw, err := readScope(root, baseRef)
	if err != nil {
		return err
	}
	if planOnly {
		fmt.Print(raw)
		return nil
	}
	if err := runPython(root, "ci/validate_versions.py"); err != nil {
		return err
	}
	if selection.Checks["tools"] {
		if err := runMake(root, "check-compose"); err != nil {
			return err
		}
	}
	goModules, npmModules, err := rootModules(root)
	if err != nil {
		return err
	}
	for _, module := range append(append([]string{}, goModules...), npmModules...) {
		if selection.Checks[checkName(module)] {
			if err := VerifyModule(root, module, baseRef); err != nil {
				return err
			}
		}
	}
	for _, check := range []struct {
		name string
		args []string
	}{
		{"integration_business", []string{"integration", "SCOPE=business"}},
		{"integration_observe", []string{"integration", "SCOPE=observe"}},
		{"e2e_business", []string{"e2e", "SCOPE=business"}},
		{"e2e_observe", []string{"e2e", "SCOPE=observe"}},
		{"compose", []string{"verify-compose"}},
	} {
		if selection.Checks[check.name] {
			if err := runMake(root, check.args...); err != nil {
				return err
			}
		}
	}
	return nil
}

func readScope(root, baseRef string) (verifyScope, string, error) {
	command := exec.Command("python3", "ci/quality_scope.py", "--repo", root, "--base-ref", baseRef, "--format", "json")
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		return verifyScope{}, "", fmt.Errorf("quality scope selection failed: %w", err)
	}
	var selection verifyScope
	if err := json.Unmarshal(output, &selection); err != nil {
		return verifyScope{}, "", fmt.Errorf("decode quality scope: %w", err)
	}
	return selection, string(output), nil
}

func rootModules(root string) ([]string, []string, error) {
	goOutput, err := verifyCommandOutput(root, "make", "--no-print-directory", "list-go-modules")
	if err != nil {
		return nil, nil, fmt.Errorf("read Go module list: %w", err)
	}
	npmOutput, err := verifyCommandOutput(root, "make", "--no-print-directory", "list-npm-modules")
	if err != nil {
		return nil, nil, fmt.Errorf("read npm module list: %w", err)
	}
	goModules, npmModules := strings.Fields(goOutput), strings.Fields(npmOutput)
	if len(goModules) == 0 || len(npmModules) == 0 {
		return nil, nil, fmt.Errorf("root Make module lists must not be empty")
	}
	return goModules, npmModules, nil
}

func runMake(root string, args ...string) error {
	return runCommand(root, "make", args...)
}

func runPython(root, script string, args ...string) error {
	return runCommand(root, "python3", append([]string{script}, args...)...)
}

func runCommand(root, name string, args ...string) error {
	command := exec.Command(name, args...)
	command.Dir = root
	command.Env = os.Environ()
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("%s exited with code %d", strings.Join(append([]string{name}, args...), " "), exit.ExitCode())
		}
		return fmt.Errorf("run %s: %w", name, err)
	}
	return nil
}

func verifyCommandOutput(root, name string, args ...string) (string, error) {
	command := exec.Command(name, args...)
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func checkName(module string) string { return strings.ReplaceAll(module, "-", "_") }

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

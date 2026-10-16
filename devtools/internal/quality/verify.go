package quality

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type verifyScope struct {
	Modules []string `json:"modules"`
}

// VerifyModule runs the same three Makefile entries used by module CI.
// baseRef remains accepted by the CLI but only affects multi-module selection.
func VerifyModule(root, module, baseRef string) error {
	goModules, npmModules, err := rootModules(root)
	if err != nil {
		return err
	}
	known := append(append([]string{}, goModules...), npmModules...)
	if !contains(known, module) {
		return fmt.Errorf("unknown MODULE %q; expected one of %s", module, strings.Join(known, ", "))
	}
	for _, target := range []string{"ci-check", "ci-test", "ci-build"} {
		if err := runMake(root, target, "MODULE="+module); err != nil {
			return err
		}
	}
	return nil
}

// Verify checks only directly changed Go modules in a local common-ancestor diff.
// GitHub CI selects modules exclusively from PR labels instead. Integration,
// Compose runtime and release commands remain explicit entry points.
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
	for _, module := range selection.Modules {
		if err := VerifyModule(root, module, baseRef); err != nil {
			return err
		}
	}
	return nil
}

func readScope(root, baseRef string) (verifyScope, string, error) {
	command := exec.Command("python3", "ci/modules.py", "plan", "--repo", root, "--base-ref", baseRef)
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

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

// Package frontenddeps owns the lockfile-driven frontend dependency contract.
package frontenddeps

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/Ray-ymq/GoPulse/devtools/internal/proc"
	"github.com/Ray-ymq/GoPulse/devtools/internal/workspace"
)

var applications = map[string]struct{}{
	"frontend":       {},
	"admin-frontend": {},
}

// Runner is the side effect used to install dependencies. It is injectable so
// the marker and failure contract can be tested without running npm.
type Runner func(command []string, cwd string, environment map[string]string) error

// Ensure installs one frontend only when its inputs or runtime changed.
func Ensure(root, application string) error {
	return EnsureWithRunner(root, application, func(command []string, cwd string, environment map[string]string) error {
		return proc.Run(command, cwd, environment, "npm dependency installation for "+application, 0)
	})
}

// EnsureWithRunner is the behavior seam used by focused tests and by callers
// that need to record or constrain the installation command.
func EnsureWithRunner(root, application string, runner Runner) error {
	if _, ok := applications[application]; !ok {
		return fmt.Errorf("unknown frontend application %q", application)
	}
	if runner == nil {
		return fmt.Errorf("frontend dependency runner is nil")
	}
	ws, err := workspace.For(root)
	if err != nil {
		return err
	}
	if err := protectActiveApplication(ws, application); err != nil {
		return err
	}
	lock, err := takeLock(filepath.Join(ws.PrivateRoot(), "frontenddeps", application+".lock"))
	if err != nil {
		return err
	}
	defer lock()

	applicationRoot := filepath.Join(ws.Root, application)
	nodeVersion, err := commandVersion("node", "--version")
	if err != nil {
		return fmt.Errorf("node is required for %s dependencies: %w", application, err)
	}
	npmVersion, err := commandVersion("npm", "--version")
	if err != nil {
		return fmt.Errorf("npm is required for %s dependencies: %w", application, err)
	}
	digest, err := inputDigest(applicationRoot, application, nodeVersion, npmVersion)
	if err != nil {
		return err
	}
	markerPath := filepath.Join(ws.PrivateRoot(), "frontenddeps", application+".json")
	if markerMatches(markerPath, digest) && directoryExists(filepath.Join(applicationRoot, "node_modules")) {
		fmt.Printf("[gopulse] reusing %s dependencies (input %s)\n", application, digest[:12])
		return nil
	}
	if err := runner([]string{"npm", "ci", "--no-audit", "--no-fund"}, applicationRoot, nil); err != nil {
		return err
	}
	return workspace.SaveJSON(markerPath, map[string]string{
		"application": application,
		"digest":      digest,
		"node":        nodeVersion,
		"npm":         npmVersion,
		"goos":        runtime.GOOS,
		"goarch":      runtime.GOARCH,
	})
}

// InputDigest exposes the exact identity used by the reuse marker.
func InputDigest(root, application, nodeVersion, npmVersion string) (string, error) {
	return inputDigest(root, application, nodeVersion, npmVersion)
}

func inputDigest(root, application, nodeVersion, npmVersion string) (string, error) {
	hash := sha256.New()
	hash.Write([]byte(application + "\x00" + nodeVersion + "\x00" + npmVersion + "\x00" + runtime.GOOS + "\x00" + runtime.GOARCH + "\x00"))
	for _, name := range []string{"package.json", "package-lock.json"} {
		path := filepath.Join(root, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", path, err)
		}
		hash.Write([]byte(name + "\x00"))
		hash.Write(raw)
		hash.Write([]byte("\x00"))
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func commandVersion(command string, argument string) (string, error) {
	if _, err := exec.LookPath(command); err != nil {
		return "", err
	}
	process := exec.Command(command, argument)
	raw, err := process.Output()
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(raw))
	if value == "" {
		return "", fmt.Errorf("%s returned an empty version", command)
	}
	return value, nil
}

func markerMatches(path, digest string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	marker := struct {
		Digest string `json:"digest"`
	}{}
	return json.Unmarshal(raw, &marker) == nil && marker.Digest == digest
}

func directoryExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func protectActiveApplication(ws workspace.Workspace, application string) error {
	state, err := workspace.Load(ws)
	if err != nil || state == nil || state.Status != "running" {
		return err
	}
	record, found := state.Processes[application]
	if found && proc.Owned(record) {
		return fmt.Errorf("cannot replace %s dependencies while its owned development process is running; run make stop first", application)
	}
	return nil
}

func takeLock(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	handle, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(handle.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = handle.Close()
		return nil, fmt.Errorf("another %s dependency installation owns the lock", filepath.Base(filepath.Dir(path)))
	}
	return func() {
		_ = syscall.Flock(int(handle.Fd()), syscall.LOCK_UN)
		_ = handle.Close()
	}, nil
}

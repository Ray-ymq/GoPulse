// Package packaging implements the immutable release candidate: it builds the
// product images from a committed Git archive, assembles the transport bundle,
// verifies the candidate against its registry digests and promotes the same
// index. The manifest contract itself lives in lifecycle/internal/release.
package packaging

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Products, Sources and Platforms mirror the closed release manifest contract.
var (
	Products  = []string{"backend", "business-worker", "search-indexer", "frontend", "admin-frontend", "router", "marshaller", "monitor", "redis-exporter"}
	Sources   = []string{"redis", "mysql", "rabbitmq", "kafka", "elasticsearch", "victoriametrics"}
	Platforms = []string{"linux/amd64", "linux/arm64"}
)

// RegistryImage is the pinned loopback registry used when the caller does not
// own one; the digest is the one the release workflow pinned before.
const RegistryImage = "registry:2@sha256:a3d8aaa63ed8681a604f1dea0aa03f100d5895b6a58ace528858a7b332415373"

// composeImageAliases resolves runtime service names to one manifest entry.
// backend-2 and platform-api share the single backend image; the lifecycle
// tool keeps its runtime role labels.
var composeImageAliases = map[string]string{
	"backend-2":                   "backend",
	"platform-api":                "backend",
	"business-worker-2":           "business-worker",
	"search-indexer-2":            "search-indexer",
	"router-2":                    "router",
	"marshaller-2":                "marshaller",
	"observability-elasticsearch": "elasticsearch",
	"migrate":                     "backend",
	"search-init":                 "backend",
	"admin-role":                  "backend",
	"kafka-init":                  "kafka",
}

// ComposeImageAliases returns a copy of the delivery-side runtime alias table.
func ComposeImageAliases() map[string]string {
	out := make(map[string]string, len(composeImageAliases))
	for name, logical := range composeImageAliases {
		out[name] = logical
	}
	return out
}

var (
	registryNamespace = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.:-]*(/[a-z0-9._-]+)*$`)
	platformArgument  = regexp.MustCompile(`^linux/(amd64|arm64)(,linux/(amd64|arm64))?$`)
	digestValue       = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	numericUser       = regexp.MustCompile(`^[0-9]+:[0-9]+$`)
)

// UsageError marks argument and precondition failures, which exit with code 2.
type UsageError struct{ Message string }

func (e *UsageError) Error() string { return e.Message }

func usagef(format string, args ...any) error {
	return &UsageError{Message: fmt.Sprintf(format, args...)}
}

// Sum returns the prefixed SHA256 of data, the only digest spelling the
// release contract uses.
func Sum(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func sumHex(data []byte) string { return Sum(data)[7:] }

// Repo is the committed source tree every candidate input comes from.
type Repo struct{ Root string }

// OpenRepo resolves the repository root, from an explicit path or the Git
// worktree containing the process.
func OpenRepo(explicit string) (*Repo, error) {
	root := explicit
	if root == "" {
		out, err := output("", "git", "rev-parse", "--show-toplevel")
		if err != nil {
			return nil, errors.New("cannot locate the repository root; run inside the Git worktree")
		}
		root = strings.TrimSpace(out)
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(absolute, "VERSION")); err != nil {
		return nil, usagef("repository root %s has no VERSION file", absolute)
	}
	return &Repo{Root: absolute}, nil
}

// Version reads the single completed product version.
func (r *Repo) Version() (string, error) {
	data, err := os.ReadFile(filepath.Join(r.Root, "VERSION"))
	if err != nil {
		return "", err
	}
	version := strings.TrimSpace(string(data))
	if !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(version) {
		return "", usagef("VERSION must be major.minor.patch, found %q", version)
	}
	return version, nil
}

// Revision is the committed revision the candidate binds to.
func (r *Repo) Revision() (string, error) {
	out, err := output(r.Root, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", errors.New("cannot read the committed revision")
	}
	return strings.TrimSpace(out), nil
}

// Clean refuses candidate builds from a source tree with tracked modifications:
// untracked local state never enters the Git archive context anyway.
func (r *Repo) Clean() error {
	out, err := output(r.Root, "git", "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return errors.New("cannot inspect the source tree")
	}
	if strings.TrimSpace(out) != "" {
		return usagef("candidate builds require a committed source tree")
	}
	return nil
}

// File reads one bundle input from the committed tree.
func (r *Repo) File(path string) ([]byte, error) {
	return os.ReadFile(filepath.Join(r.Root, filepath.FromSlash(path)))
}

// command builds one child process rooted at the repository.
func command(dir, name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stderr = os.Stderr
	return cmd
}

// output runs a command and returns its standard output.
func output(dir, name string, args ...string) (string, error) {
	cmd := command(dir, name, args...)
	var stdout strings.Builder
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %s failed: %w", name, strings.Join(args, " "), err)
	}
	return stdout.String(), nil
}

// attached runs a command with inherited output.
func attached(dir, name string, args ...string) error {
	if err := command(dir, name, args...).Run(); err != nil {
		return fmt.Errorf("%s %s failed: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

// streamed runs a command with the given standard input.
func streamed(dir string, stdin io.Reader, name string, args ...string) error {
	cmd := command(dir, name, args...)
	cmd.Stdin = stdin
	cmd.Stdout = os.Stdout
	return cmd.Run()
}

// sortedNames keeps archive members, payload digests and checksums in one
// stable order on every host.
func sortedNames[V any](values map[string]V) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

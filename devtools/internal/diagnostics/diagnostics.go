// Package diagnostics writes private, post-cleanup failure evidence for one
// workspace. It deliberately has no public receipt schema: the files are
// operator material, while lifecycle and acceptance receipts keep their
// existing contracts.
package diagnostics

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Ray-ymq/GoPulse/devtools/internal/workspace"
)

const (
	maxTextBytes = 2 * 1024 * 1024
	maxTreeBytes = 20 * 1024 * 1024
	maxPathParts = 64
)

var secretAssignment = regexp.MustCompile(`(?i)((?:password|passwd|token|secret|authorization|cookie|api[_-]?key|jwt)["']?[[:space:]]*[:=][[:space:]]*["']?)([^"'[:space:],;]+)`)

// Metadata describes safe candidate and environment identity. Values in
// Environment are labels or non-secret facts supplied by the caller; they are
// still passed through Redact before being written.
type Metadata struct {
	Label        string
	Project      string
	Branch       string
	Revision     string
	Version      string
	Mode         string
	Scope        string
	Environment  map[string]string
	Reproduction [][]string
	Secrets      []string
}

// Command is one actual foreground command and its bounded output.
type Command struct {
	Label    string
	Args     []string
	Cwd      string
	ExitCode int
	Stdout   string
	Stderr   string
	TimedOut bool
}

// Report owns one failure directory. AddCleanupError must be called after the
// primary failure and before Finalize so cleanup errors remain separate.
type Report struct {
	directory string
	metadata  Metadata
	secrets   []string
	commands  []Command
	logs      []string
	cleanup   []string
}

// New creates a private failure directory below the existing workspace
// identity. Every directory in the new path is checked with Lstat so a
// symlink cannot redirect evidence outside the checkout's private root.
func New(ws workspace.Workspace, metadata Metadata) (*Report, error) {
	if ws.Root == "" || ws.Identity == "" {
		return nil, fmt.Errorf("diagnostic workspace identity is incomplete")
	}
	base := filepath.Join(ws.PrivateRoot(), "failures")
	if err := safeMkdirAll(ws.Root, base); err != nil {
		return nil, fmt.Errorf("create diagnostic root: %w", err)
	}
	stamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	label := safeName(metadata.Label)
	if label == "" {
		label = "failure"
	}
	for attempt := 0; attempt < 8; attempt++ {
		var random [6]byte
		if _, err := rand.Read(random[:]); err != nil {
			return nil, fmt.Errorf("create diagnostic identity: %w", err)
		}
		name := fmt.Sprintf("%s-%s-%s", stamp, label, hex.EncodeToString(random[:]))
		directory := filepath.Join(base, name)
		if err := os.Mkdir(directory, 0o700); err == nil {
			metadata.Label = label
			return &Report{directory: directory, metadata: metadata, secrets: append([]string(nil), metadata.Secrets...)}, nil
		} else if !os.IsExist(err) {
			return nil, fmt.Errorf("create diagnostic directory: %w", err)
		}
	}
	return nil, fmt.Errorf("could not allocate a unique diagnostic directory")
}

// Directory returns the durable failure directory.
func (r *Report) Directory() string { return r.directory }

// RecordCommand records a command and immediately writes its bounded output so
// an interrupted cleanup still leaves useful material.
func (r *Report) RecordCommand(command Command) error {
	command = r.sanitizeCommand(command)
	r.commands = append(r.commands, command)
	name := fmt.Sprintf("command-%02d-%s.txt", len(r.commands), safeName(command.Label))
	var content strings.Builder
	fmt.Fprintf(&content, "label: %s\n", command.Label)
	fmt.Fprintf(&content, "cwd: %s\n", command.Cwd)
	fmt.Fprintf(&content, "exit_code: %d\n", command.ExitCode)
	fmt.Fprintf(&content, "timed_out: %t\n\n", command.TimedOut)
	content.WriteString("stdout:\n")
	content.WriteString(command.Stdout)
	content.WriteString("\n\nstderr:\n")
	content.WriteString(command.Stderr)
	return writePrivate(filepath.Join(r.directory, name), []byte(content.String()))
}

// AddCleanupError adds a cleanup failure without changing the primary error.
func (r *Report) AddCleanupError(err error) {
	if err != nil {
		r.cleanup = append(r.cleanup, Redact(err.Error(), r.secrets))
	}
}

// CopyText copies a bounded text log or browser report into the report. Source
// symlinks are rejected; the destination remains private and mode 0600.
func (r *Report) CopyText(label, source string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return fmt.Errorf("read diagnostic source %s: %w", source, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("diagnostic source %s is not a regular file", source)
	}
	file, err := os.Open(source)
	if err != nil {
		return err
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, maxTextBytes+1))
	closeErr := file.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	truncated := len(raw) > maxTextBytes
	if truncated {
		raw = raw[:maxTextBytes]
	}
	text := Redact(string(raw), r.secrets)
	if truncated {
		text += "\n[diagnostic excerpt truncated at 2097152 bytes]\n"
	}
	name := safeName(label)
	if name == "" {
		name = fmt.Sprintf("log-%02d", len(r.logs)+1)
	}
	path := filepath.Join(r.directory, "logs", name+".txt")
	if err := safeMkdir(pathDir(path)); err != nil {
		return err
	}
	if err := writePrivate(path, []byte(text)); err != nil {
		return err
	}
	r.logs = append(r.logs, path)
	return nil
}

// WriteText stores a generated excerpt using the same size and redaction
// boundary as copied logs.
func (r *Report) WriteText(label, text string) error {
	if len(text) > maxTextBytes {
		text = text[:maxTextBytes] + "\n[diagnostic excerpt truncated at 2097152 bytes]\n"
	}
	name := safeName(label)
	if name == "" {
		name = fmt.Sprintf("excerpt-%02d", len(r.logs)+1)
	}
	path := filepath.Join(r.directory, "logs", name+".txt")
	if err := safeMkdir(pathDir(path)); err != nil {
		return err
	}
	if err := writePrivate(path, []byte(Redact(text, r.secrets))); err != nil {
		return err
	}
	r.logs = append(r.logs, path)
	return nil
}

// CopyTree preserves a bounded browser report directory before its owning
// process or container is removed. It accepts only regular files and refuses
// symlink entries so the report cannot walk outside the intended source tree.
func (r *Report) CopyTree(label, source string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return fmt.Errorf("read diagnostic tree %s: %w", source, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("diagnostic tree %s is not a directory", source)
	}
	destination := filepath.Join(r.directory, "artifacts", safeName(label))
	if err := safeMkdir(destination); err != nil {
		return err
	}
	total := int64(0)
	err = filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == source {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("diagnostic tree contains symlink %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		fileInfo, statErr := entry.Info()
		if statErr != nil {
			return statErr
		}
		if total+fileInfo.Size() > maxTreeBytes {
			return fmt.Errorf("diagnostic tree exceeds 20 MiB")
		}
		relative, relErr := filepath.Rel(source, path)
		if relErr != nil || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || relative == ".." {
			return fmt.Errorf("diagnostic artifact escapes source tree")
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		target := filepath.Join(destination, relative)
		parent := filepath.Dir(target)
		if parent != destination {
			if err := safeMkdirAll(destination, parent); err != nil {
				return err
			}
		}
		if err := writePrivate(target, content); err != nil {
			return err
		}
		total += fileInfo.Size()
		return nil
	})
	if err != nil {
		return err
	}
	r.logs = append(r.logs, destination)
	return nil
}

// Finalize writes failure.txt, environment.txt, and reproduce.txt after all
// cleanup has run. Its own error is returned independently by callers.
func (r *Report) Finalize(primary error, primaryExit int) error {
	if primary == nil {
		return fmt.Errorf("diagnostic report requires a primary failure")
	}
	if err := r.writeEnvironment(); err != nil {
		return err
	}
	if err := r.writeReproduction(); err != nil {
		return err
	}
	var failure strings.Builder
	fmt.Fprintf(&failure, "primary_error: %s\n", Redact(primary.Error(), r.secrets))
	fmt.Fprintf(&failure, "primary_exit_code: %d\n", primaryExit)
	fmt.Fprintf(&failure, "diagnostic_directory: %s\n", r.directory)
	failure.WriteString("commands:\n")
	for _, command := range r.commands {
		fmt.Fprintf(&failure, "- %s exit=%d timed_out=%t cwd=%s argv=%s\n", command.Label, command.ExitCode, command.TimedOut, command.Cwd, shellJoin(command.Args))
	}
	failure.WriteString("logs:\n")
	for _, path := range r.logs {
		fmt.Fprintf(&failure, "- %s\n", path)
	}
	failure.WriteString("cleanup_errors:\n")
	if len(r.cleanup) == 0 {
		failure.WriteString("- none\n")
	} else {
		for _, cleanup := range r.cleanup {
			fmt.Fprintf(&failure, "- %s\n", cleanup)
		}
	}
	if err := writePrivate(filepath.Join(r.directory, "failure.txt"), []byte(failure.String())); err != nil {
		return err
	}
	if len(r.cleanup) > 0 {
		return writePrivate(filepath.Join(r.directory, "cleanup.txt"), []byte(strings.Join(r.cleanup, "\n")+"\n"))
	}
	return nil
}

func (r *Report) sanitizeCommand(command Command) Command {
	command.Label = Redact(command.Label, r.secrets)
	command.Cwd = Redact(command.Cwd, r.secrets)
	for index, arg := range command.Args {
		command.Args[index] = Redact(arg, r.secrets)
	}
	command.Stdout = Redact(command.Stdout, r.secrets)
	command.Stderr = Redact(command.Stderr, r.secrets)
	return command
}

func (r *Report) writeEnvironment() error {
	var content strings.Builder
	fmt.Fprintf(&content, "workspace_id: %s\n", r.metadata.Environment["workspace_id"])
	fmt.Fprintf(&content, "workspace_root: %s\n", r.metadata.Environment["workspace_root"])
	fmt.Fprintf(&content, "project: %s\n", r.metadata.Project)
	fmt.Fprintf(&content, "branch: %s\n", r.metadata.Branch)
	fmt.Fprintf(&content, "revision: %s\n", r.metadata.Revision)
	fmt.Fprintf(&content, "version: %s\n", r.metadata.Version)
	fmt.Fprintf(&content, "mode: %s\n", r.metadata.Mode)
	fmt.Fprintf(&content, "scope: %s\n", r.metadata.Scope)
	keys := make([]string, 0, len(r.metadata.Environment))
	for key := range r.metadata.Environment {
		if key != "workspace_id" && key != "workspace_root" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(&content, "%s: %s\n", key, Redact(r.metadata.Environment[key], r.secrets))
	}
	return writePrivate(filepath.Join(r.directory, "environment.txt"), []byte(Redact(content.String(), r.secrets)))
}

func (r *Report) writeReproduction() error {
	var content strings.Builder
	content.WriteString("reproduction_commands:\n")
	if len(r.metadata.Reproduction) == 0 {
		content.WriteString("- unavailable\n")
	} else {
		for _, command := range r.metadata.Reproduction {
			content.WriteString("- ")
			content.WriteString(shellJoin(command))
			content.WriteByte('\n')
		}
	}
	return writePrivate(filepath.Join(r.directory, "reproduce.txt"), []byte(Redact(content.String(), r.secrets)))
}

func shellJoin(args []string) string {
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "" {
			parts = append(parts, "''")
			continue
		}
		if strings.IndexFunc(arg, func(r rune) bool {
			return r == ' ' || r == '\t' || r == '\n' || r == '\'' || r == '"' || r == '$' || r == '`' || r == ';'
		}) < 0 {
			parts = append(parts, arg)
			continue
		}
		parts = append(parts, "'"+strings.ReplaceAll(arg, "'", "'\\''")+"'")
	}
	return strings.Join(parts, " ")
}

// Redact removes caller-provided secrets and common secret assignments from
// command output, environment excerpts, and reproduction material.
func Redact(value string, secrets []string) string {
	ordered := append([]string(nil), secrets...)
	sort.SliceStable(ordered, func(left, right int) bool { return len(ordered[left]) > len(ordered[right]) })
	for _, secret := range ordered {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	return secretAssignment.ReplaceAllString(value, `${1}[REDACTED]`)
}

func safeName(value string) string {
	value = strings.TrimSpace(value)
	var builder strings.Builder
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '.' || character == '_' || character == '-' {
			builder.WriteRune(character)
		} else {
			builder.WriteByte('-')
		}
	}
	return strings.Trim(builder.String(), ".-")
}

func safeMkdir(path string) error {
	if err := os.Mkdir(path, 0o700); err != nil && !os.IsExist(err) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("diagnostic path %s is not a directory", path)
	}
	return os.Chmod(path, 0o700)
}

func safeMkdirAll(root, target string) error {
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || relative == ".." {
		return fmt.Errorf("diagnostic path escapes workspace root")
	}
	current := root
	parts := strings.Split(relative, string(filepath.Separator))
	if len(parts) > maxPathParts {
		return fmt.Errorf("diagnostic path is too deep")
	}
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		if err := safeMkdir(current); err != nil {
			return err
		}
	}
	return nil
}

func pathDir(path string) string { return filepath.Dir(path) }

func writePrivate(path string, content []byte) error {
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

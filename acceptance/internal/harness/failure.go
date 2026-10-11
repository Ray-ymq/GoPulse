package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const acceptanceExcerptLimit = 2 * 1024 * 1024

var acceptanceSecretAssignment = regexp.MustCompile(`(?i)((?:password|passwd|token|secret|authorization|cookie|api[_-]?key|jwt)["']?[[:space:]]*[:=][[:space:]]*["']?)([^"'[:space:],;]+)`)

type failureCommand struct {
	Args     []string
	ExitCode int
	Stdout   string
	Stderr   string
}

type failureEvidence struct {
	directory string
	primary   string
	exitCode  int
	commands  []failureCommand
	cleanup   []string
	secrets   []string
	logs      []string
}

func (s *Session) ensureFailure(err error, exitCode int) {
	if err == nil {
		return
	}
	if s.failure == nil {
		secrets := []string{}
		for key, value := range s.values {
			upper := strings.ToUpper(key)
			if value != "" && (strings.Contains(upper, "PASSWORD") || strings.Contains(upper, "TOKEN") || strings.Contains(upper, "SECRET") || strings.Contains(upper, "COOKIE")) {
				secrets = append(secrets, value)
			}
		}
		directory, createErr := createFailureDirectory(s.Root, s.Token)
		if createErr != nil {
			fmt.Fprintf(os.Stderr, "[gopulse] warning: could not create acceptance diagnostics: %v\n", createErr)
		}
		s.failure = &failureEvidence{directory: directory, primary: err.Error(), exitCode: exitCode, secrets: secrets}
		return
	}
	// A cleanup error is deliberately not allowed to replace the original
	// failing command. If no primary exists, this error becomes the failure.
	if s.failure.primary == "" {
		s.failure.primary = err.Error()
		s.failure.exitCode = exitCode
	}
}

func (s *Session) recordFailureCommand(name string, args []string, result Result) {
	if s.failure == nil || s.failure.directory == "" {
		return
	}
	commandArgs := append([]string{name}, args...)
	for index, arg := range commandArgs {
		commandArgs[index] = redactAcceptance(arg, s.failure.secrets)
	}
	command := failureCommand{
		Args: commandArgs, ExitCode: result.ExitCode,
		Stdout: redactAcceptance(result.Stdout, s.failure.secrets), Stderr: redactAcceptance(result.Stderr, s.failure.secrets),
	}
	s.failure.commands = append(s.failure.commands, command)
	namePart := safeAcceptanceName(name)
	if namePart == "" {
		namePart = "command"
	}
	path := filepath.Join(s.failure.directory, fmt.Sprintf("command-%02d-%s.txt", len(s.failure.commands), namePart))
	content := fmt.Sprintf("exit_code: %d\nargv: %s\n\nstdout:\n%s\n\nstderr:\n%s\n", result.ExitCode,
		shellJoinAcceptance(command.Args), command.Stdout, command.Stderr)
	if err := writeAcceptancePrivate(path, []byte(content)); err != nil {
		s.addCleanupFailure("write command diagnostic: " + err.Error())
	}
}

func (s *Session) addCleanupFailure(message string) {
	if message == "" {
		return
	}
	if s.failure == nil {
		s.ensureFailure(fmt.Errorf("acceptance cleanup failed: %s", message), -1)
	}
	if s.failure != nil {
		s.failure.cleanup = append(s.failure.cleanup, redactAcceptance(message, s.failure.secrets))
	}
}

func (s *Session) writeFailureText(label, value string) {
	if s.failure == nil || s.failure.directory == "" {
		return
	}
	if len(value) > acceptanceExcerptLimit {
		value = value[:acceptanceExcerptLimit] + "\n[diagnostic excerpt truncated at 2097152 bytes]\n"
	}
	name := safeAcceptanceName(label)
	if name == "" {
		name = "excerpt"
	}
	path := filepath.Join(s.failure.directory, "logs", name+".txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		s.addCleanupFailure("create log diagnostic: " + err.Error())
		return
	}
	if err := writeAcceptancePrivate(path, []byte(redactAcceptance(value, s.failure.secrets))); err != nil {
		s.addCleanupFailure("write log diagnostic: " + err.Error())
		return
	}
	s.failure.logs = append(s.failure.logs, path)
}

func (s *Session) captureBrowserReport(container, spec string) error {
	if s.failure == nil || s.failure.directory == "" {
		return nil
	}
	destination := filepath.Join(s.failure.directory, "browser-report")
	if err := os.Mkdir(destination, 0o700); err != nil && !os.IsExist(err) {
		return err
	}
	result := s.Run("docker", "cp", container+":/work/frontend/test-results/.", destination)
	if result.ExitCode != 0 {
		return fmt.Errorf("copy browser report for %s failed with exit code %d", spec, result.ExitCode)
	}
	s.failure.logs = append(s.failure.logs, destination)
	return nil
}

func (s *Session) removeOneShot(container string) Result {
	wasCleanup := s.cleanup
	s.cleanup = true
	result := s.Run("docker", "rm", "-f", container)
	s.cleanup = wasCleanup
	if result.ExitCode != 0 {
		s.addCleanupFailure(fmt.Sprintf("remove acceptance container %s failed with exit code %d", container, result.ExitCode))
	}
	return result
}

func (s *Session) finalizeFailure() {
	if s.failure == nil || s.failure.directory == "" || s.failure.primary == "" {
		return
	}
	var environment strings.Builder
	root, _ := filepath.EvalSymlinks(s.Root)
	hash := sha256.Sum256([]byte(root))
	fmt.Fprintf(&environment, "workspace_id: %s\nworkspace_root: %s\nproject: %s\nversion: %s\nrevision: %s\nstarted_at: %s\n",
		hex.EncodeToString(hash[:])[:12], root, s.Project, s.Version, s.Revision, s.receipt.StartedAt)
	_ = writeAcceptancePrivate(filepath.Join(s.failure.directory, "environment.txt"), []byte(redactAcceptance(environment.String(), s.failure.secrets)))
	_ = writeAcceptancePrivate(filepath.Join(s.failure.directory, "reproduce.txt"), []byte("reproduction_commands:\n- make verify-compose\n"))
	var failure strings.Builder
	fmt.Fprintf(&failure, "primary_error: %s\nprimary_exit_code: %d\ndiagnostic_directory: %s\n", redactAcceptance(s.failure.primary, s.failure.secrets), s.failure.exitCode, s.failure.directory)
	failure.WriteString("commands:\n")
	for _, command := range s.failure.commands {
		fmt.Fprintf(&failure, "- exit=%d argv=%s\n", command.ExitCode, shellJoinAcceptance(command.Args))
	}
	failure.WriteString("logs:\n")
	for _, log := range s.failure.logs {
		fmt.Fprintf(&failure, "- %s\n", log)
	}
	failure.WriteString("cleanup_errors:\n")
	if len(s.failure.cleanup) == 0 {
		failure.WriteString("- none\n")
	} else {
		for _, cleanup := range s.failure.cleanup {
			fmt.Fprintf(&failure, "- %s\n", cleanup)
		}
	}
	_ = writeAcceptancePrivate(filepath.Join(s.failure.directory, "failure.txt"), []byte(failure.String()))
	if len(s.failure.cleanup) > 0 {
		_ = writeAcceptancePrivate(filepath.Join(s.failure.directory, "cleanup.txt"), []byte(strings.Join(s.failure.cleanup, "\n")+"\n"))
	}
}

func createFailureDirectory(root, token string) (string, error) {
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256([]byte(resolved))
	base := filepath.Join(resolved, ".run", "local", hex.EncodeToString(hash[:])[:12], "failures")
	if err := safeAcceptanceMkdirAll(resolved, base); err != nil {
		return "", err
	}
	name := time.Now().UTC().Format("20060102T150405.000000000Z") + "-acceptance-" + safeAcceptanceName(token)
	directory := filepath.Join(base, name)
	if err := os.Mkdir(directory, 0o700); err != nil {
		return "", err
	}
	return directory, nil
}

func redactAcceptance(value string, secrets []string) string {
	ordered := append([]string(nil), secrets...)
	sort.SliceStable(ordered, func(left, right int) bool { return len(ordered[left]) > len(ordered[right]) })
	for _, secret := range ordered {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	return acceptanceSecretAssignment.ReplaceAllString(value, `${1}[REDACTED]`)
}

func safeAcceptanceName(value string) string {
	var builder strings.Builder
	for _, character := range strings.TrimSpace(value) {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '-' || character == '_' || character == '.' {
			builder.WriteRune(character)
		} else {
			builder.WriteByte('-')
		}
	}
	return strings.Trim(builder.String(), ".-")
}

func shellJoinAcceptance(args []string) string {
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "" {
			parts = append(parts, "''")
		} else if strings.IndexFunc(arg, func(character rune) bool { return strings.ContainsRune(" \t\n'\"$`;", character) }) >= 0 {
			parts = append(parts, "'"+strings.ReplaceAll(arg, "'", "'\\''")+"'")
		} else {
			parts = append(parts, arg)
		}
	}
	return strings.Join(parts, " ")
}

func safeAcceptanceMkdirAll(root, target string) error {
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("acceptance diagnostic path escapes workspace")
	}
	current := root
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		if err := os.Mkdir(current, 0o700); err != nil && !os.IsExist(err) {
			return err
		}
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("acceptance diagnostic path %s is not a directory", current)
		}
		if err := os.Chmod(current, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func writeAcceptancePrivate(path string, content []byte) error {
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

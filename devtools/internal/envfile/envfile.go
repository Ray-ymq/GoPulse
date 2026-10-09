// Package envfile merges the project's environment contract for a local
// development, observation, or test lifecycle.
//
// Precedence (identical to the replaced Python helper): checked-in
// .env.example defaults, then an explicit or discovered dotenv file, then
// KNOWN_CONFIG_KEYS taken from the caller environment, then mode defaults for
// keys no caller supplied. Test mode additionally forces its isolation values
// so a developer's .env can never leak into an isolated test stack.
package envfile

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Modes accepted by Compose and by the private environment file name.
const (
	ModeDev     = "dev"
	ModeObserve = "observe"
	ModeTest    = "test"
)

var envKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Values is a merged environment. It is intentionally a plain map so callers
// can add process overrides before writing the private file.
type Values map[string]string

// Parse reads the small dotenv contract used by the project without variable
// expansion, matching .env.example comments, blank lines, export prefixes,
// single or double quoted values, and invalid-entry reporting.
func Parse(path string) (Values, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	values := Values{}
	for number, rawLine := range strings.Split(string(raw), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(rawLine, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		key, value, found := strings.Cut(line, "=")
		if !found || !envKey.MatchString(strings.TrimSpace(key)) {
			return nil, fmt.Errorf("invalid environment entry in %s:%d", path, number+1)
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && value[0] == value[len(value)-1] && (value[0] == '\'' || value[0] == '"') {
			value = value[1 : len(value)-1]
		}
		values[strings.TrimSpace(key)] = value
	}
	return values, nil
}

// File resolves the caller's environment file: an explicit path, then
// GOPULSE_ENV_FILE, then the repository .env when it exists. A missing
// explicit file is an error; a missing default is not.
func File(root, explicit string, environ map[string]string) (string, error) {
	candidate := explicit
	if candidate == "" {
		candidate = environ["GOPULSE_ENV_FILE"]
	}
	if candidate != "" {
		path := candidate
		if strings.HasPrefix(path, "~") {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			path = filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
		if !filepath.IsAbs(path) {
			absolute, err := filepath.Abs(filepath.Join(root, path))
			if err != nil {
				return "", err
			}
			path = absolute
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			return "", fmt.Errorf("environment file does not exist: %s", path)
		}
		return path, nil
	}
	def := filepath.Join(root, ".env")
	if info, err := os.Stat(def); err == nil && !info.IsDir() {
		return def, nil
	}
	return "", nil
}

// Compose merges checked-in defaults, an optional caller file, and caller
// variables for one lifecycle mode.
func Compose(root, mode, explicit string, environ map[string]string) (Values, error) {
	defaults, err := Parse(filepath.Join(root, ".env.example"))
	if err != nil {
		return nil, err
	}
	values := Values{}
	for key, value := range defaults {
		values[key] = value
	}
	source, err := File(root, explicit, environ)
	if err != nil {
		return nil, err
	}
	caller := map[string]struct{}{}
	if source != "" {
		fileValues, err := Parse(source)
		if err != nil {
			return nil, err
		}
		for key, value := range fileValues {
			values[key] = value
			caller[key] = struct{}{}
		}
	}
	for key := range knownConfigKeys {
		if value, ok := environ[key]; ok {
			values[key] = value
			caller[key] = struct{}{}
		}
	}
	switch mode {
	case ModeDev:
		applyDefaults(values, devDefaults, caller)
	case ModeObserve:
		applyDefaults(values, observeDefaults, caller)
	case ModeTest:
		// Isolation boundary: force the test stack values over any caller value.
		for key, value := range testDefaults {
			values[key] = value
		}
	default:
		return nil, fmt.Errorf("unknown environment mode: %s", mode)
	}
	return values, nil
}

func applyDefaults(values Values, defaults map[string]string, caller map[string]struct{}) {
	for key, value := range defaults {
		if _, supplied := caller[key]; !supplied {
			values[key] = value
		}
	}
}

// WritePrivate writes the sorted, 0600 private environment file consumed by
// Compose and by every owned child process, and returns its path.
func WritePrivate(privateRoot, mode string, values Values) (string, error) {
	if err := os.MkdirAll(privateRoot, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(privateRoot, mode+".env")
	keys := make([]string, 0, len(values))
	for key := range values {
		if envKey.MatchString(key) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var builder strings.Builder
	for _, key := range keys {
		builder.WriteString(key)
		builder.WriteString("=")
		builder.WriteString(values[key])
		builder.WriteString("\n")
	}
	if err := os.WriteFile(path, []byte(builder.String()), 0o600); err != nil {
		return "", err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

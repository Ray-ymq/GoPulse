package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

type storageIdentity struct {
	device uint64
	inode  uint64
}

func ensureRoot(root string) (string, storageIdentity, error) {
	clean, err := validatePluginRoot(root)
	if err != nil {
		return "", storageIdentity{}, err
	}
	for _, dir := range []string{clean, filepath.Join(clean, ".staging")} {
		if err = rejectSymlinkComponents(dir); err != nil {
			return "", storageIdentity{}, err
		}
		if err = os.MkdirAll(dir, 0750); err != nil {
			return "", storageIdentity{}, err
		}
		if err = requireDirectory(dir); err != nil {
			return "", storageIdentity{}, err
		}
	}
	identity, err := pathIdentity(clean)
	return clean, identity, err
}

func validatePluginRoot(root string) (string, error) {
	if !filepath.IsAbs(root) {
		return "", errors.New("plugin root must be absolute")
	}
	clean := filepath.Clean(root)
	if clean == string(filepath.Separator) {
		return "", errors.New("plugin root must not be the filesystem root")
	}
	home, err := os.UserHomeDir()
	if err == nil && samePath(clean, home) {
		return "", errors.New("plugin root must not be the user home directory")
	}
	if repository := repositoryRoot(); repository != "" && samePath(clean, repository) {
		return "", errors.New("plugin root must not be the repository root")
	}
	if err = rejectSymlinkComponents(clean); err != nil {
		return "", err
	}
	return clean, nil
}

func samePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && filepath.Clean(leftAbs) == filepath.Clean(rightAbs)
}

func repositoryRoot() string {
	current, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		version, versionErr := os.Lstat(filepath.Join(current, "VERSION"))
		monitorModule, monitorErr := os.Lstat(filepath.Join(current, "monitor", "go.mod"))
		backendModule, backendErr := os.Lstat(filepath.Join(current, "backend", "go.mod"))
		if versionErr == nil && version.Mode().IsRegular() && monitorErr == nil && monitorModule.Mode().IsRegular() && backendErr == nil && backendModule.Mode().IsRegular() {
			return filepath.Clean(current)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}

func rejectSymlinkComponents(path string) error {
	clean := filepath.Clean(path)
	volume := filepath.VolumeName(clean)
	remainder := strings.TrimPrefix(clean, volume)
	current := volume + string(filepath.Separator)
	for _, component := range strings.Split(strings.TrimPrefix(remainder, string(filepath.Separator)), string(filepath.Separator)) {
		if component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("plugin storage path contains a symlink")
		}
	}
	return nil
}

func requireDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("plugin storage path is not a directory")
	}
	return nil
}

func pathIdentity(path string) (storageIdentity, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return storageIdentity{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return storageIdentity{}, errors.New("plugin root identity is invalid")
	}
	return storageIdentity{device: uint64(stat.Dev), inode: stat.Ino}, nil
}

func validateStorage(root string, expected storageIdentity) error {
	actual, err := pathIdentity(root)
	if err != nil || actual != expected {
		return errors.New("plugin root changed after initialization")
	}
	checks := []struct {
		path string
		dir  bool
	}{
		{filepath.Join(root, ".staging"), true},
		{filepath.Join(root, "registry.json"), false},
		{filepath.Join(root, PluginID), true},
		{filepath.Join(root, PluginID, "releases"), true},
		{filepath.Join(root, PluginID, "runtime"), true},
	}
	for _, check := range checks {
		info, statErr := os.Lstat(check.path)
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 || (check.dir && !info.IsDir()) || (!check.dir && !info.Mode().IsRegular()) {
			return errors.New("plugin storage node has an unexpected type")
		}
	}
	processPath := filepath.Join(root, PluginID, "runtime", "process.json")
	if info, statErr := os.Lstat(processPath); statErr == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
		return errors.New("plugin storage node has an unexpected type")
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	releasesPath := filepath.Join(root, PluginID, "releases")
	entries, readErr := os.ReadDir(releasesPath)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	for _, entry := range entries {
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("plugin release has an unexpected type")
		}
	}
	return nil
}
func loadRegistry(root string) (registryFile, error) {
	path := filepath.Join(root, "registry.json")
	info, statErr := os.Lstat(path)
	if statErr == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
		return registryFile{}, errors.New("registry has an unexpected file type")
	}
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return registryFile{}, statErr
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return registryFile{Plugins: map[string]registryEntry{}}, nil
	}
	if err != nil {
		return registryFile{}, err
	}
	var reg registryFile
	if err = json.Unmarshal(data, &reg); err != nil || reg.Plugins == nil {
		return registryFile{}, errors.New("registry is invalid")
	}
	return reg, nil
}
func saveRegistry(root string, reg registryFile) error {
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWrite(filepath.Join(root, "registry.json"), data, 0640)
}
func atomicWrite(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	ok := false
	defer func() {
		tmp.Close()
		if !ok {
			os.Remove(name)
		}
	}()
	if err = tmp.Chmod(mode); err != nil {
		return err
	}
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	if err = syncDir(dir); err != nil {
		return err
	}
	ok = true
	return nil
}
func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
func switchCurrent(pluginDir, version string) error {
	tmp := filepath.Join(pluginDir, fmt.Sprintf(".current-%d", os.Getpid()))
	os.Remove(tmp)
	if err := os.Symlink(filepath.Join("releases", version), tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(pluginDir, "current")); err != nil {
		os.Remove(tmp)
		return err
	}
	return syncDir(pluginDir)
}
func readCurrent(pluginDir string) (string, error) {
	target, err := os.Readlink(filepath.Join(pluginDir, "current"))
	if err != nil {
		return "", err
	}
	clean := filepath.Clean(target)
	prefix := "releases" + string(os.PathSeparator)
	if filepath.IsAbs(clean) || !strings.HasPrefix(clean, prefix) || filepath.Base(clean) == "." {
		return "", errors.New("invalid current link")
	}
	return filepath.Base(clean), nil
}

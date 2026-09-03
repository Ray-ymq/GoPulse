package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ensureRoot(root string) error {
	for _, dir := range []string{root, filepath.Join(root, ".staging")} {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return err
		}
	}
	return nil
}
func loadRegistry(root string) (registryFile, error) {
	path := filepath.Join(root, "registry.json")
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

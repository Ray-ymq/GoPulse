package migrations

import (
	"bytes"
	"fmt"
	"io/fs"
	"regexp"
	"strconv"
)

// Target validates the embedded schema inventory and returns its last version.
func Target() (uint, error) { return validateInventory(files) }

func validateInventory(files fs.FS) (uint, error) {
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return 0, err
	}
	pattern := regexp.MustCompile(`^([0-9]{6})_(.+)\.(up|down)\.sql$`)
	pairs := map[int]map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		match := pattern.FindStringSubmatch(entry.Name())
		if match == nil {
			return 0, fmt.Errorf("invalid migration filename")
		}
		version, _ := strconv.Atoi(match[1])
		if version < 1 {
			return 0, fmt.Errorf("invalid migration version")
		}
		if pairs[version] == nil {
			pairs[version] = map[string]string{}
		}
		if pairs[version][match[3]] != "" {
			return 0, fmt.Errorf("duplicate migration version")
		}
		pairs[version][match[3]] = match[2]
		data, err := fs.ReadFile(files, entry.Name())
		if err != nil || len(bytes.TrimSpace(data)) == 0 {
			return 0, fmt.Errorf("unreadable or empty migration")
		}
	}
	if len(pairs) == 0 {
		return 0, fmt.Errorf("empty migration inventory")
	}
	for version := 1; version <= len(pairs); version++ {
		pair := pairs[version]
		if pair["up"] == "" || pair["up"] != pair["down"] {
			return 0, fmt.Errorf("missing or mismatched migration pair")
		}
	}
	return uint(len(pairs)), nil
}

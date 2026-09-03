package plugin

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

var semverPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func ParseManifest(data []byte) (Manifest, error) {
	if len(data) == 0 || len(data) > 64<<10 {
		return Manifest{}, NewError(CodePackageInvalid, "plugin package manifest is invalid")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	token, err := dec.Token()
	if err != nil || token != json.Delim('{') {
		return Manifest{}, NewError(CodePackageInvalid, "plugin package manifest is invalid")
	}
	allowed := map[string]bool{"schema_version": true, "id": true, "name": true, "version": true, "kind": true, "source": true, "os": true, "arch": true, "entrypoint": true, "entrypoint_sha256": true, "health_path": true, "metrics_path": true}
	seen := map[string]bool{}
	values := map[string]json.RawMessage{}
	for dec.More() {
		keyToken, e := dec.Token()
		if e != nil {
			return Manifest{}, NewError(CodePackageInvalid, "plugin package manifest is invalid")
		}
		key, ok := keyToken.(string)
		if !ok || !allowed[key] || seen[key] {
			return Manifest{}, NewError(CodePackageInvalid, "plugin package manifest is invalid")
		}
		seen[key] = true
		var raw json.RawMessage
		if e = dec.Decode(&raw); e != nil {
			return Manifest{}, NewError(CodePackageInvalid, "plugin package manifest is invalid")
		}
		values[key] = raw
	}
	if _, err = dec.Token(); err != nil {
		return Manifest{}, NewError(CodePackageInvalid, "plugin package manifest is invalid")
	}
	if _, err = dec.Token(); !errors.Is(err, io.EOF) {
		return Manifest{}, NewError(CodePackageInvalid, "plugin package manifest is invalid")
	}
	if len(seen) != len(allowed) {
		return Manifest{}, NewError(CodePackageInvalid, "plugin package manifest is invalid")
	}
	canonical, _ := json.Marshal(values)
	var manifest Manifest
	strict := json.NewDecoder(bytes.NewReader(canonical))
	strict.DisallowUnknownFields()
	if strict.Decode(&manifest) != nil {
		return Manifest{}, NewError(CodePackageInvalid, "plugin package manifest is invalid")
	}
	if manifest.SchemaVersion != 1 || manifest.ID != PluginID || strings.TrimSpace(manifest.Name) == "" || len(manifest.Name) > 80 || !semverPattern.MatchString(manifest.Version) || manifest.Kind != "metrics-exporter" || manifest.Source != "redis" || manifest.OS != "linux" || manifest.Arch != runtime.GOARCH || manifest.Entrypoint != "bin/gopulse-redis-exporter" || !digestPattern.MatchString(manifest.EntrypointSHA256) || manifest.HealthPath != "/health" || manifest.MetricsPath != "/metrics" {
		return Manifest{}, NewError(CodePackageInvalid, "plugin package manifest is invalid")
	}
	if _, err := hex.DecodeString(manifest.EntrypointSHA256); err != nil {
		return Manifest{}, NewError(CodePackageInvalid, "plugin package manifest is invalid")
	}
	return manifest, nil
}
func CompareSemver(a, b string) (int, error) {
	pa, e := parseSemver(a)
	if e != nil {
		return 0, e
	}
	pb, e := parseSemver(b)
	if e != nil {
		return 0, e
	}
	for i := range pa {
		if pa[i] < pb[i] {
			return -1, nil
		}
		if pa[i] > pb[i] {
			return 1, nil
		}
	}
	return 0, nil
}
func parseSemver(v string) ([3]uint64, error) {
	var out [3]uint64
	if !semverPattern.MatchString(v) {
		return out, fmt.Errorf("invalid semver")
	}
	for i, p := range strings.Split(v, ".") {
		n, e := strconv.ParseUint(p, 10, 64)
		if e != nil {
			return out, e
		}
		out[i] = n
	}
	return out, nil
}

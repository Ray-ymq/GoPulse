// Package release defines the immutable release contract shared by all hosts.
package release

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var platforms = []string{"linux/amd64", "linux/arm64"}
var products = []string{"backend", "business-worker", "search-indexer", "frontend", "admin-frontend", "router", "marshaller", "monitor", "redis-exporter"}
var sources = []string{"redis", "mysql", "rabbitmq", "kafka", "elasticsearch", "victoriametrics"}
var digest = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var revision = regexp.MustCompile(`^[0-9a-f]{40}$`)
var semver = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
var immutable = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:/-]*@sha256:[0-9a-f]{64}$`)

type Image struct {
	Ref       string            `json:"ref"`
	Platforms map[string]string `json:"platforms"`
}
type Asset struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type Plugin struct {
	ID         string `json:"id"`
	Version    string `json:"version"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	Purpose    string `json:"purpose"`
	Archive    string `json:"archive_sha256"`
	Entrypoint string `json:"entrypoint_sha256"`
	Schema     string `json:"schema_sha256"`
}

// BundleSHA256 hashes canonical bundle payload entries, excluding the manifest
// and checksums. The final archive SHA256 lives in a detached checksums file,
// avoiding an impossible archive/embedded-manifest self-reference.
type Manifest struct {
	SchemaVersion           int              `json:"schema_version"`
	Version                 string           `json:"version"`
	Revision                string           `json:"revision"`
	Compose                 Asset            `json:"compose"`
	BundleSHA256            string           `json:"bundle_sha256"`
	Images                  map[string]Image `json:"images"`
	ThirdParty              map[string]Image `json:"third_party"`
	Lifecycle               Image            `json:"lifecycle"`
	Plugins                 []Plugin         `json:"plugins"`
	SupportedUpgradeSources []string         `json:"supported_upgrade_sources"`
}

// Check JSON tokens first: encoding/json alone silently accepts duplicate keys.
func unique(dec *json.Decoder) error {
	token, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			key, err := dec.Token()
			if err != nil {
				return err
			}
			s, ok := key.(string)
			if !ok || seen[s] {
				return errors.New("duplicate JSON identity")
			}
			seen[s] = true
			if err = unique(dec); err != nil {
				return err
			}
		}
	case '[':
		for dec.More() {
			if err := unique(dec); err != nil {
				return err
			}
		}
	default:
		return errors.New("invalid JSON delimiter")
	}
	_, err = dec.Token()
	return err
}
func Parse(data []byte) (*Manifest, error) {
	if len(data) > 4<<20 {
		return nil, errors.New("manifest too large")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := unique(dec); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("trailing JSON")
	}
	dec = json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}
func checkImage(i Image) error {
	if !immutable.MatchString(i.Ref) || strings.Contains(strings.Split(i.Ref, "@")[0], ":latest") || len(i.Platforms) != 2 {
		return errors.New("image must have immutable index and exactly two platforms")
	}
	for _, p := range platforms {
		if !digest.MatchString(i.Platforms[p]) {
			return errors.New("missing platform digest")
		}
	}
	if i.Platforms[platforms[0]] == i.Platforms[platforms[1]] {
		return errors.New("platform digests must differ")
	}
	return nil
}
func (m *Manifest) Validate() error {
	if m.SchemaVersion != 1 || !semver.MatchString(m.Version) || !revision.MatchString(m.Revision) || !digest.MatchString(m.BundleSHA256) || m.Compose.Path != "deploy/product/compose.yaml" || !digest.MatchString(m.Compose.SHA256) {
		return errors.New("invalid release identity or asset")
	}
	if len(m.Images) != 9 || len(m.ThirdParty) != 6 {
		return errors.New("incomplete image set")
	}
	for _, name := range products {
		if err := checkImage(m.Images[name]); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	for _, name := range sources {
		if err := checkImage(m.ThirdParty[name]); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if err := checkImage(m.Lifecycle); err != nil {
		return err
	}
	seen := map[string]bool{}
	current := map[string]bool{}
	legacy := false
	for _, p := range m.Plugins {
		key := p.ID + "/" + p.Version + "/" + p.Arch
		if seen[key] || p.OS != "linux" || !digest.MatchString(p.Archive) || !digest.MatchString(p.Entrypoint) {
			return errors.New("invalid or duplicate plugin")
		}
		seen[key] = true
		switch p.Purpose {
		case "current":
			if p.Version != m.Version || (p.Arch != "amd64" && p.Arch != "arm64") || !digest.MatchString(p.Schema) {
				return errors.New("invalid current plugin")
			}
			current[p.ID+"/"+p.Arch] = true
		case "upgrade-only":
			if legacy || p.ID != "redis-exporter" || p.Version != "1.9.4" || p.Arch != "amd64" || p.Schema != "" {
				return errors.New("invalid legacy input")
			}
			legacy = true
		default:
			return errors.New("unknown plugin purpose")
		}
	}
	if len(m.Plugins) != 13 || len(current) != 12 || !legacy {
		return errors.New("incomplete plugin catalog")
	}
	for _, s := range sources {
		for _, a := range []string{"amd64", "arm64"} {
			if !current[s+"-exporter/"+a] {
				return errors.New("missing plugin platform")
			}
		}
	}
	// Phase 16-01 registers upgrade artifacts, not a working upgrade command.
	if m.SupportedUpgradeSources == nil || len(m.SupportedUpgradeSources) != 0 {
		return errors.New("upgrade not yet accepted")
	}
	return nil
}
func Sum(data []byte) string { h := sha256.Sum256(data); return "sha256:" + hex.EncodeToString(h[:]) }
func (m *Manifest) CheckAssets(root string) error {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(m.Compose.Path)))
	if err != nil {
		return err
	}
	if Sum(data) != m.Compose.SHA256 {
		return errors.New("compose checksum mismatch")
	}
	return nil
}
func (m *Manifest) CheckTool(version, rev, toolArch, serverOS, serverArch string) error {
	if version != m.Version || rev != m.Revision {
		return errors.New("tool and manifest version/revision mismatch")
	}
	if serverOS != "linux" || serverArch != toolArch || (serverArch != "amd64" && serverArch != "arm64") {
		return errors.New("Docker server platform does not match lifecycle image")
	}
	return nil
}

package packaging

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Ray-ymq/GoPulse/lifecycle/internal/release"
)

var errLocalImageMissing = errors.New("local image missing")

// verificationReceipt is the immutable receipt record a promotion gate reads.
// The field order matches the receipts already stored with earlier candidates.
type verificationReceipt struct {
	ManifestSHA256 string `json:"manifest_sha256"`
	Revision       string `json:"revision"`
	Platform       string `json:"platform"`
	Status         string `json:"status"`
}

// Verify inspects the candidate against its registry digests and writes the
// receipt the promotion gate requires. Foreign platforms stay metadata-only.
func Verify(manifestPath, platform string, runtimeGate bool) error {
	path, err := filepath.Abs(manifestPath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		return usagef("release manifest is missing: %s", path)
	}
	m, err := VerifyBundle(path)
	if err != nil {
		return err
	}
	for _, name := range append(append([]string{}, Products...), "lifecycle") {
		image := m.Lifecycle
		if name != "lifecycle" {
			image = m.Images[name]
		}
		if err := InspectImage(image, platform, m.Version, m.Revision, true); err != nil {
			return fmt.Errorf("%s %s: %w", name, platform, err)
		}
		fmt.Printf("PASS metadata: %s %s\n", name, platform)
	}
	for _, name := range Sources {
		if err := InspectImage(m.ThirdParty[name], platform, m.Version, m.Revision, false); err != nil {
			return fmt.Errorf("%s %s: %w", name, platform, err)
		}
		fmt.Printf("PASS third-party metadata: %s %s\n", name, platform)
	}
	arch := platformArch(platform)
	records, err := PluginRecords(m.Images["monitor"], platform, filepath.Join(filepath.Dir(path), "verified-plugins", arch))
	if err != nil {
		return err
	}
	expected := make([]release.Plugin, 0, len(m.Plugins))
	for _, plugin := range m.Plugins {
		if plugin.Arch == arch {
			expected = append(expected, plugin)
		}
	}
	byIdentity := func(plugins []release.Plugin) {
		sort.Slice(plugins, func(i, j int) bool {
			if plugins[i].ID != plugins[j].ID {
				return plugins[i].ID < plugins[j].ID
			}
			return plugins[i].Version < plugins[j].Version
		})
	}
	byIdentity(records)
	byIdentity(expected)
	if len(records) != len(expected) {
		return errors.New("Monitor plugin catalog differs from manifest")
	}
	for i := range records {
		if records[i] != expected[i] {
			return errors.New("Monitor plugin catalog differs from manifest")
		}
	}
	status := "metadata-only; real arm64 runtime DEFERRED to Phase-16-06"
	if runtimeGate {
		if err := runtimeVerify(m, platform); err != nil {
			return err
		}
		status = "amd64-runtime-passed"
	}
	manifest, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	encoded, err := encodeJSON(verificationReceipt{
		ManifestSHA256: Sum(manifest),
		Revision:       m.Revision,
		Platform:       platform,
		Status:         status,
	})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "verification-"+arch+".json"), encoded, 0o644); err != nil {
		return err
	}
	fmt.Println(status)
	return nil
}

// runtimeVerify proves the lifecycle candidate runs on this host before any
// promotion. The tool image starts read-only and reports its bound identity.
func runtimeVerify(m *release.Manifest, platform string) error {
	serverJSON, err := output("", "docker", "version", "--format", "{{json .Server}}")
	if err != nil {
		return err
	}
	var server struct {
		OS   string `json:"Os"`
		Arch string `json:"Arch"`
	}
	if json.Unmarshal([]byte(serverJSON), &server) != nil {
		return errors.New("invalid Docker server version")
	}
	if platform != "linux/amd64" || server.OS+"/"+server.Arch != platform {
		return errors.New("runtime gate requires real matching Linux amd64 server")
	}
	ref, err := PlatformRef(m.Lifecycle, platform)
	if err != nil {
		return err
	}
	if err := ensureRuntimeImage(ref, platform); err != nil {
		return err
	}
	probe, err := output("", "docker", "run", "--rm", "--read-only", "--network", "none", "--cap-drop", "ALL", ref, "version", "--json")
	if err != nil {
		return err
	}
	var result struct {
		Version  string `json:"version"`
		Revision string `json:"revision"`
		Platform string `json:"platform"`
	}
	if json.Unmarshal([]byte(probe), &result) != nil {
		return errors.New("invalid lifecycle runtime identity")
	}
	if result.Version != m.Version || result.Revision != m.Revision || result.Platform != platform {
		return errors.New("lifecycle runtime mismatch")
	}
	return nil
}

// ensureRuntimeImage reuses an immutable platform image already in the local
// image store. Only an explicit Docker "no such image" result permits a pull;
// successful inspection with a mismatched platform or digest is a hard error.
// A pulled image is inspected again before it can reach the runtime probe.
func ensureRuntimeImage(ref, platform string) error {
	matched, err := inspectRuntimeImage(ref, platform)
	if err != nil {
		return err
	}
	if matched {
		fmt.Printf("PASS local image: %s\n", ref)
		return nil
	}
	if err := attached("", "docker", "pull", ref); err != nil {
		return err
	}
	matched, err = inspectRuntimeImage(ref, platform)
	if err != nil {
		return fmt.Errorf("pulled image verification failed: %w", err)
	}
	if !matched {
		return fmt.Errorf("pulled image %s is still missing", ref)
	}
	fmt.Printf("PASS pulled image: %s\n", ref)
	return nil
}

// inspectRuntimeImage returns matched=false only for an explicit local image
// miss. Every other inspect failure, including malformed metadata, is fatal so
// a bad local image cannot be silently replaced by a pull.
func inspectRuntimeImage(ref, platform string) (bool, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("docker", "image", "inspect", "--platform", platform, "--format", "{{json .}}", ref)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.ToLower(stderr.String())
		if strings.Contains(message, "no such image") || strings.Contains(message, "no such object") {
			return false, nil
		}
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			return false, fmt.Errorf("cannot inspect local image %s: %w", ref, err)
		}
		return false, fmt.Errorf("cannot inspect local image %s: %s", ref, detail)
	}
	var image struct {
		OS           string   `json:"Os"`
		Architecture string   `json:"Architecture"`
		RepoDigests  []string `json:"RepoDigests"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &image); err != nil {
		return false, fmt.Errorf("invalid local image inspection for %s: %w", ref, err)
	}
	actualPlatform := image.OS + "/" + image.Architecture
	if actualPlatform != platform {
		return false, fmt.Errorf("local image platform mismatch: got %s, want %s", actualPlatform, platform)
	}
	want := normalizeRepoDigest(ref)
	for _, digest := range image.RepoDigests {
		if normalizeRepoDigest(digest) == want {
			return true, nil
		}
	}
	return false, fmt.Errorf("local image RepoDigest mismatch: want %s", ref)
}

// inspectLocalThirdPartyImage verifies a locked source image without asking a
// registry for its manifest. Docker exposes the exact platform manifest digest
// through RepoDigests and the platform/configuration through image inspect;
// that is sufficient to bind the local image to the third-party lock while
// keeping an offline candidate verification genuinely offline.
func inspectLocalThirdPartyImage(image release.Image, platform string) error {
	ref, err := PlatformRef(image, platform)
	if err != nil {
		return err
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("docker", "image", "inspect", "--platform", platform, "--format", "{{json .}}", ref)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.ToLower(stderr.String())
		if strings.Contains(message, "no such image") || strings.Contains(message, "no such object") {
			return fmt.Errorf("%w: %s", errLocalImageMissing, ref)
		}
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			return fmt.Errorf("cannot inspect local third-party image %s: %w", ref, err)
		}
		return fmt.Errorf("cannot inspect local third-party image %s: %s", ref, detail)
	}
	var inspected struct {
		OS           string   `json:"Os"`
		Architecture string   `json:"Architecture"`
		RepoDigests  []string `json:"RepoDigests"`
		RootFS       struct {
			Type   string   `json:"Type"`
			Layers []string `json:"Layers"`
		} `json:"RootFS"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &inspected); err != nil {
		return fmt.Errorf("invalid local third-party image inspection for %s: %w", ref, err)
	}
	if inspected.OS+"/"+inspected.Architecture != platform {
		return fmt.Errorf("local third-party image platform mismatch: got %s/%s, want %s", inspected.OS, inspected.Architecture, platform)
	}
	want := normalizeRepoDigest(ref)
	matched := false
	for _, digest := range inspected.RepoDigests {
		if normalizeRepoDigest(digest) == want {
			matched = true
			break
		}
	}
	if !matched {
		return fmt.Errorf("local third-party image RepoDigest mismatch: want %s", ref)
	}
	if inspected.RootFS.Type != "layers" || len(inspected.RootFS.Layers) == 0 {
		return fmt.Errorf("local third-party image has no layer metadata: %s", ref)
	}
	for _, layer := range inspected.RootFS.Layers {
		if !digestValue.MatchString(layer) {
			return fmt.Errorf("local third-party image has invalid layer metadata: %s", ref)
		}
	}
	return nil
}

// normalizeRepoDigest removes only the tag from a repository reference. The
// colon in a registry host such as 127.0.0.1:15001 is before the last slash
// and therefore remains part of the repository identity.
func normalizeRepoDigest(ref string) string {
	at := strings.LastIndexByte(ref, '@')
	if at < 0 {
		return ref
	}
	repository := ref[:at]
	if colon := strings.LastIndexByte(repository, ':'); colon > strings.LastIndexByte(repository, '/') {
		repository = repository[:colon]
	}
	return repository + ref[at:]
}

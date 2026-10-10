package packaging

import (
	"bufio"
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

// composeGateMarker is printed by the native acceptance closure on any
// acceptance error; a runner that reports an error but exits zero is still
// blocking by itself.
const composeGateMarker = "[gopulse-acceptance] ERROR:"

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
func Verify(repo *Repo, manifestPath, platform string, runtimeGate bool) error {
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
		if err := runtimeVerify(repo, m, path, platform); err != nil {
			return err
		}
		status = "amd64-runtime-and-compose-passed"
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

// runtimeVerify proves the candidate runs on this host before any promotion:
// the tool image starts read-only and the fixed Compose closure consumes the
// candidate platform digests exactly once.
func runtimeVerify(repo *Repo, m *release.Manifest, path, platform string) error {
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
	if err := attached("", "docker", "pull", ref); err != nil {
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
	// The full Compose acceptance runs ONCE against the candidate digests.
	return runComposeGate(repo, path)
}

// runComposeGate builds the native acceptance command when needed, then
// streams the fixed candidate closure and fails on a reported acceptance
// error even when the runner exits successfully.
func runComposeGate(repo *Repo, manifest string) error {
	commandPath := filepath.Join(repo.Root, "acceptance", "bin", "gopulse-acceptance")
	removeCommand := false
	if _, err := os.Stat(commandPath); err != nil {
		temporary, err := os.CreateTemp("", "gopulse-acceptance-")
		if err != nil {
			return err
		}
		commandPath = temporary.Name()
		if err := temporary.Close(); err != nil {
			os.Remove(commandPath)
			return err
		}
		removeCommand = true
		defer os.Remove(commandPath)
		build := exec.Command("go", "build", "-o", commandPath, "./cmd/gopulse-acceptance")
		build.Dir = filepath.Join(repo.Root, "acceptance")
		build.Stdout = os.Stdout
		build.Stderr = os.Stderr
		if err := build.Run(); err != nil {
			return fmt.Errorf("build native acceptance command: %w", err)
		}
	}
	if removeCommand {
		_ = os.Chmod(commandPath, 0o700)
	}
	for _, args := range [][]string{
		{"compose", "--scope", "observability", "--manifest", manifest},
		{"lifecycle", "--install", "clean", "--manifest", manifest, "--platform", "linux/amd64"},
		{"lifecycle", "--install", "clean", "--failure-matrix", "--manifest", manifest, "--platform", "linux/amd64"},
	} {
		if err := runNativeAcceptance(commandPath, repo.Root, args...); err != nil {
			return err
		}
	}
	return nil
}

func runNativeAcceptance(commandPath, root string, args ...string) error {
	cmd := exec.Command(commandPath, args...)
	cmd.Dir = root
	cmd.Env = os.Environ()
	stream, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return err
	}
	reported := false
	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Println(line)
		if strings.Contains(line, composeGateMarker) {
			reported = true
		}
	}
	if err := scanner.Err(); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	if err := cmd.Wait(); err != nil || reported {
		return errors.New("native acceptance reported a failure; no success receipt emitted")
	}
	return nil
}

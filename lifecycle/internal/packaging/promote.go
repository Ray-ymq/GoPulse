package packaging

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Ray-ymq/GoPulse/lifecycle/internal/release"
)

// receiptStatus is the closed status vocabulary of the verification receipts.
func receiptStatus(arch string) string {
	if arch == "amd64" {
		return "amd64-runtime-and-compose-passed"
	}
	return "metadata-only; real arm64 runtime DEFERRED to Phase-16-06"
}

// Promote copies the exact verified indexes to their release tags. It refuses
// to run without matching receipts and never rebuilds or reserializes an image.
func Promote(manifestPath string) error {
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
	manifest, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	architectures := make([]string, 0, len(m.Lifecycle.Platforms))
	for platform := range m.Lifecycle.Platforms {
		architectures = append(architectures, platformArch(platform))
	}
	sort.Strings(architectures)
	for _, arch := range architectures {
		receipt, err := os.ReadFile(filepath.Join(filepath.Dir(path), "verification-"+arch+".json"))
		if err != nil {
			return errors.New("candidate lacks matching successful verification receipts")
		}
		expected, err := encodeJSON(verificationReceipt{
			ManifestSHA256: Sum(manifest),
			Revision:       m.Revision,
			Platform:       "linux/" + arch,
			Status:         receiptStatus(arch),
		})
		if err != nil {
			return err
		}
		if !bytes.Equal(receipt, expected) {
			return errors.New("candidate lacks matching successful verification receipts")
		}
	}
	for _, name := range append(append([]string{}, Products...), "lifecycle") {
		image := m.Lifecycle
		if name != "lifecycle" {
			image = m.Images[name]
		}
		if err := promoteImage(image, m.Version); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	fmt.Println("Same-digest promotion verified; bundle checksum unchanged.")
	return nil
}

// releaseTarget derives the release tag of one immutable reference. Only the
// tag is replaced: a registry host with a port keeps its own colon.
func releaseTarget(ref, version string) string {
	repository := strings.Split(ref, "@")[0]
	if index := strings.LastIndexByte(repository, ':'); index >= 0 {
		repository = repository[:index]
	}
	return repository + ":" + version
}

func promoteImage(image release.Image, version string) error {
	ref := image.Ref
	target := releaseTarget(ref, version)
	cmd := exec.Command("docker", "buildx", "imagetools", "inspect", "--raw", target)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	switch err := cmd.Run(); {
	case err == nil:
		if Sum(stdout.Bytes()) != strings.Split(ref, "@")[1] {
			return errors.New("refusing to replace an existing version with different content")
		}
	case !strings.Contains(strings.ToLower(stderr.String()), "not found"):
		return errors.New("cannot establish whether promotion target exists")
	}
	// Copy the exact index, not a rebuilt or reserialized single-arch image.
	if err := attached("", "docker", "buildx", "imagetools", "create", "--prefer-index=true", "-t", target, ref); err != nil {
		return err
	}
	promoted, err := rawReference(target)
	if err != nil {
		return err
	}
	if Sum(promoted) != strings.Split(ref, "@")[1] {
		return errors.New("promotion changed index digest")
	}
	return nil
}

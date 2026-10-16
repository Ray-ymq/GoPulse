package packaging

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Ray-ymq/GoPulse/lifecycle/internal/release"
)

// receiptStatus is the closed status vocabulary of the verification receipts.
func receiptStatus(arch string) string {
	if arch == "amd64" {
		return "amd64-runtime-passed"
	}
	return "metadata-only; real arm64 runtime DEFERRED to Phase-16-06"
}

// Promote confirms the exact verified indexes under latest. It refuses to run
// without matching receipts and never rebuilds or reserializes an image.
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
		if err := promoteImage(image); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	fmt.Println("Same-digest promotion verified; bundle checksum unchanged.")
	return nil
}

// releaseTarget derives the latest tag of one immutable reference. Only the
// image tag is replaced: a registry host with a port keeps its own colon.
func releaseTarget(ref string) string {
	repository := strings.Split(ref, "@")[0]
	if index := strings.LastIndexByte(repository, ':'); index > strings.LastIndexByte(repository, '/') {
		repository = repository[:index]
	}
	return repository + ":latest"
}

func promoteImage(image release.Image) error {
	// Build already published latest. Confirmation must not write the tag
	// again, since doing so could replace another build published meanwhile.
	published, err := rawReference(releaseTarget(image.Ref))
	if err != nil {
		return err
	}
	if Sum(published) != strings.Split(image.Ref, "@")[1] {
		return errors.New("latest changed after candidate build; refusing to replace different content")
	}
	return nil
}

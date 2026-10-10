package scenario

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Ray-ymq/GoPulse/acceptance/internal/harness"
)

type candidateManifest struct {
	Version    string                    `json:"version"`
	Revision   string                    `json:"revision"`
	Images     map[string]candidateImage `json:"images"`
	ThirdParty map[string]candidateImage `json:"third_party"`
}

type candidateImage struct {
	Ref       string            `json:"ref"`
	Platforms map[string]string `json:"platforms"`
}

// configureCandidate validates and exposes the immutable platform references
// through the same Compose environment variables used by the source stack.
// Pulling is explicit because the product Compose contract intentionally uses
// pull_policy=never for local development.
func configureCandidate(session *harness.Session, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read candidate manifest: %w", err)
	}
	var manifest candidateManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("parse candidate manifest: %w", err)
	}
	if manifest.Version != session.Version || manifest.Revision != session.Revision {
		return fmt.Errorf("candidate identity does not match VERSION and HEAD")
	}
	images := make(map[string]string, len(manifest.Images)+len(manifest.ThirdParty))
	for name, image := range manifest.Images {
		images[name] = platformReference(image)
	}
	for name, image := range manifest.ThirdParty {
		images[name] = platformReference(image)
	}
	for name, ref := range images {
		if ref == "" {
			return fmt.Errorf("candidate image %s has no linux/amd64 reference", name)
		}
		key := "GOPULSE_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_IMAGE"
		session.SetValue(key, ref)
		if result := session.Run("docker", "pull", ref); result.ExitCode != 0 {
			return fmt.Errorf("pull candidate image %s failed with exit code %d", name, result.ExitCode)
		}
	}
	return nil
}

func platformReference(image candidateImage) string {
	digest := image.Platforms["linux/amd64"]
	if image.Ref == "" || digest == "" {
		return ""
	}
	base := strings.SplitN(image.Ref, "@", 2)[0]
	return base + "@" + digest
}

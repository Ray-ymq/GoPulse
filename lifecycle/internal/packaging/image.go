package packaging

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Ray-ymq/GoPulse/lifecycle/internal/release"
)

// rawIndex is the registry index document behind one candidate tag.
type rawIndex struct {
	Manifests []struct {
		Digest   string `json:"digest"`
		Platform struct {
			OS           string `json:"os"`
			Architecture string `json:"architecture"`
		} `json:"platform"`
	} `json:"manifests"`
}

// imageRecord derives the immutable reference and platform digests of one
// candidate tag from the raw index bytes the registry returned.
func imageRecord(ref string, data []byte) (release.Image, error) {
	var index rawIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return release.Image{}, errors.New("invalid registry index document")
	}
	platforms := map[string]string{}
	supported := map[string]bool{"linux/amd64": true, "linux/arm64": true}
	for _, item := range index.Manifests {
		platform := item.Platform.OS + "/" + item.Platform.Architecture
		if !supported[platform] {
			continue
		}
		if _, seen := platforms[platform]; seen {
			return release.Image{}, errors.New("duplicate image platform")
		}
		platforms[platform] = item.Digest
	}
	if platforms["linux/amd64"] == "" {
		return release.Image{}, errors.New("missing image platform")
	}
	return release.Image{Ref: strings.Split(ref, "@")[0] + "@" + Sum(data), Platforms: platforms}, nil
}

func recordImage(ref string) (release.Image, error) {
	data, err := rawReference(ref)
	if err != nil {
		return release.Image{}, err
	}
	return imageRecord(ref, data)
}

func rawReference(ref string) ([]byte, error) {
	out, err := output("", "docker", "buildx", "imagetools", "inspect", "--raw", ref)
	if err != nil {
		return nil, fmt.Errorf("cannot inspect %s", ref)
	}
	return []byte(out), nil
}

// PlatformRef resolves one platform digest from an immutable index reference.
func PlatformRef(image release.Image, platform string) (string, error) {
	digest, ok := image.Platforms[platform]
	if !ok {
		return "", fmt.Errorf("image %s has no %s manifest", image.Ref, platform)
	}
	return strings.Split(image.Ref, "@")[0] + "@" + digest, nil
}

// imageDocument is the platform manifest the registry stores.
type imageDocument struct {
	Config struct {
		Digest string `json:"digest"`
	} `json:"config"`
	Layers []struct {
		Digest string `json:"digest"`
		Size   int64  `json:"size"`
	} `json:"layers"`
}

// imageConfig is the OCI platform configuration behind one manifest.
type imageConfig struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	RootFS       struct {
		DiffIDs []string `json:"diff_ids"`
	} `json:"rootfs"`
	Config struct {
		User       string            `json:"User"`
		Entrypoint []string          `json:"Entrypoint"`
		Labels     map[string]string `json:"Labels"`
	} `json:"config"`
}

// checkImageDocument rejects layer and configuration metadata that cannot
// describe an immutable product artifact.
func checkImageDocument(document, config []byte, platform string) error {
	var manifest imageDocument
	if err := json.Unmarshal(document, &manifest); err != nil {
		return errors.New("invalid platform manifest")
	}
	if len(manifest.Layers) == 0 || !digestValue.MatchString(manifest.Config.Digest) {
		return errors.New("missing config/layers")
	}
	for _, layer := range manifest.Layers {
		if layer.Size <= 0 || !digestValue.MatchString(layer.Digest) {
			return errors.New("invalid layer metadata")
		}
	}
	var parsed imageConfig
	if err := json.Unmarshal(config, &parsed); err != nil {
		return errors.New("invalid image configuration")
	}
	if parsed.OS+"/"+parsed.Architecture != platform || len(parsed.RootFS.DiffIDs) != len(manifest.Layers) {
		return errors.New("config platform/layers mismatch")
	}
	return nil
}

// checkProductLabels enforces the OCI identity every product image must carry.
func checkProductLabels(config imageConfig, version, revision string) error {
	for key, expected := range map[string]string{
		"org.opencontainers.image.version":  version,
		"org.opencontainers.image.revision": revision,
		"org.opencontainers.image.source":   "https://github.com/Ray-ymq/GoPulse",
	} {
		if config.Config.Labels[key] != expected {
			return fmt.Errorf("OCI %s mismatch", strings.TrimPrefix(key, "org.opencontainers.image."))
		}
	}
	if config.Config.Labels["org.opencontainers.image.title"] == "" || !numericUser.MatchString(config.Config.User) || len(config.Config.Entrypoint) == 0 {
		return errors.New("missing numeric user/title/entrypoint")
	}
	return nil
}

// InspectImage re-derives the manifest record from the registry and checks it
// has not drifted, including the OCI identity of product images.
func InspectImage(image release.Image, platform, version, revision string, product bool) error {
	index, err := rawReference(image.Ref)
	if err != nil {
		return err
	}
	if Sum(index) != strings.Split(image.Ref, "@")[1] {
		return errors.New("index digest mismatch")
	}
	selected, err := imageRecord(image.Ref, index)
	if err != nil {
		return err
	}
	if len(selected.Platforms) != len(image.Platforms) {
		return errors.New("index platform digest mismatch")
	}
	for name, digest := range selected.Platforms {
		if image.Platforms[name] != digest {
			return errors.New("index platform digest mismatch")
		}
	}
	ref, err := PlatformRef(image, platform)
	if err != nil {
		return err
	}
	document, err := rawReference(ref)
	if err != nil {
		return err
	}
	if Sum(document) != image.Platforms[platform] {
		return errors.New("platform manifest digest mismatch")
	}
	configJSON, err := output("", "docker", "buildx", "imagetools", "inspect", "--format", "{{json .Image}}", ref)
	if err != nil {
		return fmt.Errorf("cannot read the image configuration of %s", ref)
	}
	if err := checkImageDocument(document, []byte(configJSON), platform); err != nil {
		return err
	}
	if product {
		var parsed imageConfig
		if err := json.Unmarshal([]byte(configJSON), &parsed); err != nil {
			return errors.New("invalid image configuration")
		}
		return checkProductLabels(parsed, version, revision)
	}
	return nil
}

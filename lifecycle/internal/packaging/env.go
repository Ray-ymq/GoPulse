package packaging

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/Ray-ymq/GoPulse/lifecycle/internal/release"
)

// CandidateEnv renders the validated immutable amd64 references the isolated
// Compose runner consumes, in the order the manifest lists them.
func CandidateEnv(manifestPath, version, revision string) ([]string, error) {
	path, err := filepath.Abs(manifestPath)
	if err != nil {
		return nil, err
	}
	m, err := VerifyBundle(path)
	if err != nil {
		return nil, err
	}
	if m.Version != version || m.Revision != revision {
		return nil, errors.New("candidate identity mismatch")
	}
	lines := make([]string, 0, len(Products)+len(Sources))
	appendRef := func(name string, image release.Image) error {
		ref, err := PlatformRef(image, "linux/amd64")
		if err != nil {
			return err
		}
		lines = append(lines, "GOPULSE_"+strings.ToUpper(strings.ReplaceAll(name, "-", "_"))+"_IMAGE="+ref)
		return nil
	}
	for _, name := range Products {
		if image, ok := m.Images[name]; ok {
			if err := appendRef(name, image); err != nil {
				return nil, err
			}
		}
	}
	for _, name := range Sources {
		if image, ok := m.ThirdParty[name]; ok {
			if err := appendRef(name, image); err != nil {
				return nil, err
			}
		}
	}
	return lines, nil
}

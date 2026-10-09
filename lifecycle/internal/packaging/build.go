package packaging

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Ray-ymq/GoPulse/lifecycle/internal/release"
)

// buildSpec binds one product image to its Dockerfile and target.
type buildSpec struct{ dockerfile, target string }

func specOf(name string) buildSpec {
	switch name {
	case "backend", "business-worker", "search-indexer":
		return buildSpec{dockerfile: "backend", target: name}
	case "router", "marshaller", "monitor", "redis-exporter":
		return buildSpec{dockerfile: "observability", target: name}
	default:
		return buildSpec{dockerfile: name}
	}
}

func platformArch(platform string) string { return platform[strings.LastIndexByte(platform, '/')+1:] }

// Build pushes the candidate images and assembles the bundle. The manifest is
// published last so an interrupted build never leaves a complete candidate.
func Build(repo *Repo, registry, platformList, output string) (string, error) {
	if !registryNamespace.MatchString(registry) {
		return "", usagef("invalid registry namespace: %s", registry)
	}
	if !platformArgument.MatchString(platformList) {
		return "", usagef("platform must be linux/amd64, linux/arm64 or a comma-separated pair")
	}
	if err := repo.Clean(); err != nil {
		return "", err
	}
	revision, err := repo.Revision()
	if err != nil {
		return "", err
	}
	version, err := repo.Version()
	if err != nil {
		return "", err
	}
	out, err := filepath.Abs(output)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(out, "release-manifest.json")); err == nil {
		return "", usagef("refusing to overwrite complete candidate: %s", filepath.Join(out, "release-manifest.json"))
	}
	images := map[string]release.Image{}
	for _, name := range append(append([]string{}, Products...), "lifecycle") {
		spec := specOf(name)
		tag := fmt.Sprintf("%s/%s:%s-candidate-%s", registry, name, version, revision[:12])
		args := []string{"buildx", "build", "--platform", platformList, "--provenance=false",
			"--build-arg", "VERSION=" + version, "--build-arg", "REVISION=" + revision,
			"--metadata-file", filepath.Join(out, name+"-build.json"),
			"-f", "deploy/docker/" + spec.dockerfile + ".Dockerfile", "-t", tag, "--push"}
		if spec.target != "" {
			args = append(args, "--target", spec.target)
		}
		if err := buildFromArchive(repo, revision, append(args, "-")); err != nil {
			return "", fmt.Errorf("building %s failed: %w", name, err)
		}
		index, err := rawReference(tag)
		if err != nil {
			return "", err
		}
		var probe struct {
			Manifests []json.RawMessage `json:"manifests"`
		}
		if json.Unmarshal(index, &probe) != nil {
			return "", fmt.Errorf("%s: invalid registry index document", tag)
		}
		if len(probe.Manifests) == 0 {
			if err := attached("", "docker", "buildx", "imagetools", "create", "--prefer-index=true", "-t", tag, tag); err != nil {
				return "", err
			}
		}
		if images[name], err = recordImage(tag); err != nil {
			return "", fmt.Errorf("%s: %w", name, err)
		}
	}
	thirdParty := map[string]release.Image{}
	lock, err := repo.File("deploy/release/third-party.lock.json")
	if err != nil {
		return "", errors.New("third-party lock file is missing")
	}
	if err := json.Unmarshal(lock, &thirdParty); err != nil {
		return "", errors.New("invalid third-party lock file")
	}
	// The normalized product name differs from the VictoriaMetrics repository.
	if image, ok := thirdParty["victoria-metrics"]; ok {
		thirdParty["victoriametrics"] = image
		delete(thirdParty, "victoria-metrics")
	}
	m := &release.Manifest{
		SchemaVersion: 1, Version: version, Revision: revision,
		Images:     map[string]release.Image{},
		ThirdParty: thirdParty,
		Lifecycle:  images["lifecycle"],
		Plugins:    []release.Plugin{}, SupportedUpgradeSources: []string{},
	}
	for _, name := range Products {
		m.Images[name] = images[name]
	}
	for _, platform := range strings.Split(platformList, ",") {
		records, err := PluginRecords(m.Images["monitor"], platform, filepath.Join(out, "plugins", platformArch(platform)))
		if err != nil {
			return "", err
		}
		m.Plugins = append(m.Plugins, records...)
		for _, name := range Products {
			if err := InspectImage(m.Images[name], platform, version, revision, true); err != nil {
				return "", fmt.Errorf("%s %s: %w", name, platform, err)
			}
		}
		if err := InspectImage(m.Lifecycle, platform, version, revision, true); err != nil {
			return "", fmt.Errorf("lifecycle %s: %w", platform, err)
		}
	}
	staging, err := os.MkdirTemp(out, ".bundle-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(staging)
	product, err := ProductCompose(repo, m)
	if err != nil {
		return "", err
	}
	if err := WriteBundle(repo, m, product, staging); err != nil {
		return "", err
	}
	manifest := filepath.Join(out, "release-manifest.json")
	err = filepath.WalkDir(staging, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || entry.Name() == "release-manifest.json" {
			return err
		}
		relative, err := filepath.Rel(staging, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(out, relative)
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		return os.Rename(path, destination)
	})
	if err != nil {
		return "", err
	}
	if err := os.Rename(filepath.Join(staging, "release-manifest.json"), manifest); err != nil {
		return "", err
	}
	return manifest, nil
}

// buildFromArchive feeds a committed Git archive to buildx as the only build
// context, so untracked local state cannot leak into a candidate.
func buildFromArchive(repo *Repo, revision string, args []string) error {
	archive := exec.Command("git", "-C", repo.Root, "archive", "--format=tar", revision)
	archive.Stderr = os.Stderr
	context, err := archive.StdoutPipe()
	if err != nil {
		return err
	}
	if err := archive.Start(); err != nil {
		return err
	}
	build := command(repo.Root, "docker", args...)
	build.Stdin = context
	build.Stdout = os.Stdout
	buildErr := build.Run()
	_ = context.Close()
	archiveErr := archive.Wait()
	if buildErr != nil {
		return fmt.Errorf("docker buildx build failed: %w", buildErr)
	}
	if archiveErr != nil {
		return errors.New("git archive failed")
	}
	return nil
}

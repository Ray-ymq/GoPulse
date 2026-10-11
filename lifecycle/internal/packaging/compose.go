package packaging

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Ray-ymq/GoPulse/lifecycle/internal/release"
)

// encodeJSON keeps the documented two-space layout without escaping HTML
// characters, so bundle assets stay readable and byte-stable.
func encodeJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// composeDocument reads the resolved Compose model with interpolation and path
// resolution disabled: the candidate must not depend on the build host.
func composeDocument(repo *Repo) ([]byte, error) {
	out, err := output(repo.Root, "docker", "compose",
		"--env-file", repo.Root+"/.env.example", "-f", repo.Root+"/deploy/compose.yaml",
		"config", "--no-interpolate", "--no-path-resolution", "--format", "json")
	if err != nil {
		return nil, errors.New("cannot resolve the product Compose model")
	}
	return []byte(out), nil
}

// ProductCompose rewrites the resolved Compose model onto immutable candidate
// references and returns the bundle's product topology.
func ProductCompose(repo *Repo, m *release.Manifest) ([]byte, error) {
	document, err := composeDocument(repo)
	if err != nil {
		return nil, err
	}
	return productCompose(m, document)
}

func productCompose(m *release.Manifest, document []byte) ([]byte, error) {
	var model map[string]any
	if err := json.Unmarshal(document, &model); err != nil {
		return nil, errors.New("invalid Compose model")
	}
	delete(model, "name")
	services, ok := model["services"].(map[string]any)
	if !ok {
		return nil, errors.New("Compose model has no services")
	}
	if _, ok := services["acceptance"]; !ok {
		return nil, errors.New("Compose model has no acceptance service")
	}
	delete(services, "acceptance")
	for name, raw := range services {
		service, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("service %s is not an object", name)
		}
		delete(service, "build")
		logical := name
		if alias, ok := composeImageAliases[name]; ok {
			logical = alias
		}
		image, ok := m.Images[logical]
		if !ok {
			image, ok = m.ThirdParty[logical]
		}
		if !ok {
			return nil, fmt.Errorf("unmapped product service: %s", name)
		}
		service["image"] = image.Ref
		service["pull_policy"] = "always"
	}
	// Reuse the existing frontend reverse proxy as the sole product edge and
	// keep the frontend itself internal; development Compose stays unchanged.
	frontend, ok := services["frontend"].(map[string]any)
	if !ok {
		return nil, errors.New("Compose model has no frontend service")
	}
	copied, err := encodeJSON(frontend)
	if err != nil {
		return nil, err
	}
	var edge map[string]any
	if err := json.Unmarshal(copied, &edge); err != nil {
		return nil, err
	}
	services["edge"] = edge
	for name, raw := range services {
		if name == "edge" {
			continue
		}
		if service, ok := raw.(map[string]any); ok {
			delete(service, "ports")
		}
	}
	for _, kind := range []string{"volumes", "networks", "secrets"} {
		resources, ok := model[kind].(map[string]any)
		if !ok {
			continue
		}
		for _, raw := range resources {
			if resource, ok := raw.(map[string]any); ok {
				delete(resource, "name")
			}
		}
	}
	return encodeJSON(model)
}

// toolService is the fixed lifecycle tool service shipped in the bundle.
type toolService struct {
	Image       string           `json:"image"`
	Platform    string           `json:"platform"`
	ReadOnly    bool             `json:"read_only"`
	NetworkMode string           `json:"network_mode"`
	User        string           `json:"user"`
	GroupAdd    []string         `json:"group_add"`
	CapDrop     []string         `json:"cap_drop"`
	SecurityOpt []string         `json:"security_opt"`
	Tmpfs       []string         `json:"tmpfs"`
	Volumes     []map[string]any `json:"volumes"`
	Command     []string         `json:"command"`
}

// ToolCompose renders the immutable lifecycle tool entry point of the bundle.
func ToolCompose(m *release.Manifest) ([]byte, error) {
	ref, err := PlatformRef(m.Lifecycle, "linux/amd64")
	if err != nil {
		return nil, err
	}
	install := "${GOPULSE_INSTALL_DIR:?set absolute private installation directory}"
	socket := "${GOPULSE_DOCKER_SOCKET:-/var/run/docker.sock}"
	tool := toolService{
		Image: ref, Platform: "linux/amd64", ReadOnly: true, NetworkMode: "host",
		User:     "${GOPULSE_TOOL_UID:?set host uid}:${GOPULSE_TOOL_GID:?set host gid}",
		GroupAdd: []string{"${GOPULSE_SOCKET_GID:?set Docker socket gid}"},
		CapDrop:  []string{"ALL"}, SecurityOpt: []string{"no-new-privileges:true"}, Tmpfs: []string{"/tmp"},
		Volumes: []map[string]any{
			{"type": "bind", "source": "${GOPULSE_BUNDLE_DIR:?set absolute bundle directory}", "target": "/bundle", "read_only": true},
			{"type": "bind", "source": install, "target": install},
			{"type": "bind", "source": socket, "target": "/var/run/docker.sock"},
		},
		Command: []string{"version", "--json"},
	}
	return encodeJSON(map[string]any{"services": map[string]any{"lifecycle": tool}})
}

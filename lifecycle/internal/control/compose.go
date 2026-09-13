package control

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const prefix = "io.gopulse.lifecycle."

func (c *Controller) labels(resource string) map[string]string {
	return map[string]string{prefix + "installation": c.state.Token, prefix + "manifest": c.manifestHash, prefix + "resource": resource}
}
func (c *Controller) prepare() error {
	aliases := map[string]string{"edge": "frontend", "migrate": "backend", "search-init": "backend", "admin-role": "backend", "kafka-init": "kafka"}
	for name, raw := range c.services {
		if name == "lifecycle" {
			delete(c.services, name)
			continue
		}
		s, ok := raw.(map[string]any)
		if !ok {
			return fail(ManifestError, "compose", "invalid service")
		}
		logical := name
		if alias := aliases[name]; alias != "" {
			logical = alias
		}
		image, ok := c.manifest.Images[logical]
		if !ok {
			image, ok = c.manifest.ThirdParty[logical]
		}
		ref := strings.Split(image.Ref, "@")[0] + "@" + image.Platforms["linux/amd64"]
		if !ok || (s["image"] != image.Ref && s["image"] != ref) {
			return fail(ManifestError, "compose", "service image must match manifest: "+name)
		}
		s["image"] = ref
		s["platform"] = "linux/amd64"
		s["pull_policy"] = "never"
		s["labels"] = c.labels(name)
		for _, key := range []string{"build", "container_name", "network_mode", "privileged", "devices", "env_file"} {
			if _, ok := s[key]; ok {
				return fail(ManifestError, "compose", "unsafe service field: "+key)
			}
		}
		if ports, ok := s["ports"]; ok {
			if name != "edge" {
				return fail(ManifestError, "compose", "only edge may publish ports")
			}
			_ = ports
			s["ports"] = []string{"127.0.0.1:" + strconv.Itoa(c.state.Port) + ":8080"}
		}
		if mounts, ok := s["volumes"].([]any); ok {
			for _, raw := range mounts {
				v, ok := raw.(map[string]any)
				if !ok || v["type"] != "volume" {
					return fail(ManifestError, "compose", "product bind mounts forbidden")
				}
			}
		}
	}
	if c.services["edge"] == nil {
		return fail(ManifestError, "compose", "unique edge service required")
	}
	for _, kind := range []string{"networks", "volumes"} {
		resources, _ := c.doc[kind].(map[string]any)
		for name, raw := range resources {
			r, ok := raw.(map[string]any)
			if !ok {
				r = map[string]any{}
				resources[name] = r
			}
			if r["external"] == true {
				return fail(ManifestError, "compose", "external product resources forbidden")
			}
			r["name"] = c.state.Project + "_" + name
			r["labels"] = c.labels(kind + "/" + name)
		}
	}
	// Compose environment-sourced secrets create a temporary host bind path that
	// does not exist outside the tool container. Use a private same-path mount.
	c.doc["secrets"] = map[string]any{"victoriametrics_password": map[string]any{"file": filepath.Join(c.dir, "victoriametrics_password")}}
	return nil
}
func (c *Controller) compose(args ...string) error {
	if e := c.prepare(); e != nil {
		return e
	}
	b, _ := json.Marshal(c.doc)
	argv := []string{"--host", c.endpoint, "compose", "--project-name", c.state.Project, "--project-directory", c.dir, "--env-file", "/dev/null", "-f", "-"}
	cmd := exec.CommandContext(c.ctx, "docker", append(argv, args...)...)
	isolate(cmd)
	cmd.Stdin = strings.NewReader(string(b))
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=/tmp", "COMPOSE_DISABLE_ENV_FILE=1"}
	for k, v := range c.env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	// Never echo Compose output, environment, or rendered configuration.
	if e := cmd.Run(); e != nil {
		return fail(Failed, c.state.Phase, "Compose failed; use status/logs; data and resources retained for retry")
	}
	return nil
}

type Container struct {
	ID     string `json:"Id"`
	Config struct {
		Image  string
		Labels map[string]string
	}
	State struct {
		Status   string
		ExitCode int
		Health   *struct{ Status string }
	}
	HostConfig struct {
		PortBindings map[string][]struct{ HostIp, HostPort string }
	}
}

func (c *Controller) containers() ([]Container, error) {
	b, e := c.docker("ps", "-aq", "--filter", "label=com.docker.compose.project="+c.state.Project)
	if e != nil {
		return nil, fail(Daemon, "inspect", "cannot list project containers")
	}
	ids := strings.Fields(string(b))
	if len(ids) == 0 {
		return []Container{}, nil
	}
	b, e = c.docker(append([]string{"inspect", "--type", "container"}, ids...)...)
	var cs []Container
	if e != nil || json.Unmarshal(b, &cs) != nil {
		return nil, fail(Daemon, "inspect", "cannot inspect project containers")
	}
	return cs, nil
}
func (c *Controller) checkLabels(labels map[string]string, resource string) bool {
	return labels["com.docker.compose.project"] == c.state.Project && labels[prefix+"installation"] == c.state.Token && labels[prefix+"manifest"] == c.manifestHash && labels[prefix+"resource"] == resource
}
func (c *Controller) owned() ([]Container, error) {
	if e := c.prepare(); e != nil {
		return nil, e
	}
	cs, e := c.containers()
	if e != nil {
		return nil, e
	}
	for _, container := range cs {
		n := container.Config.Labels["com.docker.compose.service"]
		s, ok := c.services[n].(map[string]any)
		if !ok || !c.checkLabels(container.Config.Labels, n) || container.Config.Image != s["image"] {
			return nil, fail(Ownership, "ownership", "foreign container or image in project; refusing operation")
		}
	}
	for _, kind := range []string{"network", "volume"} {
		b, e := c.docker(kind, "ls", "-q", "--filter", "label=com.docker.compose.project="+c.state.Project)
		if e != nil {
			return nil, fail(Daemon, "ownership", "cannot list project resources")
		}
		ids := strings.Fields(string(b))
		if len(ids) == 0 {
			continue
		}
		b, e = c.docker(append([]string{kind, "inspect"}, ids...)...)
		var resources []struct {
			Name   string
			Labels map[string]string
		}
		if e != nil || json.Unmarshal(b, &resources) != nil {
			return nil, fail(Daemon, "ownership", "cannot inspect resources")
		}
		for _, r := range resources {
			name := strings.TrimPrefix(r.Name, c.state.Project+"_")
			all, _ := c.doc[kind+"s"].(map[string]any)
			if _, ok := all[name]; !ok || r.Name != c.state.Project+"_"+name || !c.checkLabels(r.Labels, kind+"s/"+name) {
				return nil, fail(Ownership, "ownership", "foreign project resource; refusing operation")
			}
		}
	}
	// Detect name collisions even when a foreign resource has no project label.
	for _, kind := range []string{"network", "volume"} {
		all, _ := c.doc[kind+"s"].(map[string]any)
		for name := range all {
			b, e := c.docker(kind, "ls", "--format", "{{json .}}", "--filter", "name="+c.state.Project+"_"+name)
			if e != nil {
				return nil, fail(Daemon, "ownership", "cannot inspect resource names")
			}
			for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
				if line == "" {
					continue
				}
				var r struct{ Name string }
				if json.Unmarshal([]byte(line), &r) != nil {
					return nil, fail(Daemon, "ownership", "invalid resource listing")
				}
				if r.Name != c.state.Project+"_"+name {
					continue
				}
				b, e = c.docker(kind, "inspect", r.Name)
				var rs []struct{ Labels map[string]string }
				if e != nil || json.Unmarshal(b, &rs) != nil || len(rs) != 1 || !c.checkLabels(rs[0].Labels, kind+"s/"+name) {
					return nil, fail(Ownership, "ownership", "resource name collision; refusing adoption")
				}
			}
		}
	}
	return cs, nil
}
func (c *Controller) phase(name string) error {
	c.state.Phase = name
	if e := c.save(); e != nil {
		return fail(Permission, "state", "cannot persist operation phase")
	}
	return nil
}
func (c *Controller) up() error {
	if _, e := c.owned(); e != nil {
		return e
	}
	if e := c.phase("pull"); e != nil {
		return e
	}
	// Docker verifies content-addressed pulls. Every service uses the selected
	// platform digest, never tags or mutable defaults.
	seen := map[string]bool{}
	for _, name := range sortedKeys(c.services) {
		s := c.services[name].(map[string]any)
		ref := s["image"].(string)
		if seen[ref] {
			continue
		}
		seen[ref] = true
		if _, e := c.docker("pull", "--platform", "linux/amd64", ref); e != nil {
			return fail(ManifestError, "pull", "cannot pull manifest digest for "+name)
		}
	}
	if e := atomicFile(filepath.Join(c.dir, "victoriametrics_password"), []byte(c.env["VICTORIAMETRICS_PASSWORD"])); e != nil {
		return fail(Permission, "secrets", "cannot materialize private file secret")
	}
	if e := c.phase("infrastructure"); e != nil {
		return e
	}
	if e := c.compose("up", "-d", "--no-build", "--pull", "never", "--wait", "--wait-timeout", "240", "mysql", "redis", "rabbitmq", "kafka", "elasticsearch", "victoriametrics"); e != nil {
		return e
	}
	if e := c.phase("initialize"); e != nil {
		return e
	}
	for _, job := range []string{"migrate", "kafka-init", "search-init"} {
		if e := c.compose("up", "--no-build", "--pull", "never", "--no-deps", "--exit-code-from", job, job); e != nil {
			return e
		}
	}
	if e := c.phase("services"); e != nil {
		return e
	}
	if e := c.compose("up", "-d", "--no-build", "--pull", "never", "--wait", "--wait-timeout", "240"); e != nil {
		return e
	}
	if _, e := c.status(true); e != nil {
		return e
	}
	return c.phase("ready")
}
func (c *Controller) down(purge bool, confirm string) error {
	if purge && confirm != c.state.Project {
		return fail(Usage, "confirmation", "--purge requires --confirm with exact installation project")
	}
	if _, e := c.owned(); e != nil {
		return e
	}
	if e := c.phase("stopping"); e != nil {
		return e
	}
	args := []string{"down", "--timeout", "30"}
	if purge {
		args = append(args, "--volumes")
	}
	if e := c.compose(args...); e != nil {
		return e
	}
	return c.phase("stopped")
}
func (c *Controller) status(strict bool) (any, error) {
	cs, e := c.owned()
	if e != nil {
		return nil, e
	}
	result := map[string]any{}
	ready := true
	for _, n := range sortedKeys(c.services) {
		s := c.services[n].(map[string]any)
		if s["profiles"] != nil {
			continue
		}
		found := false
		for _, container := range cs {
			if container.Config.Labels["com.docker.compose.service"] != n {
				continue
			}
			if found {
				return nil, fail(Ownership, "verify", "duplicate service container")
			}
			found = true
			state := container.State.Status
			healthy := state == "running"
			if container.State.Health != nil {
				healthy = healthy && container.State.Health.Status == "healthy"
				state += "/" + container.State.Health.Status
			}
			if n == "migrate" || n == "kafka-init" || n == "search-init" {
				healthy = container.State.Status == "exited" && container.State.ExitCode == 0
			}
			ready = ready && healthy
			result[n] = state
			for _, bindings := range container.HostConfig.PortBindings {
				for _, binding := range bindings {
					if n != "edge" || binding.HostIp != "127.0.0.1" || binding.HostPort != strconv.Itoa(c.state.Port) {
						return nil, fail(Ownership, "edge", "unexpected published port")
					}
				}
			}
			if n == "edge" && len(container.HostConfig.PortBindings) == 0 {
				ready = false
			}
		}
		if !found {
			result[n] = "missing"
			ready = false
		}
	}
	if strict && !ready {
		return result, fail(NotReady, "verify", "product is not ready; inspect status (verification is read-only)")
	}
	return result, nil
}
func (c *Controller) redact(text string) string {
	for k, v := range c.env {
		if len(v) > 0 && (strings.Contains(k, "TOKEN") || strings.Contains(k, "SECRET") || strings.Contains(k, "PASSWORD")) {
			text = strings.ReplaceAll(text, v, "[REDACTED]")
		}
	}
	return strings.ReplaceAll(text, c.state.Token, "[REDACTED]")
}
func (c *Controller) logs(service string, tail int, since, until string) (string, error) {
	if _, ok := c.services[service]; !ok || service == "lifecycle" {
		return "", fail(Usage, "logs", "service is not in bundle allowlist")
	}
	cs, e := c.owned()
	if e != nil {
		return "", e
	}
	var lines strings.Builder
	for _, container := range cs {
		if container.Config.Labels["com.docker.compose.service"] != service {
			continue
		}
		args := []string{"logs", "--tail", strconv.Itoa(tail), "--timestamps"}
		if since != "" {
			args = append(args, "--since", since)
		}
		if until != "" {
			args = append(args, "--until", until)
		}
		args = append(args, container.ID)
		cmd := exec.CommandContext(c.ctx, "docker", append([]string{"--host", c.endpoint}, args...)...)
		isolate(cmd)
		b, e := cmd.CombinedOutput()
		if e != nil {
			return "", fail(Failed, "logs", "cannot read scoped logs")
		}
		fmt.Fprint(&lines, c.redact(string(b)))
	}
	return lines.String(), nil
}

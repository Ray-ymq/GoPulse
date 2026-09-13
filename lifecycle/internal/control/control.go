package control

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Ray-ymq/GoPulse/lifecycle/internal/release"
)

const (
	Usage         = 2
	ManifestError = 10
	Daemon        = 11
	Platform      = 12
	Capacity      = 13
	Permission    = 14
	Port          = 15
	Locked        = 16
	Ownership     = 17
	Failed        = 18
	NotReady      = 19
	Interrupted   = 20
)

type Failure struct {
	Code    int    `json:"code"`
	Stage   string `json:"stage"`
	Message string `json:"message"`
}

func (e *Failure) Error() string             { return e.Message }
func fail(code int, stage, msg string) error { return &Failure{code, stage, msg} }

type Controller struct {
	ctx                   context.Context
	dir, bundle, endpoint string
	manifest              *release.Manifest
	manifestHash          string
	state                 State
	env                   map[string]string
	doc                   map[string]any
	services              map[string]any
}

func (c *Controller) docker(args ...string) ([]byte, error) {
	cmd := exec.CommandContext(c.ctx, "docker", append([]string{"--host", c.endpoint}, args...)...)
	isolate(cmd)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=/tmp", "DOCKER_CONFIG=/tmp/gopulse-docker-empty"}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	b, e := cmd.Output()
	if e != nil {
		return nil, errors.New("Docker operation failed; inspect scoped status/logs (raw diagnostics suppressed)")
	}
	return b, nil
}

// Kill the entire Docker/Compose process group on cancellation before releasing
// the installation lock, including the Compose CLI plugin child.
func isolate(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 2 * time.Second
}
func Run(ctx context.Context, args []string, version, revision string, out io.Writer) error {
	if len(args) == 0 {
		return fail(Usage, "arguments", "command required")
	}
	command := args[0]
	if command == "backup-inspect" {
		return inspectBackup(args[1:], out)
	}
	switch command {
	case "doctor", "init", "up", "down", "status", "logs", "verify":
	default:
		return fail(Usage, "arguments", "unknown command")
	}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("install", "", "absolute private installation directory, mounted at the same host path")
	bundle := fs.String("bundle", "/bundle", "immutable bundle directory")
	endpoint := fs.String("endpoint", "", "explicit unix:///path Docker endpoint")
	port := fs.Int("port", 18080, "loopback edge port (init/doctor)")
	service := fs.String("service", "edge", "allowlisted service (logs)")
	tail := fs.Int("tail", 100, "log lines, 1..1000")
	since := fs.String("since", "", "RFC3339 start time")
	until := fs.String("until", "", "RFC3339 end time")
	purge := fs.Bool("purge", false, "delete owned volumes with --confirm PROJECT")
	confirm := fs.String("confirm", "", "project confirmation")
	fs.Bool("json", true, "structured output (always enabled)")
	if fs.Parse(args[1:]) != nil || fs.NArg() != 0 || !filepath.IsAbs(*dir) || !filepath.IsAbs(*bundle) || !strings.HasPrefix(*endpoint, "unix:///") || *port < 1024 || *port > 65535 {
		return fail(Usage, "arguments", "require absolute --install/--bundle, explicit local --endpoint unix:///path and port 1024..65535")
	}
	if *tail < 1 || *tail > 1000 {
		return fail(Usage, "arguments", "tail must be 1..1000")
	}
	for _, s := range []string{*since, *until} {
		if s != "" {
			if _, e := time.Parse(time.RFC3339, s); e != nil {
				return fail(Usage, "arguments", "log times must be RFC3339")
			}
		}
	}
	c := &Controller{ctx: ctx, dir: filepath.Clean(*dir), bundle: filepath.Clean(*bundle), endpoint: *endpoint}
	if e := privateDir(c.dir); e != nil {
		return fail(Permission, "directory", "create a private installation directory with mode 0700")
	}
	if e := c.loadManifest(version, revision); e != nil {
		return e
	}
	if e := c.server(); e != nil {
		return e
	}
	mutate := command == "init" || command == "up" || command == "down"
	if mutate {
		unlock, e := lock(c.dir)
		if e != nil {
			return fail(Locked, "lock", "another operation holds the installation lock or lock is unsafe")
		}
		defer unlock()
	}
	if command == "init" || command == "doctor" {
		if command == "init" {
			entries, err := os.ReadDir(c.dir)
			if err != nil {
				return fail(Permission, "init", "cannot inspect directory")
			}
			for _, entry := range entries {
				if entry.Name() != ".lock" {
					return fail(Permission, "init", "init requires an empty installation directory")
				}
			}
		}
		if e := c.doctor(*port); e != nil {
			return e
		}
		if command == "doctor" {
			return json.NewEncoder(out).Encode(map[string]any{"schema": 1, "command": command, "platform": "linux/amd64", "manifest_digest": c.manifestHash, "status": "passed"})
		}
		if e := c.initialize(*port); e != nil {
			return e
		}
	} else {
		if e := c.load(); e != nil {
			return e
		}
	}
	if mutate && command != "init" {
		c.state.Operation = random()
		if e := c.save(); e != nil {
			return fail(Permission, "state", "cannot persist operation")
		}
	}
	var result any
	var err error
	switch command {
	case "init":
		result = c.public(nil)
	case "up":
		err = c.up()
		result = c.public(nil)
	case "down":
		err = c.down(*purge, *confirm)
		result = c.public(nil)
	case "status", "verify":
		var statuses any
		statuses, err = c.status(command == "verify")
		result = c.public(statuses)
	case "logs":
		var text string
		text, err = c.logs(*service, *tail, *since, *until)
		result = map[string]any{"schema": 1, "service": *service, "logs": text}
	}
	if err != nil {
		if ctx.Err() != nil && mutate {
			c.state.Phase = "interrupted"
			_ = c.save()
			return fail(Interrupted, "interrupted", "operation interrupted; rerun status then up/down; data retained")
		}
		return err
	}
	return json.NewEncoder(out).Encode(result)
}
func (c *Controller) public(services any) any {
	return map[string]any{"schema": 1, "version": c.state.Version, "manifest_digest": c.state.Manifest, "project": c.state.Project, "phase": c.state.Phase, "operation_id": c.state.Operation, "edge": "http://127.0.0.1:" + strconv.Itoa(c.state.Port), "services": services}
}
func (c *Controller) loadManifest(version, rev string) error {
	b, e := os.ReadFile(filepath.Join(c.bundle, "release-manifest.json"))
	if e != nil {
		return fail(ManifestError, "manifest", "cannot read release manifest")
	}
	c.manifest, e = release.Parse(b)
	if e != nil {
		return fail(ManifestError, "manifest", "invalid release manifest")
	}
	c.manifestHash = release.Sum(b)
	if e = c.manifest.CheckAssets(c.bundle); e != nil {
		return fail(ManifestError, "checksum", "bundle compose checksum mismatch")
	}
	if e = c.manifest.CheckTool(version, rev, "amd64", "linux", "amd64"); e != nil {
		return fail(ManifestError, "identity", "tool version/revision must match release manifest")
	}
	readme, e := os.ReadFile(filepath.Join(c.bundle, "README.md"))
	if e != nil {
		return fail(ManifestError, "checksum", "missing bundle README")
	}
	compose, e := os.ReadFile(filepath.Join(c.bundle, c.manifest.Compose.Path))
	if e != nil {
		return fail(ManifestError, "checksum", "missing compose")
	}
	tool, e := os.ReadFile(filepath.Join(c.bundle, "compose.yaml"))
	if e != nil {
		return fail(ManifestError, "checksum", "missing tool Compose entry")
	}
	payload := strings.TrimPrefix(release.Sum(readme), "sha256:") + "  README.md\n" + strings.TrimPrefix(release.Sum(tool), "sha256:") + "  compose.yaml\n" + strings.TrimPrefix(release.Sum(compose), "sha256:") + "  deploy/product/compose.yaml\n"
	if release.Sum([]byte(payload)) != c.manifest.BundleSHA256 {
		return fail(ManifestError, "checksum", "bundle payload mismatch")
	}
	checks, e := os.ReadFile(filepath.Join(c.bundle, "checksums"))
	expected := payload + strings.TrimPrefix(release.Sum(b), "sha256:") + "  release-manifest.json\n"
	if e != nil || string(checks) != expected {
		return fail(ManifestError, "checksum", "bundle checksums mismatch")
	}
	if json.Unmarshal(compose, &c.doc) != nil {
		return fail(ManifestError, "compose", "product compose must be canonical JSON")
	}
	c.services, _ = c.doc["services"].(map[string]any)
	if c.services == nil {
		return fail(ManifestError, "compose", "missing services")
	}
	return nil
}
func (c *Controller) server() error {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return fail(Platform, "platform", "lifecycle tool must run on Linux amd64")
	}
	b, e := c.docker("version", "--format", "{{json .Server}}")
	if e != nil {
		return fail(Daemon, "daemon", "Docker unavailable at explicit endpoint")
	}
	var s struct{ Os, Arch, Version string }
	if json.Unmarshal(b, &s) != nil {
		return fail(Daemon, "daemon", "invalid Docker server response")
	}
	if s.Os != "linux" || s.Arch != "amd64" {
		return fail(Platform, "platform", "only Linux amd64 Docker servers are supported")
	}
	major, _ := strconv.Atoi(strings.Split(s.Version, ".")[0])
	if major < 24 {
		return fail(Daemon, "docker-version", "Docker Engine >=24 required")
	}
	b, e = c.docker("compose", "version", "--format", "json")
	var v struct{ Version string }
	if e != nil || json.Unmarshal(b, &v) != nil {
		return fail(Daemon, "compose-version", "Compose >=2.24 required")
	}
	parts := strings.Split(strings.TrimPrefix(v.Version, "v"), ".")
	major, _ = strconv.Atoi(parts[0])
	minor := 0
	if len(parts) > 1 {
		minor, _ = strconv.Atoi(parts[1])
	}
	if major < 2 || (major == 2 && minor < 24) {
		return fail(Daemon, "compose-version", "Compose >=2.24 required")
	}
	return nil
}

// Use the Engine distribution API, not client-side registry lookup: the
// daemon owns registry mirrors/TLS configuration and is the actual pull actor.
func (c *Controller) distribution(ref string, platform bool) error {
	client := http.Client{Timeout: 60 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", strings.TrimPrefix(c.endpoint, "unix://"))
	}}}
	defer client.CloseIdleConnections()
	req, e := http.NewRequestWithContext(c.ctx, http.MethodGet, "http://docker/v1.43/distribution/"+url.PathEscape(ref)+"/json", nil)
	if e != nil {
		return e
	}
	response, e := client.Do(req)
	if e != nil {
		return e
	}
	defer response.Body.Close()
	var result struct {
		Descriptor struct{ Digest string }
		Platforms  []struct{ OS, Architecture string }
	}
	if response.StatusCode != 200 || json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result) != nil || result.Descriptor.Digest != strings.Split(ref, "@")[1] {
		return errors.New("registry descriptor mismatch")
	}
	found := false
	for _, p := range result.Platforms {
		if p.OS == "linux" && p.Architecture == "amd64" {
			found = true
		}
	}
	if !platform && !found {
		return errors.New("registry platform mismatch")
	}
	return nil
}
func (c *Controller) doctor(port int) error {
	images := map[string]release.Image{"lifecycle": c.manifest.Lifecycle}
	for n, i := range c.manifest.Images {
		images[n] = i
	}
	for n, i := range c.manifest.ThirdParty {
		images[n] = i
	}
	for name, image := range images {
		if err := c.distribution(image.Ref, false); err != nil {
			return fail(ManifestError, "digest", "registry index unavailable for "+name)
		}
		ref := strings.Split(image.Ref, "@")[0] + "@" + image.Platforms["linux/amd64"]
		if err := c.distribution(ref, true); err != nil {
			return fail(ManifestError, "digest", "registry platform digest unavailable for "+name)
		}
	}

	var st syscall.Statfs_t
	if syscall.Statfs(c.dir, &st) != nil || st.Bavail*uint64(st.Bsize) < 5<<30 {
		return fail(Capacity, "disk", "at least 5 GiB free installation disk required")
	}
	b, e := c.docker("info", "--format", "{{json .}}")
	var info struct {
		MemTotal int64
		NCPU     int
	}
	if e != nil || json.Unmarshal(b, &info) != nil {
		return fail(Daemon, "daemon", "cannot inspect Docker capacity")
	}
	if info.MemTotal < 6<<30 || info.NCPU < 2 {
		return fail(Capacity, "memory", "Docker server requires >=6 GiB memory and 2 CPUs")
	}
	f, e := os.CreateTemp(c.dir, ".doctor-")
	if e != nil {
		return fail(Permission, "directory", "installation directory is not writable")
	}
	name := f.Name()
	e = f.Sync()
	f.Close()
	os.Remove(name)
	if e != nil {
		return fail(Permission, "directory", "installation directory cannot sync")
	}
	l, e := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if e != nil {
		return fail(Port, "port", "edge port occupied; choose another --port")
	}
	l.Close()
	return nil
}
func (c *Controller) load() error {
	b, e := readPrivate(filepath.Join(c.dir, "state.json"))
	if e != nil || json.Unmarshal(b, &c.state) != nil {
		return fail(Permission, "state", "private initialized state required")
	}
	if c.state.Schema != 1 || c.state.Manifest != c.manifestHash || c.state.Version != c.manifest.Version || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(c.state.Token) || c.state.Project != "gopulse-"+c.state.Token[:12] || c.state.Port < 1024 || c.state.Port > 65535 {
		return fail(Ownership, "state", "installation identity does not match bundle")
	}
	b, e = readPrivate(filepath.Join(c.dir, "secrets.json"))
	if e != nil || json.Unmarshal(b, &c.env) != nil {
		return fail(Permission, "secrets", "private secrets required")
	}
	return nil
}
func (c *Controller) initialize(port int) error {
	entries, e := os.ReadDir(c.dir)
	if e != nil {
		return fail(Permission, "init", "cannot read installation directory")
	}
	for _, f := range entries {
		if f.Name() != ".lock" {
			return fail(Permission, "init", "init requires empty directory; existing secrets are never overwritten")
		}
	}
	c.state = State{1, c.manifest.Version, c.manifestHash, "", "", port, "initialized", random()}
	c.state.Token = random()
	c.state.Project = "gopulse-" + c.state.Token[:12]
	c.env = map[string]string{"MYSQL_DATABASE": "gopulse", "MYSQL_USER": "gopulse", "RABBITMQ_USER": "gopulse", "VICTORIAMETRICS_USERNAME": "gopulse", "GOPULSE_VERSION": c.manifest.Version, "GOPULSE_REVISION": c.manifest.Revision, "AUTH_COOKIE_SECURE": "false", "PUBLISHED_HOST": "127.0.0.1", "FRONTEND_PORT": strconv.Itoa(port), "HTTP_PORT": strconv.Itoa(port)}
	data, _ := json.Marshal(c.doc)
	r := regexp.MustCompile(`\$\{([A-Z0-9_]+):\?[^}]*\}`)
	for _, match := range r.FindAllSubmatch(data, -1) {
		key := string(match[1])
		if _, ok := c.env[key]; !ok {
			if strings.Contains(key, "PASSWORD") || strings.Contains(key, "TOKEN") || strings.Contains(key, "SECRET") {
				c.env[key] = random()
			} else {
				return fail(ManifestError, "configuration", "unsupported required configuration key: "+key)
			}
		}
	}
	if e = c.prepare(); e != nil {
		return e
	}
	b, _ := json.Marshal(c.env)
	if e = atomicFile(filepath.Join(c.dir, "secrets.json"), b); e != nil {
		return fail(Permission, "init", "cannot write private secrets")
	}
	if e = c.save(); e != nil {
		os.Remove(filepath.Join(c.dir, "secrets.json"))
		return fail(Permission, "init", "cannot commit initialized state")
	}
	return nil
}
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

package plugin

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/Ray-ymq/GoPulse/monitor/internal/events"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// revision is immutable. Only active.json commits a revision; registry/current
// are compatibility projections and are never used to choose between revisions.
type revision struct {
	ID     string          `json:"revision"`
	Entry  registryEntry   `json:"entry"`
	Config json.RawMessage `json:"-"`
	Secret json.RawMessage `json:"-"`
}
type runtimeSlot struct {
	blocked   bool
	operation chan struct{}
	active    *revision
	process   *runtimeProcess
	observer  MetricsLifecycle
	status    Status
}
type runtimeCore struct {
	cfg      ManagerConfig
	catalog  *releaseCatalog
	identity storageIdentity
	mu       sync.RWMutex
	slots    map[string]*runtimeSlot
	// Private test seam represents a process interruption, not a production flag.
	interrupt func(string) error
}

func NewManager(ctx context.Context, cfg ManagerConfig) (*Manager, error) {
	if cfg.PackagesRoot == "" {
		cfg.PackagesRoot = "/opt/gopulse/packages"
	}
	var releases []Release
	if json.Unmarshal([]byte(compiledReleaseJSON), &releases) != nil {
		return nil, operationFailed()
	}
	catalog, err := newReleaseCatalog(cfg.PackagesRoot, releases)
	if err != nil {
		return nil, err
	}
	core, err := newRuntimeCore(ctx, cfg, catalog)
	if err != nil {
		return nil, err
	}
	return &Manager{core: core}, nil
}
func operationFailed() error { return NewError(CodeFailed, "plugin operation failed") }
func unavailable() error     { return NewError(CodeNotFound, "plugin was not found") }
func newRuntimeCore(ctx context.Context, cfg ManagerConfig, catalog *releaseCatalog) (*runtimeCore, error) {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.StopTimeout <= 0 {
		cfg.StopTimeout = 5 * time.Second
	}
	if cfg.StartupTimeout <= 0 {
		cfg.StartupTimeout = 10 * time.Second
	}
	root, identity, err := ensureRoot(cfg.Root)
	if err != nil {
		return nil, err
	}
	cfg.Root = root
	if err = os.Chmod(root, 0700); err != nil {
		return nil, err
	}
	c := &runtimeCore{cfg: cfg, catalog: catalog, identity: identity, slots: map[string]*runtimeSlot{}}
	for _, item := range OfficialCatalog() {
		c.slots[item.ID] = &runtimeSlot{operation: make(chan struct{}, 1)}
	}
	// Load old state only if there is no active commit for that ID. A damaged
	// legacy registry cannot prevent recovery from an already committed revision.
	old, legacyErr := loadRegistry(root)
	for _, item := range OfficialCatalog() {
		slot := c.slots[item.ID]
		active, err := c.loadActive(item.ID)
		if errors.Is(err, os.ErrNotExist) {
			if legacyErr != nil {
				slot.blocked = true
				continue
			}
			entry, exists := old.Plugins[item.ID]
			if !exists {
				if e := c.cleanRecord(item.ID); e != nil {
					slot.blocked = true
				} else {
					c.discardCandidates(item.ID, "")
				}
				continue
			}
			active, err = c.migrate(ctx, item.ID, entry)
		} else if err == nil {
			slot.active = active
			err = c.restore(ctx, item.ID, active)
		}
		if err != nil {
			slot.blocked = true
			// Invalid inputs never become an executable or a process ownership claim.
			if active != nil {
				slot.active = active
				c.setStatus(item.ID, active, ObservedFailed, "recovery_failed")
			}
			continue
		}
		slot.active = active
		_ = switchCurrent(c.dir(item.ID), active.Entry.CurrentVersion)
		c.discardCandidates(item.ID, active.ID)
	}
	blocked := false
	for _, slot := range c.slots {
		blocked = blocked || slot.blocked
	}
	if !blocked {
		c.projectRegistry()
	}
	return c, nil
}
func (c *runtimeCore) dir(id string) string { return filepath.Join(c.cfg.Root, id) }
func (c *runtimeCore) slot(id string) (*runtimeSlot, error) {
	s, ok := c.slots[id]
	if !ok {
		return nil, unavailable()
	}
	return s, nil
}
func (c *runtimeCore) lock(ctx context.Context, id string) (*runtimeSlot, error) {
	s, err := c.slot(id)
	if err != nil {
		return nil, err
	}
	select {
	case s.operation <- struct{}{}:
	case <-ctx.Done():
		return nil, operationFailed()
	}
	actual, err := pathIdentity(c.cfg.Root)
	if err != nil || actual != c.identity || rejectSymlinkComponents(c.dir(id)) != nil {
		<-s.operation
		return nil, operationFailed()
	}
	if s.blocked {
		<-s.operation
		return nil, operationFailed()
	}
	return s, nil
}
func unlock(s *runtimeSlot) { <-s.operation }
func (c *runtimeCore) list() []Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := []Status{}
	for _, entry := range OfficialCatalog() {
		if s := c.slots[entry.ID]; s.active != nil {
			out = append(out, s.status)
		}
	}
	return out
}
func (c *runtimeCore) get(id string) (Status, error) {
	s, err := c.slot(id)
	if err != nil {
		return Status{}, err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if s.blocked {
		return Status{}, operationFailed()
	}
	if s.active == nil {
		return Status{}, unavailable()
	}
	return s.status, nil
}
func (c *runtimeCore) setStatus(id string, r *revision, state ObservedState, code string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.slots[id]
	status := preserveMetrics(statusFromEntry(r.Entry, state, nil), s.status)
	if state == ObservedRunning {
		now := c.cfg.Now().UTC()
		status.StartedAt = &now
	}
	if code != "" {
		msg := "plugin failed to recover"
		if code == "process_exited" {
			msg = "plugin process exited unexpectedly"
		}
		status.LastError = &SafeError{Code: code, Message: msg, At: c.cfg.Now().UTC()}
	}
	s.status = status
}
func strictFile(path string, out any) error {
	if rejectSymlinkComponents(path) != nil {
		return operationFailed()
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return operationFailed()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !uniqueJSON(data) {
		return operationFailed()
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	return d.Decode(out)
}
func (c *runtimeCore) loadActive(id string) (*revision, error) {
	var pointer struct {
		Revision string `json:"revision"`
	}
	if err := strictFile(filepath.Join(c.dir(id), "active.json"), &pointer); err != nil {
		return nil, err
	}
	if len(pointer.Revision) != 32 || !digestPattern.MatchString(pointer.Revision+pointer.Revision) {
		return nil, operationFailed()
	}
	dir := filepath.Join(c.dir(id), "revisions", pointer.Revision)
	var r revision
	if err := strictFile(filepath.Join(dir, "revision.json"), &r); err != nil {
		return nil, err
	}
	if r.ID != pointer.Revision || r.Entry.Manifest.ID != id || r.Entry.CurrentVersion != r.Entry.Manifest.Version || !validReleaseManifest(r.Entry.Manifest) || (r.Entry.DesiredState != DesiredRunning && r.Entry.DesiredState != DesiredStopped) || r.Entry.InstalledAt.IsZero() || r.Entry.UpdatedAt.IsZero() {
		return nil, operationFailed()
	}
	if err := strictFile(filepath.Join(dir, "config.json"), &r.Config); err != nil {
		return nil, err
	}
	secretPath := filepath.Join(dir, "secret.json")
	if err := strictFile(secretPath, &r.Secret); err != nil {
		return nil, err
	}
	info, err := os.Stat(secretPath)
	if err != nil || info.Mode().Perm() != 0600 {
		return nil, operationFailed()
	}
	for _, parent := range []string{c.dir(id), filepath.Join(c.dir(id), "revisions"), dir} {
		info, e := os.Stat(parent)
		if e != nil || info.Mode().Perm() != 0700 {
			return nil, operationFailed()
		}
	}
	if _, _, err = c.parseRequest(id, configRequest(r.Config, r.Secret), nil); err != nil {
		return nil, err
	}
	return &r, nil
}
func secureDir(path string) error {
	if rejectSymlinkComponents(path) != nil {
		return operationFailed()
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	return os.Chmod(path, 0700)
}
func (c *runtimeCore) prepare(id string, r *revision) error {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return err
	}
	r.ID = hex.EncodeToString(random)
	base := c.dir(id)
	parent := filepath.Join(base, "revisions")
	dir := filepath.Join(parent, r.ID)
	for _, p := range []string{base, parent, dir} {
		if err := secureDir(p); err != nil {
			return err
		}
	}
	for name, value := range map[string]any{"revision.json": r, "config.json": r.Config, "secret.json": r.Secret} {
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if err = atomicWrite(filepath.Join(dir, name), data, 0600); err != nil {
			return err
		}
	}
	if err := syncDir(parent); err != nil {
		return err
	}
	if c.interrupt != nil {
		return c.interrupt("prepared")
	}
	return nil
}
func (c *runtimeCore) commit(id string, r *revision) error {
	data, _ := json.Marshal(map[string]string{"revision": r.ID})
	if err := atomicWrite(filepath.Join(c.dir(id), "active.json"), data, 0600); err != nil {
		return err
	}
	c.mu.Lock()
	c.slots[id].active = r
	c.mu.Unlock()
	if c.interrupt != nil {
		if err := c.interrupt("committed"); err != nil {
			return err
		}
	}
	// Current is a projection only. Startup always chooses active.json first.
	_ = switchCurrent(c.dir(id), r.Entry.CurrentVersion)
	c.projectRegistry()
	return nil
}
func (c *runtimeCore) current(id string) (Release, error) {
	for _, r := range c.catalog.releases {
		if r.Manifest.ID == id && r.Purpose == "current" {
			return r, nil
		}
	}
	return Release{}, unavailable()
}
func (c *runtimeCore) ensureRelease(r *revision) error {
	m := r.Entry.Manifest
	pin, _, err := c.catalog.verifyArchive(m.ID, m.Version, "")
	if err != nil || pin.Manifest != m {
		return operationFailed()
	}
	base := filepath.Join(c.dir(m.ID), "releases")
	dir := filepath.Join(base, m.Version)
	if rejectSymlinkComponents(dir) != nil {
		return operationFailed()
	}
	if _, err = os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		stage, e := os.MkdirTemp(filepath.Join(c.cfg.Root, ".staging"), "release-")
		if e != nil {
			return e
		}
		defer os.RemoveAll(stage)
		if _, e = c.catalog.extractVerified(m.ID, m.Version, "", stage); e != nil {
			return e
		}
		if e = secureDir(base); e != nil {
			return e
		}
		if e = os.Rename(stage, dir); e != nil {
			return e
		}
		if e = syncDir(base); e != nil {
			return e
		}
	} else if err != nil {
		return err
	}
	var existing Manifest
	if err = strictFile(filepath.Join(dir, "plugin.json"), &existing); err != nil || existing != m {
		return operationFailed()
	}
	entry := filepath.Join(dir, m.Entrypoint)
	if rejectSymlinkComponents(entry) != nil {
		return operationFailed()
	}
	f, err := os.Open(entry)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxFileBytes {
		return operationFailed()
	}
	sum := sha256.New()
	if _, err = io.Copy(sum, f); err != nil || hex.EncodeToString(sum.Sum(nil)) != m.EntrypointSHA256 {
		return operationFailed()
	}
	if m.SchemaVersion == 2 {
		schema, err := os.ReadFile(filepath.Join(dir, m.ConfigSchemaPath))
		if err != nil || rejectSymlinkComponents(filepath.Join(dir, m.ConfigSchemaPath)) != nil || ValidateConfigSchema(schema, m) != nil {
			return operationFailed()
		}
	}
	return nil
}
func (c *runtimeCore) env(r *revision) map[string]string {
	env := map[string]string{}
	for k, v := range c.cfg.ExporterEnv {
		if k == "GOPULSE_RUNTIME_MODE" || (r.Entry.Manifest.Source == "redis" && strings.HasPrefix(k, "REDIS_")) {
			env[k] = v
		}
	}
	entry, _ := LookupOfficial(r.Entry.Manifest.ID)
	if entry.Source != "redis" {
		prefix := strings.ToUpper(entry.Source)
		env[prefix+"_EXPORTER_HTTP_HOST"] = "127.0.0.1"
		env[prefix+"_EXPORTER_HTTP_PORT"] = strconv.Itoa(entry.Port)
	}
	if adapter, ok := adapterFor(r.Entry.Manifest.ID); ok {
		for key, value := range adapter.Environment(r.Config, r.Secret) {
			env[key] = value
		}
	}
	if r.Entry.Manifest.SchemaVersion == 1 {
		delete(env, "REDIS_EXPORTER_CONNECT_TIMEOUT")
	}
	env["_GOPULSE_REVISION"] = r.ID
	return env
}
func (c *runtimeCore) health(id string) string {
	if id == PluginID {
		return c.cfg.HealthURL
	}
	entry, _ := LookupOfficial(id)
	return "http://127.0.0.1:" + strconv.Itoa(entry.Port) + "/health"
}
func (c *runtimeCore) launch(ctx context.Context, id string, r *revision, trial bool) (*runtimeProcess, error) {
	if err := c.ensureRelease(r); err != nil {
		return nil, err
	}
	rp, err := startProcess(ctx, c.dir(id), r.Entry.Manifest, c.env(r), c.health(id), c.cfg.StartupTimeout)
	if err != nil {
		return nil, err
	}
	if trial {
		timeout, cancel := context.WithTimeout(ctx, c.cfg.StartupTimeout)
		defer cancel()
		request, _ := http.NewRequestWithContext(timeout, http.MethodGet, strings.TrimSuffix(c.health(id), "/health")+"/metrics", nil)
		client := &http.Client{Timeout: c.cfg.StartupTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		response, e := client.Do(request)
		if e == nil {
			var body []byte
			body, e = io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
			response.Body.Close()
			if e == nil {
				if len(body) > 1<<20 || (c.cfg.ValidateSnapshot == nil && c.cfg.ValidateSourceSnapshot == nil) {
					e = operationFailed()
				} else {
					if c.cfg.ValidateSourceSnapshot != nil {
						e = c.cfg.ValidateSourceSnapshot(r.Entry.Manifest.Source, response.StatusCode, body)
					} else {
						e = c.cfg.ValidateSnapshot(response.StatusCode, body)
					}
				}
			}
		}
		if e != nil {
			rp.intentional.Store(true)
			_ = terminateProcess(rp.record, c.cfg.StopTimeout)
			return nil, operationFailed()
		}
	}
	return rp, nil
}
func (c *runtimeCore) stopProcess(ctx context.Context, id string) error {
	s := c.slots[id]
	if s.observer != nil {
		bounded, cancel := context.WithTimeout(ctx, c.cfg.StopTimeout)
		defer cancel()
		if err := s.observer.Disable(bounded); err != nil {
			return operationFailed()
		}
	}
	if s.process != nil {
		p := s.process
		p.intentional.Store(true)
		if ownsProcess(p.record) {
			if err := terminateProcess(p.record, c.cfg.StopTimeout); err != nil {
				return err
			}
		}
		s.process = nil
	}
	return c.cleanRecord(id)
}
func (c *runtimeCore) cleanRecord(id string) error {
	path := processRecordPath(c.dir(id))
	if rejectSymlinkComponents(path) != nil {
		return operationFailed()
	}
	record, err := loadProcessRecord(c.dir(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return operationFailed()
	}
	if ownsProcess(record) {
		// Do not trust a PID, executable path or revision copied into a volume. Verify
		// image provenance, release-relative executable and exact command marker too.
		trusted := false
		for _, pin := range c.catalog.releases {
			m := pin.Manifest
			expected := filepath.Join(c.dir(id), "releases", m.Version, m.Entrypoint)
			if m.ID == id && record.ExecutablePath == expected && record.WorkingDirectory == filepath.Dir(filepath.Dir(expected)) && record.CommandLineMarker == expected && (record.PluginID == id || (record.PluginID == "" && m.SchemaVersion == 1)) {
				if c.ensureRelease(&revision{Entry: registryEntry{Manifest: m}}) == nil {
					trusted = true
				}
				break
			}
		}
		if !trusted {
			return operationFailed()
		}
		if err = terminateProcess(record, c.cfg.StopTimeout); err != nil {
			return err
		}
	}
	return os.Remove(path)
}
func (c *runtimeCore) observe(id string, r *revision, rp *runtimeProcess) {
	s := c.slots[id]
	s.process = rp
	c.setStatus(id, r, ObservedRunning, "")
	if s.observer != nil {
		s.observer.Enable(r.Entry.Manifest)
	}
	go func() {
		<-rp.done
		if rp.intentional.Load() {
			return
		}
		slot, err := c.lock(context.Background(), id)
		if err != nil {
			return
		}
		defer unlock(slot)
		if slot.process != rp || rp.intentional.Load() {
			return
		}
		slot.process = nil
		if slot.observer != nil {
			ctx, cancel := context.WithTimeout(context.Background(), c.cfg.StopTimeout)
			_ = slot.observer.Disable(ctx)
			cancel()
		}
		c.setStatus(id, r, ObservedFailed, "process_exited")
	}()
}
func (c *runtimeCore) restore(ctx context.Context, id string, r *revision) error {
	if err := c.ensureRelease(r); err != nil {
		return err
	}
	if err := c.cleanRecord(id); err != nil {
		return err
	}
	if r.Entry.DesiredState == DesiredStopped {
		c.setStatus(id, r, ObservedStopped, "")
		return nil
	}
	rp, err := c.launch(ctx, id, r, false)
	if err != nil {
		return err
	}
	c.observe(id, r, rp)
	return nil
}
func (c *runtimeCore) transact(ctx context.Context, id string, next *revision) (Status, error) {
	s := c.slots[id]
	old := s.active
	if err := c.ensureRelease(next); err != nil {
		return Status{}, err
	}
	if err := c.prepare(id, next); err != nil {
		return Status{}, err
	}
	if err := c.stopProcess(ctx, id); err != nil {
		return Status{}, err
	}
	rp, err := c.launch(ctx, id, next, true)
	if err == nil && next.Entry.DesiredState == DesiredStopped {
		rp.intentional.Store(true)
		err = terminateProcess(rp.record, c.cfg.StopTimeout)
		rp = nil
	}
	if err != nil {
		_ = c.cleanRecord(id)
		if old != nil {
			if restoreErr := c.restore(context.Background(), id, old); restoreErr != nil {
				c.setStatus(id, old, ObservedFailed, "recovery_failed")
			}
		}
		_ = os.RemoveAll(filepath.Join(c.dir(id), "revisions", next.ID))
		return Status{}, NewError(CodeFailed, "plugin operation failed")
	}
	if err = c.commit(id, next); err != nil {
		if rp != nil {
			rp.intentional.Store(true)
			_ = terminateProcess(rp.record, c.cfg.StopTimeout)
		}
		// Resolve an ambiguous fsync/interruption by reading the sole commit fact.
		committed, e := c.loadActive(id)
		if e == nil {
			c.mu.Lock()
			s.active = committed
			c.mu.Unlock()
			_ = c.restore(context.Background(), id, committed)
		} else if old != nil {
			_ = c.restore(context.Background(), id, old)
		}
		return Status{}, operationFailed()
	}
	if rp != nil {
		c.observe(id, next, rp)
	} else {
		_ = c.cleanRecord(id)
		c.setStatus(id, next, ObservedStopped, "")
	}
	if c.cfg.EventRecorder != nil {
		kind, previous, before := "exporter_plugin_installed", "", "not_installed"
		if old != nil {
			kind, previous, before = "exporter_plugin_updated", old.Entry.CurrentVersion, string(old.Entry.DesiredState)
		}
		if old == nil || old.Entry.CurrentVersion != next.Entry.CurrentVersion {
			event := events.New(kind, next.Entry.CurrentVersion, previous, before, string(next.Entry.DesiredState), c.cfg.Now())
			event.Metadata.PluginID = id
			c.cfg.EventRecorder.Record(event)
		}
	}
	return c.get(id)
}
func (c *runtimeCore) parseRequest(id string, data []byte, previous json.RawMessage) (json.RawMessage, json.RawMessage, error) {
	mode := c.cfg.ExporterEnv["GOPULSE_RUNTIME_MODE"]
	if mode == "" {
		mode = "host"
	}
	adapter, ok := adapterFor(id)
	if !ok {
		return nil, nil, unavailable()
	}
	return adapter.Parse(data, mode, previous)
}
func configRequest(cfg, secret any) []byte {
	data, _ := json.Marshal(map[string]any{"config": cfg, "secrets": secret})
	return data
}
func (c *runtimeCore) configure(ctx context.Context, id string, data []byte, install bool) (Status, error) {
	s, err := c.lock(ctx, id)
	if err != nil {
		return Status{}, err
	}
	defer unlock(s)
	if _, ok := adapterFor(id); !ok {
		return Status{}, unavailable()
	}
	if install && s.active != nil {
		return Status{}, NewError(CodeConflict, "plugin is already installed")
	}
	var next revision
	var previous json.RawMessage
	if install {
		pin, e := c.current(id)
		if e != nil {
			return Status{}, e
		}
		now := c.cfg.Now().UTC()
		next.Entry = registryEntry{Manifest: pin.Manifest, CurrentVersion: pin.Manifest.Version, DesiredState: DesiredRunning, InstalledAt: now, UpdatedAt: now}
	} else {
		if s.active == nil {
			return Status{}, unavailable()
		}
		if s.active.Entry.Manifest.SchemaVersion == 1 {
			return Status{}, NewError("upgrade_required", "plugin upgrade is required")
		}
		next = *s.active
		previous = s.active.Secret
		next.Entry.UpdatedAt = c.cfg.Now().UTC()
	}
	next.Config, next.Secret, err = c.parseRequest(id, data, previous)
	if err != nil {
		return Status{}, err
	}
	return c.transact(ctx, id, &next)
}
func (c *runtimeCore) selectUpload(id, path string) (Release, error) {
	digest, err := archiveDigest(path)
	if err != nil {
		return Release{}, err
	}
	for _, r := range c.catalog.releases {
		if r.Manifest.ID == id && r.Manifest.SchemaVersion == 2 && r.ArchiveSHA256 == digest {
			if _, _, err = c.catalog.verifyArchive(id, r.Manifest.Version, path); err == nil {
				return r, nil
			}
		}
	}
	return Release{}, NewError(CodePackageInvalid, "plugin package is invalid")
}
func (c *runtimeCore) update(ctx context.Context, id, path string) (Status, error) {
	s, err := c.lock(ctx, id)
	if err != nil {
		return Status{}, err
	}
	defer unlock(s)
	if s.active == nil {
		return Status{}, unavailable()
	}
	pin, err := c.selectUpload(id, path)
	if err != nil {
		return Status{}, err
	}
	comparison, err := CompareSemver(pin.Manifest.Version, s.active.Entry.CurrentVersion)
	if err != nil || comparison <= 0 {
		return Status{}, NewError(CodeConflict, "plugin update version must be newer")
	}
	next := *s.active
	next.Entry.Manifest = pin.Manifest
	next.Entry.CurrentVersion = pin.Manifest.Version
	next.Entry.UpdatedAt = c.cfg.Now().UTC()
	if _, _, err = c.parseRequest(id, configRequest(next.Config, next.Secret), nil); err != nil {
		return Status{}, err
	}
	return c.transact(ctx, id, &next)
}
func (c *runtimeCore) setDesired(ctx context.Context, id string, desired DesiredState) (Status, error) {
	slot, err := c.lock(ctx, id)
	if err != nil {
		return Status{}, err
	}
	defer unlock(slot)
	old := slot.active
	if old == nil {
		return Status{}, unavailable()
	}
	if old.Entry.DesiredState == desired {
		if desired == DesiredStopped {
			if err = c.stopProcess(ctx, id); err != nil {
				return Status{}, err
			}
			c.setStatus(id, old, ObservedStopped, "")
		} else if slot.process == nil {
			if err = c.restore(ctx, id, old); err != nil {
				return Status{}, err
			}
		}
		return c.get(id)
	}
	next := *old
	next.Entry.DesiredState = desired
	next.Entry.UpdatedAt = c.cfg.Now().UTC()
	if err = c.prepare(id, &next); err != nil {
		return Status{}, err
	}
	var rp *runtimeProcess
	if desired == DesiredStopped {
		err = c.stopProcess(ctx, id)
	} else {
		rp, err = c.launch(ctx, id, &next, false)
	}
	if err == nil {
		err = c.commit(id, &next)
	}
	if err != nil {
		if rp != nil {
			rp.intentional.Store(true)
			_ = terminateProcess(rp.record, c.cfg.StopTimeout)
		}
		active, readErr := c.loadActive(id)
		if readErr != nil {
			active = old
		}
		c.mu.Lock()
		slot.active = active
		c.mu.Unlock()
		if c.restore(context.Background(), id, active) != nil {
			c.setStatus(id, active, ObservedFailed, "recovery_failed")
		}
		return Status{}, operationFailed()
	}
	if rp != nil {
		c.observe(id, &next, rp)
	} else {
		c.setStatus(id, &next, ObservedStopped, "")
	}
	if c.cfg.EventRecorder != nil {
		kind := "exporter_plugin_stopped"
		if desired == DesiredRunning {
			kind = "exporter_plugin_started"
		}
		event := events.New(kind, next.Entry.CurrentVersion, "", string(old.Entry.DesiredState), string(desired), c.cfg.Now())
		event.Metadata.PluginID = id
		c.cfg.EventRecorder.Record(event)
	}
	return c.get(id)
}
func (c *runtimeCore) legacyConfig() (RedisConfig, RedisSecret) {
	e := c.cfg.ExporterEnv
	p, _ := strconv.Atoi(e["REDIS_PORT"])
	db, _ := strconv.Atoi(e["REDIS_DB"])
	scrape := e["REDIS_EXPORTER_SCRAPE_TIMEOUT"]
	if scrape == "" {
		scrape = "2s"
	}
	return RedisConfig{Host: e["REDIS_HOST"], Port: p, Database: db, ConnectTimeout: scrape, ScrapeTimeout: scrape}, RedisSecret{Password: e["REDIS_PASSWORD"]}
}
func (c *runtimeCore) migrate(ctx context.Context, id string, entry registryEntry) (*revision, error) {
	if !validReleaseManifest(entry.Manifest) || entry.Manifest.SchemaVersion != 1 || entry.Manifest.ID != id || entry.CurrentVersion != entry.Manifest.Version || (entry.DesiredState != DesiredRunning && entry.DesiredState != DesiredStopped) {
		return nil, operationFailed()
	}
	r := &revision{Entry: entry}
	legacyConfig, legacySecret := c.legacyConfig()
	r.Config, _ = json.Marshal(legacyConfig)
	r.Secret, _ = json.Marshal(legacySecret)
	version, err := readCurrent(c.dir(id))
	if err != nil || version != entry.CurrentVersion {
		return r, operationFailed()
	}
	if err = c.ensureRelease(r); err != nil {
		return r, err
	}
	// Preserve the original registry before changing any compatibility projection.
	backup := filepath.Join(c.cfg.Root, "legacy-v1-registry.json")
	if _, err = os.Lstat(backup); errors.Is(err, os.ErrNotExist) {
		raw, e := os.ReadFile(filepath.Join(c.cfg.Root, "registry.json"))
		if e != nil {
			return r, e
		}
		if e = atomicWrite(backup, raw, 0400); e != nil {
			return r, e
		}
	}
	if err = c.prepare(id, r); err != nil {
		return r, err
	}
	if err = c.cleanRecord(id); err != nil {
		return r, err
	}
	var rp *runtimeProcess
	if entry.DesiredState == DesiredRunning {
		rp, err = c.launch(ctx, id, r, true)
		if err != nil {
			return r, err
		}
	}
	if err = c.commit(id, r); err != nil {
		if rp != nil {
			rp.intentional.Store(true)
			_ = terminateProcess(rp.record, c.cfg.StopTimeout)
		}
		return r, err
	}
	if rp != nil {
		c.observe(id, r, rp)
	} else {
		c.setStatus(id, r, ObservedStopped, "")
	}
	return r, nil
}
func (c *runtimeCore) bootstrap(ctx context.Context) (Status, error) {
	if s, err := c.get(PluginID); err == nil {
		return s, nil
	} else if pe, ok := AsError(err); !ok || pe.Code != CodeNotFound {
		return Status{}, err
	}
	cfg, secret := c.legacyConfig()
	return c.configure(ctx, PluginID, configRequest(cfg, secret), true)
}
func (c *runtimeCore) installAlias(ctx context.Context, path string) (Status, error) {
	if _, err := c.get(PluginID); err == nil {
		return Status{}, NewError(CodeConflict, "plugin is already installed")
	}
	pin, err := c.selectUpload(PluginID, path)
	if err != nil {
		return Status{}, err
	}
	current, err := c.current(PluginID)
	if err != nil || pin.Manifest != current.Manifest {
		return Status{}, NewError(CodePackageInvalid, "plugin package is invalid")
	}
	return c.bootstrap(ctx)
}
func (c *runtimeCore) attach(id string, observer MetricsLifecycle) {
	s, err := c.lock(context.Background(), id)
	if err != nil {
		return
	}
	defer unlock(s)
	s.observer = observer
	if s.process != nil && observer != nil {
		observer.Enable(s.active.Entry.Manifest)
	}
}
func (c *runtimeCore) recordMetrics(id string, scrape, success *time.Time, code, message string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.slots[id]
	if s == nil || s.active == nil {
		return
	}
	if scrape != nil {
		s.status.LastScrapeAt = scrape
	}
	if success != nil {
		s.status.LastSuccessAt = success
	}
	if code == "" {
		s.status.LastError = nil
	} else {
		s.status.LastError = &SafeError{Code: code, Message: message, At: c.cfg.Now().UTC()}
	}
}
func (c *runtimeCore) shutdown(ctx context.Context) error {
	var failures []error
	for _, item := range OfficialCatalog() {
		s, err := c.lock(ctx, item.ID)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		err = c.stopProcess(ctx, item.ID)
		unlock(s)
		if err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (m *Manager) Configure(ctx context.Context, id string, data []byte, install bool) (Status, error) {
	if m.core == nil {
		return Status{}, operationFailed()
	}
	return m.core.configure(ctx, id, data, install)
}
func (m *Manager) ConnectionTest(ctx context.Context, id string, data []byte) error {
	if m.core == nil || (id != PluginID && id != "mysql-exporter" && id != "rabbitmq-exporter" && id != "kafka-exporter" && id != "elasticsearch-exporter" && id != "victoriametrics-exporter") {
		return unavailable()
	}
	c := m.core
	cfg, secret, err := c.parseRequest(id, data, nil)
	if err != nil {
		return err
	}
	pin, err := c.current(id)
	if err != nil {
		return err
	}
	// Extract only into an OS temporary directory, not the plugin volume. No
	// registry/release/revision/process record is created, even on cancellation.
	stage, err := os.MkdirTemp("", "gopulse-connection-check-")
	if err != nil {
		return operationFailed()
	}
	defer os.RemoveAll(stage)
	if _, err = c.catalog.extractVerified(id, pin.Manifest.Version, "", stage); err != nil {
		return err
	}
	timeout, cancel := context.WithTimeout(ctx, c.cfg.StartupTimeout)
	defer cancel()
	cmd := exec.CommandContext(timeout, filepath.Join(stage, pin.Manifest.Entrypoint), "--check")
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	for key, value := range c.env(&revision{Entry: registryEntry{Manifest: pin.Manifest}, Config: cfg, Secret: secret}) {
		if key != "_GOPULSE_REVISION" {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = time.Second
	if cmd.Run() != nil {
		return NewError(CodeFailed, "plugin operation failed")
	}
	return nil
}

func (c *runtimeCore) discardCandidates(id, active string) {
	root := filepath.Join(c.dir(id), "revisions")
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.Name() != active && entry.IsDir() && len(entry.Name()) == 32 && strings.Trim(entry.Name(), "0123456789abcdef") == "" {
			_ = os.RemoveAll(filepath.Join(root, entry.Name()))
		}
	}
}

func (c *runtimeCore) projectRegistry() {
	c.mu.RLock()
	defer c.mu.RUnlock()
	registry := registryFile{Plugins: map[string]registryEntry{}}
	for id, slot := range c.slots {
		if slot.active != nil {
			registry.Plugins[id] = slot.active.Entry
		}
	}
	if rejectSymlinkComponents(filepath.Join(c.cfg.Root, "registry.json")) == nil {
		_ = saveRegistry(c.cfg.Root, registry)
	}
}

package plugin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/Ray-ymq/GoPulse/monitor/internal/events"
)

type ManagerConfig struct {
	Root           string
	ExporterEnv    map[string]string
	HealthURL      string
	StartupTimeout time.Duration
	StopTimeout    time.Duration
	Now            func() time.Time
}
type Manager struct {
	cfg             ManagerConfig
	mu              sync.RWMutex
	registry        registryFile
	states          map[string]Status
	runtimes        map[string]*runtimeProcess
	operation       chan struct{}
	observer        MetricsLifecycle
	rootIdentity    storageIdentity
	persistRegistry func(registryFile) error
	eventRecorder   EventRecorder
}

func NewManager(ctx context.Context, cfg ManagerConfig) (*Manager, error) {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if err := validateHealthPort(cfg.ExporterEnv); err != nil {
		return nil, err
	}
	root, rootIdentity, err := ensureRoot(cfg.Root)
	if err != nil {
		return nil, err
	}
	cfg.Root = root
	reg, err := loadRegistry(cfg.Root)
	if err != nil {
		return nil, err
	}
	m := &Manager{cfg: cfg, registry: reg, states: map[string]Status{}, runtimes: map[string]*runtimeProcess{}, operation: make(chan struct{}, 1), rootIdentity: rootIdentity, eventRecorder: discardEventRecorder{}}
	m.persistRegistry = func(registry registryFile) error { return saveRegistry(m.cfg.Root, registry) }
	for id, entry := range reg.Plugins {
		m.states[id] = statusFromEntry(entry, ObservedStopped, nil)
	}
	if err = validateStorage(m.cfg.Root, m.rootIdentity); err != nil {
		return nil, err
	}
	if err = m.recover(ctx); err != nil {
		return nil, err
	}
	return m, nil
}

type EventRecorder interface {
	Record(events.Event) bool
}

type discardEventRecorder struct{}

func (discardEventRecorder) Record(events.Event) bool { return true }

func (m *Manager) AttachEvents(recorder EventRecorder) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if recorder == nil {
		m.eventRecorder = discardEventRecorder{}
		return
	}
	m.eventRecorder = recorder
}

func (m *Manager) recordEvent(event events.Event) {
	m.mu.RLock()
	recorder := m.eventRecorder
	m.mu.RUnlock()
	_ = recorder.Record(event)
}

func statusFromEntry(e registryEntry, observed ObservedState, last *SafeError) Status {
	return Status{ID: e.Manifest.ID, Name: e.Manifest.Name, Version: e.CurrentVersion, Kind: e.Manifest.Kind, Source: e.Manifest.Source, DesiredState: e.DesiredState, ObservedState: observed, InstalledAt: e.InstalledAt, UpdatedAt: e.UpdatedAt, LastError: last}
}
func preserveMetrics(next, previous Status) Status {
	next.LastScrapeAt = previous.LastScrapeAt
	next.LastSuccessAt = previous.LastSuccessAt
	return next
}
func (m *Manager) AttachMetrics(observer MetricsLifecycle) {
	m.mu.Lock()
	m.observer = observer
	entry, installed := m.registry.Plugins[PluginID]
	state := m.states[PluginID]
	m.mu.Unlock()
	if observer != nil && installed && state.ObservedState == ObservedRunning {
		observer.Enable(entry.Manifest)
	}
}
func (m *Manager) enableMetrics(manifest Manifest) {
	m.mu.RLock()
	observer := m.observer
	m.mu.RUnlock()
	if observer != nil {
		observer.Enable(manifest)
	}
}
func (m *Manager) disableMetrics(ctx context.Context) error {
	m.mu.RLock()
	observer := m.observer
	m.mu.RUnlock()
	if observer == nil {
		return nil
	}
	return observer.Disable(ctx)
}

func (m *Manager) disableMetricsForOperation() error {
	ctx, cancel := context.WithTimeout(context.Background(), m.cfg.StopTimeout)
	defer cancel()
	return m.disableMetrics(ctx)
}

func cloneRegistry(source registryFile) registryFile {
	clone := registryFile{Plugins: make(map[string]registryEntry, len(source.Plugins))}
	for id, entry := range source.Plugins {
		clone.Plugins[id] = entry
	}
	return clone
}
func (m *Manager) RecordMetrics(scrapeAt, successAt *time.Time, code, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	status, ok := m.states[PluginID]
	if !ok {
		return
	}
	if scrapeAt != nil {
		value := scrapeAt.UTC()
		status.LastScrapeAt = &value
	}
	if successAt != nil {
		value := successAt.UTC()
		status.LastSuccessAt = &value
	}
	if code == "" {
		status.LastError = nil
	} else {
		status.LastError = &SafeError{Code: code, Message: message, At: m.cfg.Now().UTC()}
	}
	m.states[PluginID] = status
}
func (m *Manager) validateStorageBoundary() error {
	if err := validateStorage(m.cfg.Root, m.rootIdentity); err != nil {
		return err
	}
	entry, installed := m.registry.Plugins[PluginID]
	if !installed {
		return nil
	}
	version, err := readCurrent(m.pluginDir())
	if err != nil || version != entry.CurrentVersion {
		return errors.New("plugin current link does not match the registry")
	}
	return nil
}

func (m *Manager) safeRemove(path string) {
	if validateStorage(m.cfg.Root, m.rootIdentity) == nil {
		_ = os.Remove(path)
	}
}

func (m *Manager) safeRemoveAll(path string) {
	if validateStorage(m.cfg.Root, m.rootIdentity) == nil {
		_ = os.RemoveAll(path)
	}
}

func (m *Manager) safeRemoveProcessRecord() {
	m.safeRemove(processRecordPath(m.pluginDir()))
}

func (m *Manager) begin() error {
	select {
	case m.operation <- struct{}{}:
		if err := m.validateStorageBoundary(); err != nil {
			<-m.operation
			return NewError(CodeFailed, "plugin storage boundary validation failed")
		}
		return nil
	default:
		return NewError(CodeInProgress, "plugin operation is already in progress")
	}
}
func (m *Manager) end() { <-m.operation }
func (m *Manager) List() []Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Status, 0, len(m.states))
	for _, s := range m.states {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func (m *Manager) Get(id string) (Status, error) {
	if id != PluginID {
		return Status{}, NewError(CodeNotFound, "plugin was not found")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.states[id]
	if !ok {
		return Status{}, NewError(CodeNotFound, "plugin was not found")
	}
	return s, nil
}
func (m *Manager) pluginDir() string { return filepath.Join(m.cfg.Root, PluginID) }
func (m *Manager) Install(ctx context.Context, archivePath string) (Status, error) {
	if err := m.begin(); err != nil {
		return Status{}, err
	}
	defer m.end()
	m.mu.RLock()
	_, exists := m.registry.Plugins[PluginID]
	m.mu.RUnlock()
	if exists {
		return Status{}, NewError(CodeConflict, "plugin is already installed")
	}
	return m.installNew(ctx, archivePath)
}
func (m *Manager) installNew(ctx context.Context, archivePath string) (Status, error) {
	stage, err := os.MkdirTemp(filepath.Join(m.cfg.Root, ".staging"), "install-")
	if err != nil {
		return Status{}, wrap(CodeFailed, "plugin operation failed", err)
	}
	defer m.safeRemoveAll(stage)
	manifest, err := extractPackage(archivePath, stage)
	if err != nil {
		return Status{}, err
	}
	if err = m.validateStorageBoundary(); err != nil {
		return Status{}, NewError(CodeFailed, "plugin storage boundary validation failed")
	}
	pluginDir := m.pluginDir()
	releaseDir := filepath.Join(pluginDir, "releases", manifest.Version)
	if _, e := os.Stat(releaseDir); e == nil {
		return Status{}, NewError(CodeConflict, "plugin version is already installed")
	}
	if err = os.MkdirAll(filepath.Dir(releaseDir), 0750); err != nil {
		return Status{}, wrap(CodeFailed, "plugin operation failed", err)
	}
	if err = os.Rename(stage, releaseDir); err != nil {
		return Status{}, wrap(CodeFailed, "plugin operation failed", err)
	}
	now := m.cfg.Now().UTC()
	entry := registryEntry{Manifest: manifest, CurrentVersion: manifest.Version, DesiredState: DesiredRunning, InstalledAt: now, UpdatedAt: now}
	cleanup := func() {
		m.safeRemoveProcessRecord()
		m.safeRemove(filepath.Join(pluginDir, "current"))
		m.safeRemoveAll(releaseDir)
		m.safeRemove(filepath.Join(m.cfg.Root, "registry.json"))
	}
	if err = switchCurrent(pluginDir, manifest.Version); err != nil {
		cleanup()
		return Status{}, wrap(CodeFailed, "plugin operation failed", err)
	}
	reg := registryFile{Plugins: map[string]registryEntry{PluginID: entry}}
	if err = m.persistRegistry(reg); err != nil {
		cleanup()
		return Status{}, wrap(CodeFailed, "plugin operation failed", err)
	}
	rp, err := startProcess(ctx, pluginDir, manifest, m.cfg.ExporterEnv, m.cfg.HealthURL, m.cfg.StartupTimeout)
	if err != nil {
		cleanup()
		return Status{}, NewError(CodeFailed, "plugin failed to start")
	}
	started := m.cfg.Now().UTC()
	status := statusFromEntry(entry, ObservedRunning, nil)
	status.StartedAt = &started
	m.mu.Lock()
	m.registry = reg
	m.states[PluginID] = status
	m.runtimes[PluginID] = rp
	m.mu.Unlock()
	m.watch(PluginID, rp)
	m.enableMetrics(manifest)
	m.recordEvent(events.New("exporter_plugin_installed", manifest.Version, "", "not_installed", "running", m.cfg.Now()))
	return status, nil
}
func (m *Manager) Start(ctx context.Context, id string) (Status, error) {
	if id != PluginID {
		return Status{}, NewError(CodeNotFound, "plugin was not found")
	}
	if err := m.begin(); err != nil {
		return Status{}, err
	}
	defer m.end()
	m.mu.Lock()
	entry, ok := m.registry.Plugins[id]
	previous := m.states[id]
	if !ok {
		m.mu.Unlock()
		return Status{}, NewError(CodeNotFound, "plugin was not found")
	}
	if state := m.states[id]; state.ObservedState == ObservedRunning && m.runtimeOwnedLocked(id) {
		m.mu.Unlock()
		return state, nil
	}
	entry.DesiredState = DesiredRunning
	entry.UpdatedAt = m.cfg.Now().UTC()
	m.registry.Plugins[id] = entry
	m.states[id] = preserveMetrics(statusFromEntry(entry, ObservedStarting, nil), previous)
	reg := m.registry
	m.mu.Unlock()
	if err := m.persistRegistry(reg); err != nil {
		return Status{}, wrap(CodeFailed, "plugin operation failed", err)
	}
	rp, err := startProcess(ctx, m.pluginDir(), entry.Manifest, m.cfg.ExporterEnv, m.cfg.HealthURL, m.cfg.StartupTimeout)
	if err != nil {
		failure := &SafeError{Code: "start_failed", Message: "plugin failed to start", At: m.cfg.Now().UTC()}
		m.mu.Lock()
		m.states[id] = preserveMetrics(statusFromEntry(entry, ObservedFailed, failure), previous)
		m.mu.Unlock()
		return Status{}, NewError(CodeFailed, "plugin failed to start")
	}
	started := m.cfg.Now().UTC()
	s := preserveMetrics(statusFromEntry(entry, ObservedRunning, nil), previous)
	s.StartedAt = &started
	m.mu.Lock()
	m.states[id] = s
	m.runtimes[id] = rp
	m.mu.Unlock()
	m.watch(id, rp)
	m.enableMetrics(entry.Manifest)
	m.recordEvent(events.New("exporter_plugin_started", entry.CurrentVersion, "", eventState(previous.ObservedState), "running", m.cfg.Now()))
	return s, nil
}

func eventState(state ObservedState) string {
	switch state {
	case ObservedRunning:
		return "running"
	case ObservedFailed:
		return "failed"
	default:
		return "stopped"
	}
}

func (m *Manager) runtimeOwnedLocked(id string) bool {
	rp := m.runtimes[id]
	return rp != nil && ownsProcess(rp.record)
}
func (m *Manager) Stop(ctx context.Context, id string) (Status, error) {
	if id != PluginID {
		return Status{}, NewError(CodeNotFound, "plugin was not found")
	}
	if err := m.begin(); err != nil {
		return Status{}, err
	}
	defer m.end()
	if err := m.disableMetricsForOperation(); err != nil {
		return Status{}, NewError(CodeFailed, "metrics collection could not be stopped")
	}
	if err := m.validateStorageBoundary(); err != nil {
		return Status{}, NewError(CodeFailed, "plugin storage boundary validation failed")
	}
	m.mu.Lock()
	entry, ok := m.registry.Plugins[id]
	previous := m.states[id]
	if !ok {
		m.mu.Unlock()
		return Status{}, NewError(CodeNotFound, "plugin was not found")
	}
	entry.DesiredState = DesiredStopped
	entry.UpdatedAt = m.cfg.Now().UTC()
	m.registry.Plugins[id] = entry
	reg := m.registry
	rp := m.runtimes[id]
	m.states[id] = preserveMetrics(statusFromEntry(entry, ObservedStopping, nil), previous)
	m.mu.Unlock()
	if err := m.persistRegistry(reg); err != nil {
		return Status{}, wrap(CodeFailed, "plugin operation failed", err)
	}
	var record processRecord
	if rp != nil {
		rp.intentional.Store(true)
		record = rp.record
	} else {
		record, _ = loadProcessRecord(m.pluginDir())
	}
	if record.PID > 0 && ownsProcess(record) {
		if err := terminateProcess(record, m.cfg.StopTimeout); err != nil {
			return Status{}, NewError(CodeFailed, "plugin process ownership could not be verified")
		}
	} else if record.PID > 0 {
		return Status{}, NewError(CodeFailed, "plugin process ownership could not be verified")
	}
	m.safeRemoveProcessRecord()
	s := preserveMetrics(statusFromEntry(entry, ObservedStopped, nil), previous)
	m.mu.Lock()
	delete(m.runtimes, id)
	m.states[id] = s
	m.mu.Unlock()
	if previous.ObservedState != ObservedStopped {
		m.recordEvent(events.New("exporter_plugin_stopped", entry.CurrentVersion, "", eventState(previous.ObservedState), "stopped", m.cfg.Now()))
	}
	return s, nil
}
func (m *Manager) Update(ctx context.Context, id, archivePath string) (Status, error) {
	if id != PluginID {
		return Status{}, NewError(CodeNotFound, "plugin was not found")
	}
	if err := m.begin(); err != nil {
		return Status{}, err
	}
	defer m.end()
	m.mu.RLock()
	oldRegistry := cloneRegistry(m.registry)
	old, ok := oldRegistry.Plugins[id]
	oldState := m.states[id]
	m.mu.RUnlock()
	if !ok {
		return Status{}, NewError(CodeNotFound, "plugin was not found")
	}
	stage, err := os.MkdirTemp(filepath.Join(m.cfg.Root, ".staging"), "update-")
	if err != nil {
		return Status{}, wrap(CodeFailed, "plugin operation failed", err)
	}
	defer m.safeRemoveAll(stage)
	manifest, err := extractPackage(archivePath, stage)
	if err != nil {
		return Status{}, err
	}
	if err = m.validateStorageBoundary(); err != nil {
		return Status{}, NewError(CodeFailed, "plugin storage boundary validation failed")
	}
	if manifest.ID != id {
		return Status{}, NewError(CodePackageInvalid, "plugin package is invalid")
	}
	cmp, _ := CompareSemver(manifest.Version, old.CurrentVersion)
	if cmp <= 0 {
		return Status{}, NewError(CodeConflict, "plugin update version must be newer")
	}
	newRelease := filepath.Join(m.pluginDir(), "releases", manifest.Version)
	if _, e := os.Stat(newRelease); e == nil {
		return Status{}, NewError(CodeConflict, "plugin version is already installed")
	}
	if err = os.Rename(stage, newRelease); err != nil {
		return Status{}, wrap(CodeFailed, "plugin operation failed", err)
	}
	rollbackRelease := true
	defer func() {
		if rollbackRelease {
			m.safeRemoveAll(newRelease)
		}
	}()
	wasRunning := old.DesiredState == DesiredRunning
	if wasRunning {
		if err = m.disableMetricsForOperation(); err != nil {
			return Status{}, NewError(CodeFailed, "metrics collection could not be stopped")
		}
		if err = m.validateStorageBoundary(); err != nil {
			return Status{}, NewError(CodeFailed, "plugin storage boundary validation failed")
		}
		if _, err = m.stopForUpdate(); err != nil {
			return Status{}, err
		}
	}
	if err = switchCurrent(m.pluginDir(), manifest.Version); err != nil {
		return Status{}, wrap(CodeFailed, "plugin operation failed", err)
	}
	updated := old
	updated.Manifest = manifest
	updated.CurrentVersion = manifest.Version
	updated.UpdatedAt = m.cfg.Now().UTC()
	m.mu.Lock()
	m.registry.Plugins[id] = updated
	m.states[id] = preserveMetrics(statusFromEntry(updated, ObservedUpdating, nil), oldState)
	reg := m.registry
	m.mu.Unlock()
	if err = m.persistRegistry(reg); err != nil {
		storageOK, rollbackOK := m.rollbackUpdate(id, oldRegistry, oldState, wasRunning)
		if !storageOK {
			rollbackRelease = false
		}
		if !rollbackOK {
			return Status{}, NewError(CodeFailed, "plugin update failed and rollback could not be completed")
		}
		return Status{}, wrap(CodeFailed, "plugin operation failed", err)
	}
	if !wasRunning {
		rollbackRelease = false
		s := preserveMetrics(statusFromEntry(updated, ObservedStopped, nil), oldState)
		m.mu.Lock()
		m.states[id] = s
		m.mu.Unlock()
		m.recordEvent(events.New("exporter_plugin_updated", manifest.Version, old.CurrentVersion, "stopped", "stopped", m.cfg.Now()))
		return s, nil
	}
	rp, startErr := startProcess(ctx, m.pluginDir(), manifest, m.cfg.ExporterEnv, m.cfg.HealthURL, m.cfg.StartupTimeout)
	if startErr == nil {
		rollbackRelease = false
		started := m.cfg.Now().UTC()
		s := preserveMetrics(statusFromEntry(updated, ObservedRunning, nil), oldState)
		s.StartedAt = &started
		m.mu.Lock()
		m.states[id] = s
		m.runtimes[id] = rp
		m.mu.Unlock()
		m.watch(id, rp)
		m.enableMetrics(manifest)
		m.recordEvent(events.New("exporter_plugin_updated", manifest.Version, old.CurrentVersion, "running", "running", m.cfg.Now()))
		return s, nil
	}
	storageOK, rollbackOK := m.rollbackUpdate(id, oldRegistry, oldState, true)
	if !storageOK {
		rollbackRelease = false
	}
	if !rollbackOK {
		return Status{}, NewError(CodeFailed, "plugin update failed and rollback could not be completed")
	}
	return Status{}, NewError(CodeFailed, "plugin update failed and was rolled back")
}

func (m *Manager) rollbackUpdate(id string, oldRegistry registryFile, oldState Status, restart bool) (bool, bool) {
	old := oldRegistry.Plugins[id]
	if err := validateStorage(m.cfg.Root, m.rootIdentity); err != nil {
		failure := &SafeError{Code: "rollback_failed", Message: "plugin update rollback requires repair", At: m.cfg.Now().UTC()}
		m.mu.Lock()
		m.registry = cloneRegistry(oldRegistry)
		delete(m.runtimes, id)
		m.states[id] = preserveMetrics(statusFromEntry(old, ObservedFailed, failure), oldState)
		m.mu.Unlock()
		return false, false
	}
	switchErr := switchCurrent(m.pluginDir(), old.CurrentVersion)
	saveErr := m.persistRegistry(oldRegistry)
	m.mu.Lock()
	m.registry = cloneRegistry(oldRegistry)
	delete(m.runtimes, id)
	m.mu.Unlock()
	if switchErr != nil || saveErr != nil {
		failure := &SafeError{Code: "rollback_failed", Message: "plugin update rollback requires repair", At: m.cfg.Now().UTC()}
		m.mu.Lock()
		m.states[id] = preserveMetrics(statusFromEntry(old, ObservedFailed, failure), oldState)
		m.mu.Unlock()
		return false, false
	}
	if !restart {
		restored := preserveMetrics(statusFromEntry(old, ObservedStopped, &SafeError{Code: "update_failed", Message: "plugin update failed and was rolled back", At: m.cfg.Now().UTC()}), oldState)
		m.mu.Lock()
		m.states[id] = restored
		m.mu.Unlock()
		return true, true
	}
	oldRP, restartErr := startProcess(context.Background(), m.pluginDir(), old.Manifest, m.cfg.ExporterEnv, m.cfg.HealthURL, m.cfg.StartupTimeout)
	if restartErr != nil {
		failure := &SafeError{Code: "rollback_failed", Message: "plugin update rollback could not restart the previous version", At: m.cfg.Now().UTC()}
		m.mu.Lock()
		m.states[id] = preserveMetrics(statusFromEntry(old, ObservedFailed, failure), oldState)
		m.mu.Unlock()
		return true, false
	}
	started := m.cfg.Now().UTC()
	restored := preserveMetrics(statusFromEntry(old, ObservedRunning, &SafeError{Code: "update_failed", Message: "plugin update failed and was rolled back", At: m.cfg.Now().UTC()}), oldState)
	restored.StartedAt = &started
	m.mu.Lock()
	m.states[id] = restored
	m.runtimes[id] = oldRP
	m.mu.Unlock()
	m.watch(id, oldRP)
	m.enableMetrics(old.Manifest)
	return true, true
}
func (m *Manager) stopForUpdate() (Status, error) {
	m.mu.RLock()
	rp := m.runtimes[PluginID]
	entry := m.registry.Plugins[PluginID]
	m.mu.RUnlock()
	var record processRecord
	if rp != nil {
		rp.intentional.Store(true)
		record = rp.record
	} else {
		record, _ = loadProcessRecord(m.pluginDir())
	}
	if record.PID > 0 {
		if !ownsProcess(record) {
			return Status{}, NewError(CodeFailed, "plugin process ownership could not be verified")
		}
		if err := terminateProcess(record, m.cfg.StopTimeout); err != nil {
			return Status{}, NewError(CodeFailed, "plugin process could not be stopped")
		}
	}
	m.safeRemoveProcessRecord()
	m.mu.Lock()
	delete(m.runtimes, PluginID)
	m.mu.Unlock()
	return statusFromEntry(entry, ObservedStopped, nil), nil
}
func (m *Manager) recover(ctx context.Context) error {
	for id, entry := range m.registry.Plugins {
		if id != PluginID {
			continue
		}
		version, err := readCurrent(m.pluginDir())
		if err != nil || version != entry.CurrentVersion {
			failure := &SafeError{Code: "recovery_invalid", Message: "plugin installation requires repair", At: m.cfg.Now().UTC()}
			m.states[id] = statusFromEntry(entry, ObservedFailed, failure)
			continue
		}
		if record, e := loadProcessRecord(m.pluginDir()); e == nil {
			if ownsProcess(record) {
				_ = terminateProcess(record, m.cfg.StopTimeout)
			}
			m.safeRemoveProcessRecord()
		}
		if entry.DesiredState == DesiredRunning {
			rp, e := startProcess(ctx, m.pluginDir(), entry.Manifest, m.cfg.ExporterEnv, m.cfg.HealthURL, m.cfg.StartupTimeout)
			if e != nil {
				failure := &SafeError{Code: "recovery_failed", Message: "plugin failed to recover", At: m.cfg.Now().UTC()}
				m.states[id] = statusFromEntry(entry, ObservedFailed, failure)
				continue
			}
			started := m.cfg.Now().UTC()
			s := statusFromEntry(entry, ObservedRunning, nil)
			s.StartedAt = &started
			m.states[id] = s
			m.runtimes[id] = rp
			m.watch(id, rp)
		} else {
			m.states[id] = statusFromEntry(entry, ObservedStopped, nil)
		}
	}
	return nil
}
func (m *Manager) watch(id string, runtime *runtimeProcess) {
	go func() {
		_, ok := <-runtime.done
		if !ok || runtime.intentional.Load() {
			return
		}
		m.safeRemoveProcessRecord()
		_ = m.disableMetrics(context.Background())
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.runtimes[id] != runtime {
			return
		}
		delete(m.runtimes, id)
		entry, exists := m.registry.Plugins[id]
		if !exists {
			return
		}
		failure := &SafeError{Code: "process_exited", Message: "plugin process exited unexpectedly", At: m.cfg.Now().UTC()}
		m.states[id] = preserveMetrics(statusFromEntry(entry, ObservedFailed, failure), m.states[id])
	}()
}

func (m *Manager) Shutdown(ctx context.Context) error {
	if err := m.disableMetrics(ctx); err != nil {
		return err
	}
	m.mu.RLock()
	records := []processRecord{}
	for _, rp := range m.runtimes {
		rp.intentional.Store(true)
		records = append(records, rp.record)
	}
	m.mu.RUnlock()
	var first error
	for _, record := range records {
		if ownsProcess(record) {
			if err := terminateProcess(record, m.cfg.StopTimeout); err != nil && first == nil {
				first = fmt.Errorf("stop plugin: %w", err)
			}
		}
	}
	m.safeRemoveProcessRecord()
	return first
}

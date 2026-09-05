package plugin

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/monitor/internal/events"
)

func managerConfig(root, healthURL string) ManagerConfig {
	return ManagerConfig{
		Root: root,
		ExporterEnv: map[string]string{
			"REDIS_EXPORTER_HTTP_PORT": "19121",
		},
		HealthURL:      healthURL,
		StartupTimeout: 500 * time.Millisecond,
		StopTimeout:    time.Second,
	}
}

func writeExecutablePackage(t *testing.T, version string, executable []byte) string {
	t.Helper()
	digest := sha256.Sum256(executable)
	manifest := Manifest{SchemaVersion: 1, ID: PluginID, Name: "GoPulse Redis Exporter", Version: version, Kind: "metrics-exporter", Source: "redis", OS: "linux", Arch: runtime.GOARCH, Entrypoint: "bin/gopulse-redis-exporter", EntrypointSHA256: hex.EncodeToString(digest[:]), HealthPath: "/health", MetricsPath: "/metrics"}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "plugin.tar.gz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	entries := []struct {
		name string
		mode int64
		data []byte
	}{{"plugin.json", 0600, manifestJSON}, {"bin/gopulse-redis-exporter", 0755, executable}}
	for _, entry := range entries {
		if err = tarWriter.WriteHeader(&tar.Header{Name: entry.name, Mode: entry.mode, Size: int64(len(entry.data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err = tarWriter.Write(entry.data); err != nil {
			t.Fatal(err)
		}
	}
	if err = tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err = gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestNewManagerRejectsDangerousAndSymlinkPluginRoots(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	roots := map[string]string{"filesystem": string(filepath.Separator), "home": home, "repository": repositoryRoot()}
	for name, root := range roots {
		t.Run(name, func(t *testing.T) {
			if root == "" {
				t.Skip("root is unavailable")
			}
			if _, err := NewManager(context.Background(), managerConfig(root, "http://127.0.0.1:1/health")); err == nil {
				t.Fatalf("dangerous plugin root %q was accepted", root)
			}
		})
	}
	t.Run("root symlink", func(t *testing.T) {
		parent := t.TempDir()
		target := filepath.Join(parent, "target")
		if err := os.Mkdir(target, 0750); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(parent, "plugins")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if _, err := NewManager(context.Background(), managerConfig(link, "http://127.0.0.1:1/health")); err == nil {
			t.Fatal("symlink plugin root was accepted")
		}
	})
	t.Run("internal symlink", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "plugins")
		outside := filepath.Join(t.TempDir(), "outside")
		if err := os.MkdirAll(root, 0750); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(root, ".staging")); err != nil {
			t.Fatal(err)
		}
		if _, err := NewManager(context.Background(), managerConfig(root, "http://127.0.0.1:1/health")); err == nil {
			t.Fatal("internal symlink was accepted")
		}
		if _, err := os.Stat(outside); !os.IsNotExist(err) {
			t.Fatalf("outside target was modified: %v", err)
		}
	})
}

func TestManagerRejectsPluginRootReplacementBeforeMutation(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "plugins")
	manager, err := NewManager(context.Background(), managerConfig(root, "http://127.0.0.1:1/health"))
	if err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(parent, "plugins-original")
	outside := filepath.Join(parent, "outside")
	if err = os.Rename(root, original); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(outside, 0750); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, root); err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Install(context.Background(), filepath.Join(parent, "missing.tar.gz")); err == nil {
		t.Fatal("install accepted a replaced plugin root")
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("replacement target was modified: %v", entries)
	}
}

func TestUpdateDoubleStartFailureReportsFailedAndRestoresPersistentVersion(t *testing.T) {
	var healthAvailable atomic.Bool
	healthAvailable.Store(true)
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !healthAvailable.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","service":"redis-exporter"}`))
	}))
	defer health.Close()
	root := filepath.Join(t.TempDir(), "plugins")
	manager, err := NewManager(context.Background(), managerConfig(root, health.URL))
	if err != nil {
		t.Fatal(err)
	}
	oldExecutableBytes, err := os.ReadFile("/usr/bin/yes")
	if err != nil {
		t.Fatal(err)
	}
	oldPackage := writeExecutablePackage(t, "1.3.3", oldExecutableBytes)
	if _, err = manager.Install(context.Background(), oldPackage); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	healthAvailable.Store(false)
	failingExecutableBytes, err := os.ReadFile("/bin/false")
	if err != nil {
		t.Fatal(err)
	}
	failingPackage := writeExecutablePackage(t, "1.3.4", failingExecutableBytes)
	if _, err = manager.Update(context.Background(), PluginID, failingPackage); err == nil {
		t.Fatal("Update() succeeded despite both new start and old restart failing")
	} else {
		t.Logf("Update() error = %v", err)
	}
	status, err := manager.Get(PluginID)
	if err != nil {
		t.Fatal(err)
	}
	if status.Version != "1.3.3" || status.ObservedState != ObservedFailed || status.LastError == nil || status.LastError.Code != "rollback_failed" {
		t.Fatalf("unexpected rollback status: %#v", status)
	}
	if manager.runtimes[PluginID] != nil {
		t.Fatal("failed rollback retained a runtime")
	}
	current, err := readCurrent(filepath.Join(root, PluginID))
	if err != nil || current != "1.3.3" {
		t.Fatalf("current version = %q, %v", current, err)
	}
	registry, err := loadRegistry(root)
	if err != nil || registry.Plugins[PluginID].CurrentVersion != "1.3.3" {
		t.Fatalf("registry was not restored: %#v, %v", registry, err)
	}
}

func TestUpdateRegistryFailureRestoresMemoryCurrentAndDisk(t *testing.T) {
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","service":"redis-exporter"}`))
	}))
	defer health.Close()
	root := filepath.Join(t.TempDir(), "plugins")
	manager, err := NewManager(context.Background(), managerConfig(root, health.URL))
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.ReadFile("/usr/bin/yes")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Install(context.Background(), writeExecutablePackage(t, "1.3.3", executable)); err != nil {
		t.Fatal(err)
	}
	persist := manager.persistRegistry
	manager.persistRegistry = func(registry registryFile) error {
		if registry.Plugins[PluginID].CurrentVersion == "1.3.4" {
			return errors.New("injected registry failure")
		}
		return persist(registry)
	}
	if _, err = manager.Update(context.Background(), PluginID, writeExecutablePackage(t, "1.3.4", executable)); err == nil {
		t.Fatal("Update() succeeded despite injected registry failure")
	}
	status, err := manager.Get(PluginID)
	if err != nil {
		t.Fatal(err)
	}
	if status.Version != "1.3.3" || status.ObservedState != ObservedRunning || manager.registry.Plugins[PluginID].CurrentVersion != "1.3.3" {
		t.Fatalf("memory state was not restored: %#v, %#v", status, manager.registry)
	}
	current, err := readCurrent(filepath.Join(root, PluginID))
	if err != nil || current != "1.3.3" {
		t.Fatalf("current version = %q, %v", current, err)
	}
	registry, err := loadRegistry(root)
	if err != nil || registry.Plugins[PluginID].CurrentVersion != "1.3.3" {
		t.Fatalf("disk registry was not restored: %#v, %v", registry, err)
	}
	if err = manager.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type recordingEvents struct {
	mu     sync.Mutex
	events []events.Event
	accept bool
}

func (r *recordingEvents) Record(event events.Event) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
	return r.accept
}

func (r *recordingEvents) snapshot() []events.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]events.Event(nil), r.events...)
}

func TestManagerRecordsOnlySuccessfulLifecycleTransitions(t *testing.T) {
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","service":"redis-exporter"}`))
	}))
	defer health.Close()
	manager, err := NewManager(context.Background(), managerConfig(filepath.Join(t.TempDir(), "plugins"), health.URL))
	if err != nil {
		t.Fatal(err)
	}
	recorder := &recordingEvents{accept: false}
	manager.AttachEvents(recorder)
	executable, err := os.ReadFile("/usr/bin/yes")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Install(context.Background(), writeExecutablePackage(t, "1.7.0", executable)); err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Stop(context.Background(), PluginID); err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Stop(context.Background(), PluginID); err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Start(context.Background(), PluginID); err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Start(context.Background(), PluginID); err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Update(context.Background(), PluginID, writeExecutablePackage(t, "1.7.1", executable)); err != nil {
		t.Fatal(err)
	}
	if err = manager.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{"exporter_plugin_installed", "exporter_plugin_stopped", "exporter_plugin_started", "exporter_plugin_updated"}
	recorded := recorder.snapshot()
	if len(recorded) != len(want) {
		t.Fatalf("events=%+v", recorded)
	}
	for i, name := range want {
		if recorded[i].EventName != name {
			t.Fatalf("event %d=%s want=%s", i, recorded[i].EventName, name)
		}
	}
	if recorded[3].Metadata.PreviousPluginVersion != "1.7.0" || recorded[3].Metadata.PluginVersion != "1.7.1" {
		t.Fatalf("update metadata=%+v", recorded[3].Metadata)
	}
}

func TestManagerRecordsTerminalStartFailure(t *testing.T) {
	var healthy atomic.Bool
	healthy.Store(true)
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !healthy.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","service":"redis-exporter"}`))
	}))
	defer health.Close()
	recorder := &recordingEvents{accept: true}
	cfg := managerConfig(filepath.Join(t.TempDir(), "plugins"), health.URL)
	cfg.EventRecorder = recorder
	manager, err := NewManager(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.ReadFile("/usr/bin/yes")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Install(context.Background(), writeExecutablePackage(t, "1.7.2", executable)); err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Stop(context.Background(), PluginID); err != nil {
		t.Fatal(err)
	}
	healthy.Store(false)
	if _, err = manager.Start(context.Background(), PluginID); err == nil {
		t.Fatal("start unexpectedly succeeded")
	}
	recorded := recorder.snapshot()
	last := recorded[len(recorded)-1]
	if last.EventName != "exporter_plugin_failed" || last.Metadata.Operation != "start" || last.Metadata.ErrorCode != "start_failed" || last.Metadata.ToState != "failed" {
		t.Fatalf("unexpected failure event: %+v", last)
	}
}

func TestManagerRecordsUnexpectedExitAfterStateCommit(t *testing.T) {
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","service":"redis-exporter"}`))
	}))
	defer health.Close()
	recorder := &recordingEvents{accept: true}
	cfg := managerConfig(filepath.Join(t.TempDir(), "plugins"), health.URL)
	cfg.EventRecorder = recorder
	manager, err := NewManager(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.ReadFile("/usr/bin/yes")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Install(context.Background(), writeExecutablePackage(t, "1.7.2", executable)); err != nil {
		t.Fatal(err)
	}
	manager.mu.RLock()
	pid := manager.runtimes[PluginID].record.PID
	manager.mu.RUnlock()
	if err = syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		recorded := recorder.snapshot()
		if len(recorded) >= 2 && recorded[len(recorded)-1].EventName == "exporter_plugin_exited" {
			status, getErr := manager.Get(PluginID)
			if getErr != nil || status.ObservedState != ObservedFailed || status.LastError == nil || status.LastError.Code != "process_exited" {
				t.Fatalf("exit event preceded state commit: status=%+v err=%v", status, getErr)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("unexpected exit event not recorded: %+v", recorder.snapshot())
}

type countingMetricsLifecycle struct {
	disables atomic.Int32
}

func (*countingMetricsLifecycle) Enable(Manifest) {}
func (m *countingMetricsLifecycle) Disable(context.Context) error {
	m.disables.Add(1)
	return nil
}

func TestStaleWatcherCannotAffectReplacementRuntime(t *testing.T) {
	root := filepath.Join(t.TempDir(), "plugins")
	manager, err := NewManager(context.Background(), managerConfig(root, "http://127.0.0.1:1/health"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{ID: PluginID, Version: "1.7.4"}
	entry := registryEntry{Manifest: manifest, CurrentVersion: manifest.Version, DesiredState: DesiredRunning}
	oldRecord := processRecord{PID: 101, StartTicks: "old", ExecutablePath: "/old", WorkingDirectory: "/old", CommandLineMarker: "old"}
	replacementRecord := processRecord{PID: 202, StartTicks: "replacement", ExecutablePath: "/replacement", WorkingDirectory: "/replacement", CommandLineMarker: "replacement"}
	oldRuntime := &runtimeProcess{done: make(chan error, 1), record: oldRecord}
	replacementRuntime := &runtimeProcess{done: make(chan error, 1), record: replacementRecord}
	observer := &countingMetricsLifecycle{}

	if err := os.MkdirAll(filepath.Dir(processRecordPath(manager.pluginDir())), 0750); err != nil {
		t.Fatal(err)
	}
	if err := saveProcessRecord(manager.pluginDir(), replacementRecord); err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	manager.registry.Plugins[PluginID] = entry
	manager.states[PluginID] = statusFromEntry(entry, ObservedRunning, nil)
	manager.runtimes[PluginID] = replacementRuntime
	manager.observer = observer
	manager.mu.Unlock()

	manager.handleUnexpectedExit(PluginID, oldRuntime)
	if observer.disables.Load() != 0 {
		t.Fatal("stale watcher disabled replacement metrics")
	}
	current, err := loadProcessRecord(manager.pluginDir())
	if err != nil || current != replacementRecord {
		t.Fatalf("replacement process record changed: record=%+v err=%v", current, err)
	}
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	if manager.runtimes[PluginID] != replacementRuntime || manager.states[PluginID].ObservedState != ObservedRunning {
		t.Fatalf("replacement runtime or state changed: runtime=%p status=%+v", manager.runtimes[PluginID], manager.states[PluginID])
	}
}

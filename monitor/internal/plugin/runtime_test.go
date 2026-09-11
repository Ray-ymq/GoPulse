package plugin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func runtimeFixture(t *testing.T) (ManagerConfig, *releaseCatalog, map[string]string) {
	t.Helper()
	root := t.TempDir()
	images := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_, _ = w.Write([]byte(`{"status":"ok","service":"redis-exporter"}`))
		} else {
			_, _ = w.Write([]byte("snapshot"))
		}
	}))
	t.Cleanup(server.Close)
	cfg := managerConfig(root, server.URL+"/health")
	cfg.ExporterEnv["GOPULSE_RUNTIME_MODE"] = "host"
	cfg.ExporterEnv["REDIS_HOST"] = "127.0.0.1"
	cfg.ExporterEnv["REDIS_PORT"] = "6379"
	cfg.ExporterEnv["REDIS_DB"] = "0"
	cfg.ExporterEnv["REDIS_PASSWORD"] = "runtime-secret-canary"
	cfg.ValidateSnapshot = func(code int, body []byte) error {
		if code != 200 || string(body) != "snapshot" {
			return errors.New("bad snapshot")
		}
		return nil
	}
	releases := []Release{}
	paths := map[string]string{}
	for _, version := range []string{"1.10.6", "1.11.1", "1.11.2", "1.11.3"} {
		executable := "/usr/bin/yes"
		if version == "1.11.2" {
			executable = "/bin/false"
		}
		binary, err := os.ReadFile(executable)
		if err != nil {
			t.Fatal(err)
		}
		purpose := "retained"
		var archive string
		if version == "1.10.6" {
			archive = writeExecutablePackage(t, version, binary)
			purpose = "legacy-v1"
		} else {
			m, schema := v2Manifest(t)
			m.Version = version
			sum := sha256.Sum256(binary)
			m.EntrypointSHA256 = hex.EncodeToString(sum[:])
			raw, _ := json.Marshal(m)
			archive = writeArchive(t, map[string][]byte{"plugin.json": raw, "config.schema.json": schema, m.Entrypoint: binary})
			if version == "1.11.1" {
				purpose = "current"
			}
		}
		path := filepath.Join(images, version+".tar.gz")
		raw, _ := os.ReadFile(archive)
		if err = os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		pin, err := InspectBuildRelease(path, purpose)
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, pin)
		paths[version] = path
	}
	catalog, err := newReleaseCatalog(images, releases)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, catalog, paths
}
func TestRuntimeAtomicRollbackAndStoppedUpdate(t *testing.T) {
	cfg, catalog, paths := runtimeFixture(t)
	ctx := context.Background()
	core, err := newRuntimeCore(ctx, cfg, catalog)
	if err != nil {
		t.Fatal(err)
	}
	defer core.shutdown(ctx)
	config, secret := core.legacyConfig()
	data := configRequest(config, secret)
	if _, err = core.configure(ctx, PluginID, data, true); err != nil {
		t.Fatal(err)
	}
	before, err := core.loadActive(PluginID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = core.configure(ctx, PluginID, data, true); err == nil {
		t.Fatal("duplicate install accepted")
	}
	if _, err = core.update(ctx, PluginID, paths["1.11.2"]); err == nil {
		t.Fatal("failed candidate committed")
	}
	after, err := core.loadActive(PluginID)
	if err != nil || after.ID != before.ID || !bytes.Equal(after.Secret, before.Secret) || core.slots[PluginID].process == nil {
		t.Fatal("old revision/process not restored", err)
	}
	if _, err = core.setDesired(ctx, PluginID, DesiredStopped); err != nil {
		t.Fatal(err)
	}
	if _, err = core.update(ctx, PluginID, paths["1.11.3"]); err != nil {
		t.Fatal(err)
	}
	status, _ := core.get(PluginID)
	if status.ObservedState != ObservedStopped || status.DesiredState != DesiredStopped || core.slots[PluginID].process != nil {
		t.Fatal("stopped update left a process")
	}
	if err = core.shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	recovered, err := newRuntimeCore(ctx, cfg, catalog)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.shutdown(ctx)
	restored, err := recovered.bootstrap(ctx)
	if err != nil || restored.Version != "1.11.3" || restored.DesiredState != DesiredStopped || restored.ObservedState != ObservedStopped {
		t.Fatal("registered version above current was downgraded or started", err)
	}
	active, _ := core.loadActive(PluginID)
	raw, _ := os.ReadFile(filepath.Join(core.dir(PluginID), "revisions", active.ID, "config.json"))
	if string(raw) == "" || containsSecret(raw, secret.Password) {
		t.Fatal("secret leaked into config")
	}
}
func containsSecret(data []byte, secret string) bool {
	for i := 0; i+len(secret) <= len(data); i++ {
		if string(data[i:i+len(secret)]) == secret {
			return true
		}
	}
	return false
}
func TestRuntimeMigrationAndCommitRecovery(t *testing.T) {
	for _, desired := range []DesiredState{DesiredRunning, DesiredStopped} {
		t.Run(string(desired), func(t *testing.T) {
			cfg, catalog, _ := runtimeFixture(t)
			ctx := context.Background()
			_, _, err := ensureRoot(cfg.Root)
			if err != nil {
				t.Fatal(err)
			}
			pin := catalog.releases[0]
			dir := filepath.Join(cfg.Root, PluginID, "releases", pin.Manifest.Version)
			if err = os.MkdirAll(dir, 0750); err != nil {
				t.Fatal(err)
			}
			if _, err = catalog.extractVerified(PluginID, pin.Manifest.Version, "", dir); err != nil {
				t.Fatal(err)
			}
			if err = switchCurrent(filepath.Join(cfg.Root, PluginID), pin.Manifest.Version); err != nil {
				t.Fatal(err)
			}
			at := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
			entry := registryEntry{Manifest: pin.Manifest, CurrentVersion: pin.Manifest.Version, DesiredState: desired, InstalledAt: at, UpdatedAt: at}
			if err = saveRegistry(cfg.Root, registryFile{Plugins: map[string]registryEntry{PluginID: entry}}); err != nil {
				t.Fatal(err)
			}
			core, err := newRuntimeCore(ctx, cfg, catalog)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = core.shutdown(ctx) }()
			active, err := core.loadActive(PluginID)
			if err != nil || active.Entry != entry {
				t.Fatal("migration changed legacy state", err)
			}
			if (core.slots[PluginID].process == nil) != (desired == DesiredStopped) {
				t.Fatal("migration desired mismatch")
			}
			if _, err = core.configure(ctx, PluginID, configRequest(active.Config, active.Secret), false); err == nil {
				t.Fatal("legacy configure accepted")
			}
			for _, point := range []string{"prepared", "committed"} {
				old, _ := core.loadActive(PluginID)
				next := *old
				next.Entry.DesiredState = DesiredStopped
				core.interrupt = func(p string) error {
					if p == point {
						return errors.New("simulated interruption")
					}
					return nil
				}
				if point == "prepared" {
					err = core.prepare(PluginID, &next)
				} else {
					if err = core.prepare(PluginID, &next); err == nil {
						err = core.commit(PluginID, &next)
					}
				}
				if err == nil {
					t.Fatal("interruption did not occur")
				}
				core.interrupt = nil
				if err = core.shutdown(ctx); err != nil {
					t.Fatal(err)
				}
				core, err = newRuntimeCore(ctx, cfg, catalog)
				if err != nil {
					t.Fatal(err)
				}
				recovered, err := core.loadActive(PluginID)
				if err != nil {
					t.Fatal(err)
				}
				expected := old.ID
				if point == "committed" {
					expected = next.ID
				}
				if recovered.ID != expected {
					t.Fatal("recovery did not follow sole active commit")
				}
			}
		})
	}
}

func TestUnregisteredLegacyDoesNotSignalUnownedProcess(t *testing.T) {
	cfg, catalog, _ := runtimeFixture(t)
	ctx := context.Background()
	if _, _, err := ensureRoot(cfg.Root); err != nil {
		t.Fatal(err)
	}
	pin := catalog.releases[0]
	dir := filepath.Join(cfg.Root, PluginID, "releases", pin.Manifest.Version)
	if err := os.MkdirAll(dir, 0750); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.extractVerified(PluginID, pin.Manifest.Version, "", dir); err != nil {
		t.Fatal(err)
	}
	if err := switchCurrent(filepath.Join(cfg.Root, PluginID), pin.Manifest.Version); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	entry := registryEntry{Manifest: pin.Manifest, CurrentVersion: pin.Manifest.Version, DesiredState: DesiredRunning, InstalledAt: at, UpdatedAt: at}
	if err := saveRegistry(cfg.Root, registryFile{Plugins: map[string]registryEntry{PluginID: entry}}); err != nil {
		t.Fatal(err)
	}
	child := exec.Command("sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _, _ = child.Process.Wait() }()
	ticks, err := procStartTicks(child.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	executable, cwd, marker, err := processIdentity(child.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	record := processRecord{PID: child.Process.Pid, StartTicks: ticks, ExecutablePath: executable, WorkingDirectory: cwd, CommandLineMarker: marker}
	if err = saveProcessRecord(filepath.Join(cfg.Root, PluginID), record); err != nil {
		t.Fatal(err)
	}
	unregistered, err := newReleaseCatalog(catalog.root, catalog.releases[1:])
	if err != nil {
		t.Fatal(err)
	}
	core, err := newRuntimeCore(ctx, cfg, unregistered)
	if err != nil {
		t.Fatal(err)
	}
	if !core.slots[PluginID].blocked {
		t.Fatal("unregistered legacy did not fail closed")
	}
	if !ownsProcess(record) {
		t.Fatal("unowned process was signalled")
	}
	if _, err = os.Stat(filepath.Join(cfg.Root, PluginID, "active.json")); !os.IsNotExist(err) {
		t.Fatal("unregistered legacy was committed")
	}
}

func TestUnknownRegistryIdentityIsRejectedWithoutRewriting(t *testing.T) {
	root := t.TempDir()
	raw := []byte(`{"plugins":{"unknown-exporter":{"manifest":{"id":"unknown-exporter"}}}}`)
	path := filepath.Join(root, "registry.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRegistry(root); err == nil {
		t.Fatal("unknown registry ID accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(raw, after) {
		t.Fatal("invalid legacy state was rewritten")
	}
}

// The sixth ID must share neither its operation token nor its read lock with
// other IDs. One held operation serializes same-ID mutation only.
func TestSixPluginOperationIsolation(t *testing.T) {
	cfg, catalog, _ := runtimeFixture(t)
	core, err := newRuntimeCore(context.Background(), cfg, catalog)
	if err != nil {
		t.Fatal(err)
	}
	held, err := core.lock(context.Background(), "victoriametrics-exporter")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock(held)
	for _, entry := range OfficialCatalog() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		slot, err := core.lock(ctx, entry.ID)
		cancel()
		if entry.ID == "victoriametrics-exporter" {
			if err == nil {
				unlock(slot)
				t.Fatal("same ID was not serialized")
			}
			continue
		}
		if err != nil {
			t.Fatal("foreign ID blocked", entry.ID)
		}
		unlock(slot)
	}
	_ = core.list() // reads do not acquire an operation token
}

func TestShutdownContinuesPastBlockedPlugin(t *testing.T) {
	cfg, catalog, _ := runtimeFixture(t)
	dir := filepath.Join(cfg.Root, PluginID)
	if err := os.MkdirAll(dir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "active.json"), []byte(`{"revision":"broken"}`), 0600); err != nil {
		t.Fatal(err)
	}
	core, err := newRuntimeCore(context.Background(), cfg, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if !core.slots[PluginID].blocked {
		t.Fatal("damaged Redis revision must remain blocked")
	}
	observer := &countingMetricsLifecycle{}
	core.slots["mysql-exporter"].observer = observer
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err = core.shutdown(ctx)
	var failure *Error
	if !errors.As(err, &failure) || failure.Code != CodeFailed {
		t.Fatalf("shutdown must preserve the blocked slot failure: %v", err)
	}
	if observer.disables.Load() != 1 {
		t.Fatal("healthy MySQL collector must be disabled despite blocked Redis")
	}
}

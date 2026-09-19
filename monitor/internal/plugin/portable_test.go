package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPortableOfflineRoundTrip(t *testing.T) {
	cfg, catalog, _ := runtimeFixture(t)
	cfg.ExporterEnv["GOPULSE_RUNTIME_MODE"] = "container"
	cfg.ExporterEnv["REDIS_HOST"] = "redis"
	ctx := context.Background()
	core, err := newRuntimeCore(ctx, cfg, catalog)
	if err != nil {
		t.Fatal(err)
	}
	defer core.shutdown(ctx)
	config, secret := core.legacyConfig()
	if _, err = core.configure(ctx, PluginID, configRequest(config, secret), true); err != nil {
		t.Fatal(err)
	}
	if _, _, err = exportPortable(cfg.Root, catalog); err == nil {
		t.Fatal("live storage exported")
	}
	if _, err = core.setDesired(ctx, PluginID, DesiredStopped); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	core.recordMetrics(PluginID, &at, &at, "", "")
	original, err := core.loadActive(PluginID)
	if err != nil {
		t.Fatal(err)
	}
	if err = core.shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	public, private, err := exportPortable(cfg.Root, catalog)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(public)
	secrets, _ := json.Marshal(private)
	for _, forbidden := range []string{secret.Password, cfg.Root, "executable_path", "pid", "archive", "entrypoint"} {
		if bytes.Contains(raw, []byte(forbidden)) {
			t.Fatalf("public export contains %q", forbidden)
		}
	}
	target := t.TempDir()
	if err = importPortable(target, catalog, raw, secrets); err != nil {
		t.Fatal(err)
	}
	restoredCfg := cfg
	restoredCfg.Root = target
	restored, err := newRuntimeCore(ctx, restoredCfg, catalog)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.shutdown(ctx)
	active, err := restored.loadActive(PluginID)
	if err != nil || active.ID == original.ID || active.Entry != original.Entry || !bytes.Equal(active.Config, original.Config) || !bytes.Equal(active.Secret, original.Secret) {
		t.Fatal("logical state changed or runtime identity reused", err)
	}
	if h := restored.slots[PluginID].status; h.LastSuccessAt == nil || !h.LastSuccessAt.Equal(at) {
		t.Fatal("collection history was not restored")
	}
	if restored.slots[PluginID].process != nil {
		t.Fatal("stopped plugin was started")
	}
	if _, err = restored.setDesired(ctx, PluginID, DesiredRunning); err != nil {
		t.Fatal("normal runtime cannot start restored plugin", err)
	}
	if restored.slots[PluginID].process == nil {
		t.Fatal("restored plugin did not launch")
	}
}

func TestPortableRejectsInvalidAndNonemptyTargets(t *testing.T) {
	cfg, catalog, _ := runtimeFixture(t)
	cfg.ExporterEnv["GOPULSE_RUNTIME_MODE"] = "container"
	cfg.ExporterEnv["REDIS_HOST"] = "redis"
	ctx := context.Background()
	core, err := newRuntimeCore(ctx, cfg, catalog)
	if err != nil {
		t.Fatal(err)
	}
	defer core.shutdown(ctx)
	config, secret := core.legacyConfig()
	if _, err = core.configure(ctx, PluginID, configRequest(config, secret), true); err != nil {
		t.Fatal(err)
	}
	if err = core.shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	public, private, err := exportPortable(cfg.Root, catalog)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(public)
	secrets, _ := json.Marshal(private)
	t.Run("closed schema", func(t *testing.T) {
		target := t.TempDir()
		bad := append([]byte(`{"runtime_path":"/tmp/untrusted",`), raw[1:]...)
		if importPortable(target, catalog, bad, secrets) == nil {
			t.Fatal("unknown runtime field accepted")
		}
		entries, _ := os.ReadDir(target)
		if len(entries) != 0 {
			t.Fatal("invalid input mutated target")
		}
	})
	t.Run("secret boundary", func(t *testing.T) {
		target := t.TempDir()
		changed := public
		changed.Plugins = append([]PortablePlugin(nil), public.Plugins...)
		changed.Plugins[0].Config = json.RawMessage(`{"password":"must-not-be-public"}`)
		bad, _ := json.Marshal(changed)
		if importPortable(target, catalog, bad, secrets) == nil {
			t.Fatal("public secret accepted")
		}
	})
	t.Run("existing target", func(t *testing.T) {
		target := t.TempDir()
		sentinel := filepath.Join(target, "user-data")
		if err := os.WriteFile(sentinel, []byte("preserve"), 0600); err != nil {
			t.Fatal(err)
		}
		if importPortable(target, catalog, raw, secrets) == nil {
			t.Fatal("nonempty target accepted")
		}
		value, _ := os.ReadFile(sentinel)
		if string(value) != "preserve" {
			t.Fatal("unrelated data changed")
		}
	})
	t.Run("untrusted package", func(t *testing.T) {
		target := t.TempDir()
		pin := catalog.releases[1]
		if err := os.WriteFile(filepath.Join(catalog.root, pin.PackageFile), []byte("tampered"), 0600); err != nil {
			t.Fatal(err)
		}
		if importPortable(target, catalog, raw, secrets) == nil {
			t.Fatal("tampered image package accepted")
		}
		entries, _ := os.ReadDir(target)
		if len(entries) != 0 {
			t.Fatal("untrusted package mutated target")
		}
	})
}

func TestPortableInterruptedImportCannotStart(t *testing.T) {
	cfg, catalog, _ := runtimeFixture(t)
	cfg.ExporterEnv["GOPULSE_RUNTIME_MODE"] = "container"
	cfg.ExporterEnv["REDIS_HOST"] = "redis"
	if err := os.WriteFile(filepath.Join(cfg.Root, importPending), []byte("offline-import-v1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if core, err := newRuntimeCore(context.Background(), cfg, catalog); err == nil {
		core.shutdown(context.Background())
		t.Fatal("interrupted import started runtime")
	}
	if _, _, err := exportPortable(cfg.Root, catalog); err == nil {
		t.Fatal("interrupted import exported as complete")
	}
}

package plugin

// Portable state is an offline transport, not a backup archive. The product
// lifecycle must hold its installation lock, stop Monitor, and encrypt the
// private half separately before publishing a backup. No runtime path, PID or
// executable is accepted by this contract.
import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

const MaxPortableBytes = 1 << 20
const importPending = ".restore-in-progress"

func rejectPendingImport(root string) error {
	_, err := os.Lstat(filepath.Join(root, importPending))
	if !errors.Is(err, os.ErrNotExist) {
		return operationFailed()
	}
	return nil
}

type PortablePlugin struct {
	ID          string          `json:"id"`
	Version     string          `json:"version"`
	Desired     DesiredState    `json:"desired_state"`
	InstalledAt time.Time       `json:"installed_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	Config      json.RawMessage `json:"config"`
	SecretRef   string          `json:"secret_ref"`
}
type PortableState struct {
	Schema        int              `json:"schema"`
	CatalogDigest string           `json:"catalog_digest"`
	Plugins       []PortablePlugin `json:"plugins"`
}
type PortableSecrets struct {
	Schema  int                        `json:"schema"`
	Plugins map[string]json.RawMessage `json:"plugins"`
}

func storageLease(root string) (func(), error) {
	fd, err := syscall.Open(filepath.Join(root, ".operation.lock"), syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return nil, operationFailed()
	}
	var stat syscall.Stat_t
	if syscall.Fstat(fd, &stat) != nil || stat.Mode&syscall.S_IFMT != syscall.S_IFREG || stat.Nlink != 1 || stat.Mode&0077 != 0 {
		syscall.Close(fd)
		return nil, operationFailed()
	}
	if syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		syscall.Close(fd)
		return nil, NewError(CodeConflict, "plugin storage is in use; stop Monitor before offline transfer")
	}
	var once sync.Once
	return func() { once.Do(func() { syscall.Flock(fd, syscall.LOCK_UN); syscall.Close(fd) }) }, nil
}
func portableCatalog(packages string) (*releaseCatalog, error) {
	var releases []Release
	if json.Unmarshal([]byte(compiledReleaseJSON), &releases) != nil || len(releases) == 0 {
		return nil, operationFailed()
	}
	return newReleaseCatalog(packages, releases)
}
func catalogFingerprint(c *releaseCatalog) string {
	raw, _ := json.Marshal(c.releases)
	h := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(h[:])
}
func decodePortable(raw []byte, into any) error {
	if len(raw) > MaxPortableBytes || !uniqueJSON(raw) {
		return operationFailed()
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(into) != nil || !errors.Is(d.Decode(new(any)), io.EOF) {
		return operationFailed()
	}
	return nil
}

// ExportPortable reads committed revisions only, while holding the same lease
// as the live runtime. The private result must never be logged or stored plain.
func ExportPortable(root, packages string) (PortableState, PortableSecrets, error) {
	catalog, err := portableCatalog(packages)
	if err != nil {
		return PortableState{}, PortableSecrets{}, err
	}
	return exportPortable(root, catalog)
}
func exportPortable(root string, catalog *releaseCatalog) (PortableState, PortableSecrets, error) {
	public := PortableState{Schema: 1, CatalogDigest: catalogFingerprint(catalog), Plugins: []PortablePlugin{}}
	private := PortableSecrets{Schema: 1, Plugins: map[string]json.RawMessage{}}
	fail := func() (PortableState, PortableSecrets, error) {
		return PortableState{}, PortableSecrets{}, operationFailed()
	}
	if _, err := validatePluginRoot(root); err != nil || requireDirectory(root) != nil {
		return fail()
	}
	release, err := storageLease(root)
	if err != nil {
		return PortableState{}, PortableSecrets{}, err
	}
	defer release()
	if rejectPendingImport(root) != nil {
		return fail()
	}
	c := &runtimeCore{cfg: ManagerConfig{Root: root, ExporterEnv: map[string]string{"GOPULSE_RUNTIME_MODE": "container"}}, catalog: catalog}
	for _, item := range OfficialCatalog() {
		r, err := c.loadActive(item.ID)
		if errors.Is(err, os.ErrNotExist) {
			// A legacy registry or partial revision is not an empty installation.
			if _, e := os.Lstat(c.dir(item.ID)); !errors.Is(e, os.ErrNotExist) {
				return fail()
			}
			continue
		}
		if err != nil || r.Entry.Manifest.SchemaVersion != 2 {
			return fail()
		}
		pin, _, err := catalog.verifyArchive(item.ID, r.Entry.CurrentVersion, "")
		if err != nil || pin.Manifest != r.Entry.Manifest {
			return fail()
		}
		cfg, secret, err := c.parseRequest(item.ID, configRequest(r.Config, r.Secret), nil)
		if err != nil {
			return fail()
		}
		public.Plugins = append(public.Plugins, PortablePlugin{item.ID, r.Entry.CurrentVersion, r.Entry.DesiredState, r.Entry.InstalledAt, r.Entry.UpdatedAt, cfg, item.ID})
		private.Plugins[item.ID] = secret
	}
	// Legacy-only registry entries must not disappear silently.
	reg, err := loadRegistry(root)
	if err != nil {
		return fail()
	}
	for id := range reg.Plugins {
		if _, ok := private.Plugins[id]; !ok {
			return fail()
		}
	}
	return public, private, nil
}

// ImportPortable accepts only a fresh root and an exact same-catalog export.
// Upgrade mappings belong to a separate versioned contract, never an implicit
// fallback here. Packages are re-materialized from the image-owned catalog.
// No process is launched: the normal Monitor startup restores desired state.
func ImportPortable(root, packages string, publicJSON, privateJSON []byte) error {
	catalog, err := portableCatalog(packages)
	if err != nil {
		return err
	}
	return importPortable(root, catalog, publicJSON, privateJSON)
}
func importPortable(root string, catalog *releaseCatalog, publicJSON, privateJSON []byte) (err error) {
	var public PortableState
	var private PortableSecrets
	if decodePortable(publicJSON, &public) != nil || decodePortable(privateJSON, &private) != nil || public.Schema != 1 || private.Schema != 1 || public.CatalogDigest != catalogFingerprint(catalog) || len(public.Plugins) > len(OfficialCatalog()) || len(private.Plugins) != len(public.Plugins) {
		return operationFailed()
	}
	c := &runtimeCore{cfg: ManagerConfig{Root: root, ExporterEnv: map[string]string{"GOPULSE_RUNTIME_MODE": "container"}}, catalog: catalog, slots: map[string]*runtimeSlot{}}
	for _, item := range OfficialCatalog() {
		c.slots[item.ID] = &runtimeSlot{}
	}
	revisions := make([]*revision, 0, len(public.Plugins))
	seen := map[string]bool{}
	for _, p := range public.Plugins {
		if seen[p.ID] || c.slots[p.ID] == nil || p.SecretRef != p.ID || p.InstalledAt.IsZero() || p.UpdatedAt.Before(p.InstalledAt) || (p.Desired != DesiredRunning && p.Desired != DesiredStopped) {
			return operationFailed()
		}
		seen[p.ID] = true
		secret, ok := private.Plugins[p.SecretRef]
		if !ok {
			return operationFailed()
		}
		pin, _, e := catalog.verifyArchive(p.ID, p.Version, "")
		if e != nil || pin.Manifest.SchemaVersion != 2 {
			return operationFailed()
		}
		cfg, secret, e := c.parseRequest(p.ID, configRequest(p.Config, secret), nil)
		if e != nil {
			return operationFailed()
		}
		revisions = append(revisions, &revision{Entry: registryEntry{pin.Manifest, p.Version, p.Desired, p.InstalledAt, p.UpdatedAt}, Config: cfg, Secret: secret})
	}
	if _, e := validatePluginRoot(root); e != nil || requireDirectory(root) != nil {
		return operationFailed()
	}
	release, err := storageLease(root)
	if err != nil {
		return err
	}
	defer release()
	entries, err := os.ReadDir(root)
	if err != nil {
		return operationFailed()
	}
	for _, e := range entries {
		if e.Name() != ".operation.lock" {
			return NewError(CodeConflict, "portable import requires empty plugin storage")
		}
	}
	// The root was empty under our exclusive lease. Cleanup owns only the fixed
	// catalog directories created below, not arbitrary caller-supplied paths.
	defer func() {
		if err == nil {
			return
		}
		var cleanup []error
		for _, r := range revisions {
			cleanup = append(cleanup, os.RemoveAll(c.dir(r.Entry.Manifest.ID)))
		}
		cleanup = append(cleanup, os.RemoveAll(filepath.Join(root, ".staging")))
		if e := os.Remove(filepath.Join(root, "registry.json")); e != nil && !errors.Is(e, os.ErrNotExist) {
			cleanup = append(cleanup, e)
		}
		// If cleanup cannot finish, the marker must continue blocking startup.
		if errors.Join(cleanup...) == nil {
			_ = os.Remove(filepath.Join(root, importPending))
		}
	}()
	if err = secureDir(root); err != nil {
		return operationFailed()
	}
	if err = atomicWrite(filepath.Join(root, importPending), []byte("offline-import-v1\n"), 0600); err != nil {
		return operationFailed()
	}
	if err = secureDir(filepath.Join(root, ".staging")); err != nil {
		return operationFailed()
	}
	for _, r := range revisions {
		if err = c.ensureRelease(r); err != nil {
			return operationFailed()
		}
		if err = c.prepare(r.Entry.Manifest.ID, r); err != nil {
			return operationFailed()
		}
	}
	for _, r := range revisions {
		if err = c.commit(r.Entry.Manifest.ID, r); err != nil {
			return operationFailed()
		}
	}
	if err = os.Remove(filepath.Join(root, importPending)); err != nil {
		return operationFailed()
	}
	if err = syncDir(root); err != nil {
		return operationFailed()
	}
	return nil
}

package control

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/Ray-ymq/GoPulse/lifecycle/internal/backup"
	"github.com/Ray-ymq/GoPulse/lifecycle/internal/release"
)

var identifier = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func strictData(b []byte, v any) error { return backup.DecodeJSON(b, v) }

type snapshotConfig struct {
	Schema      int               `json:"schema"`
	Values      map[string]string `json:"values"`
	MySQLCounts map[string]int64  `json:"mysql_counts"`
	MySQLSHA    string            `json:"mysql_sha256"`
	SearchSHA   string            `json:"search_sha256"`
	Metrics     metricSummary     `json:"metrics"`
	PluginsSHA  string            `json:"plugins_sha256"`
	Sessions    string            `json:"sessions"`
}
type snapshotSecrets struct {
	Schema      int               `json:"schema"`
	Credentials map[string]string `json:"credentials"`
	Plugins     json.RawMessage   `json:"plugins"`
}
type pluginTransport struct {
	Public  json.RawMessage `json:"public"`
	Private json.RawMessage `json:"private"`
}
type restoredBackup struct {
	Manifest backup.Manifest
	Files    map[string][]byte
	Config   snapshotConfig
	Secrets  snapshotSecrets
}

var portableValues = map[string]bool{"MYSQL_DATABASE": true, "MYSQL_USER": true, "RABBITMQ_USER": true, "VICTORIAMETRICS_USERNAME": true, "AUTH_COOKIE_SECURE": true}

func credentialKey(k string) bool {
	return strings.Contains(k, "PASSWORD") || strings.Contains(k, "TOKEN") || strings.Contains(k, "SECRET")
}

func (c *Controller) disk(required uint64) error {
	var s syscall.Statfs_t
	if syscall.Statfs(c.dir, &s) != nil {
		return fail(Capacity, "disk", "cannot inspect installation capacity")
	}
	if s.Bavail*uint64(s.Bsize) < required {
		return fail(Capacity, "disk", "insufficient free installation space; no completed backup or ready restore was published")
	}
	return nil
}
func (c *Controller) pluginTransfer(mode string, input []byte) ([]byte, error) {
	if _, e := c.owned(); e != nil {
		return nil, e
	}
	// The normal monitor container remains stopped. A short-lived, non-networked
	// process uses exactly its pinned image and owned volume; never an uploaded binary.
	service := c.services["monitor"].(map[string]any)
	name := c.state.Project + "-transfer-" + c.state.Operation[:12]
	args := []string{"run", "--name", name, "--rm", "-i", "--network", "none", "--read-only", "--log-driver", "none", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--tmpfs", "/tmp", "--label", prefix + "installation=" + c.state.Token, "--label", prefix + "operation=" + c.state.Operation, "-v", c.state.Project + "_monitor_plugin_data:/var/lib/gopulse-monitor/plugins", service["image"].(string), "plugin-state", mode, "--root", "/var/lib/gopulse-monitor/plugins"}
	defer func() {
		// CLI cancellation can outlive a detached daemon-side process. Inspect exact
		// labels before removing only this helper, using an independent cleanup context.
		old := c.ctx
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		c.ctx = ctx
		defer func() { c.ctx = old }()
		raw, e := c.docker("inspect", "--type", "container", name)
		if e != nil {
			return
		}
		var objects []Container
		if json.Unmarshal(raw, &objects) == nil && len(objects) == 1 && objects[0].Config.Labels[prefix+"installation"] == c.state.Token && objects[0].Config.Labels[prefix+"operation"] == c.state.Operation {
			_, _ = c.docker("rm", "-f", "-v", objects[0].ID)
		}
	}()
	return c.pipe(input, args...)
}
func (c *Controller) quiesce() error {
	if _, e := c.status(true); e != nil {
		return e
	}
	if e := c.phase("maintenance-edge"); e != nil {
		return e
	}
	if e := c.compose("stop", "--timeout", "30", "edge"); e != nil {
		return e
	}
	if e := c.phase("maintenance-business-drain"); e != nil {
		return e
	}
	if e := c.waitDrain(func() (bool, error) {
		b, e := c.sql("SELECT COUNT(*) FROM business_outbox WHERE published_at IS NULL;")
		if e != nil {
			return false, e
		}
		if strings.TrimSpace(string(b)) != "0" {
			return false, nil
		}
		return c.rabbitEmpty()
	}); e != nil {
		return e
	}
	if e := c.compose("stop", "--timeout", "30", "backend", "business-worker", "search-indexer"); e != nil {
		return e
	}
	if ok, e := c.rabbitEmpty(); e != nil {
		return e
	} else if !ok {
		return fail(NotReady, "business-drain", "business queue changed while stopping; resume and retry backup")
	}
	if e := c.phase("maintenance-observability-drain"); e != nil {
		return e
	}
	if e := c.compose("stop", "--timeout", "30", "monitor"); e != nil {
		return e
	}
	if e := c.compose("stop", "--timeout", "30", "router"); e != nil {
		return e
	}
	if e := c.waitDrain(c.kafkaEmpty); e != nil {
		return e
	}
	if e := c.compose("stop", "--timeout", "30", "marshaller"); e != nil {
		return e
	}
	if _, e := c.request("elasticsearch", "POST", "/gopulse-*/_refresh", "", nil); e != nil {
		return e
	}
	if _, e := c.request("victoriametrics", "GET", "/internal/force_flush", "", nil); e != nil {
		return e
	}
	return c.phase("maintenance-quiesced")
}
func (c *Controller) snapshot(pass []byte) (backup.Manifest, map[string][]byte, error) {
	started := time.Now().UTC()
	m := backup.Manifest{ProductVersion: c.manifest.Version, ReleaseDigest: c.manifestHash, Operation: c.state.Operation, Started: started, Domains: map[string]backup.Domain{}}
	files := map[string][]byte{}
	var e error
	if e = c.quiesce(); e != nil {
		return m, nil, e
	}
	cutover := time.Now().UTC()
	cfg := snapshotConfig{Schema: 1, Values: map[string]string{}, Sessions: "invalidate-on-restore"}
	secrets := snapshotSecrets{Schema: 1, Credentials: map[string]string{}}
	for k, v := range c.env {
		if portableValues[k] {
			cfg.Values[k] = v
		} else if credentialKey(k) {
			secrets.Credentials[k] = v
		}
	}
	if e = c.phase("export-mysql"); e != nil {
		return m, nil, e
	}
	files["mysql.sql"], cfg.MySQLCounts, e = c.mysqlExport()
	if e != nil {
		return m, nil, e
	}
	cfg.MySQLSHA = release.Sum(files["mysql.sql"])
	m.Domains["mysql"] = backup.Domain{Cutover: cutover, Start: cutover, End: cutover, Counts: cfg.MySQLCounts}
	if e = c.phase("export-search"); e != nil {
		return m, nil, e
	}
	var counts map[string]int64
	files["elasticsearch.json"], counts, e = c.searchExport()
	if e != nil {
		return m, nil, e
	}
	cfg.SearchSHA = release.Sum(files["elasticsearch.json"])
	counts["indices"] = int64(len(counts))
	m.Domains["elasticsearch"] = backup.Domain{Cutover: cutover, Start: cutover, End: cutover, Counts: counts}
	if e = c.phase("export-metrics"); e != nil {
		return m, nil, e
	}
	files["victoriametrics.native"], cfg.Metrics, e = c.metricExport(cutover)
	if e != nil {
		return m, nil, e
	}
	start, end := cutover, cutover
	if cfg.Metrics.Samples > 0 {
		start = time.UnixMilli(cfg.Metrics.Start).UTC()
		end = time.UnixMilli(cfg.Metrics.End).UTC()
	}
	m.Domains["victoriametrics"] = backup.Domain{Cutover: cutover, Start: start, End: end, Counts: map[string]int64{"series": cfg.Metrics.Series, "samples": cfg.Metrics.Samples}}
	if e = c.phase("export-messaging"); e != nil {
		return m, nil, e
	}
	files["rabbitmq.json"], e = c.rabbitExport()
	if e != nil {
		return m, nil, e
	}
	var topology map[string][]json.RawMessage
	_ = json.Unmarshal(files["rabbitmq.json"], &topology)
	m.Domains["rabbitmq"] = backup.Domain{Cutover: cutover, Start: cutover, End: cutover, Counts: map[string]int64{"queues": int64(len(topology["queues"])), "exchanges": int64(len(topology["exchanges"])), "bindings": int64(len(topology["bindings"]))}, Drained: true}
	kafka, e := c.kafkaState()
	if e != nil {
		return m, nil, e
	}
	files["kafka.json"] = marshal(kafka)
	m.Domains["kafka"] = backup.Domain{Cutover: cutover, Start: cutover, End: cutover, Counts: map[string]int64{"topics": 1, "partitions": 1}, Offsets: map[string]int64{kafka.Topic + "/0": kafka.Offsets[0].Committed}, Drained: kafka.Offsets[0].Committed == kafka.Offsets[0].End}
	if e = c.phase("export-plugins"); e != nil {
		return m, nil, e
	}
	raw, e := c.pluginTransfer("export", nil)
	if e != nil {
		return m, nil, e
	}
	defer clear(raw)
	var transport pluginTransport
	if strictData(raw, &transport) != nil {
		return m, nil, fail(Failed, "plugin-export", "invalid plugin state transport")
	}
	files["plugins.json"] = transport.Public
	secrets.Plugins = transport.Private
	cfg.PluginsSHA = release.Sum(transport.Public)
	var plugins struct {
		Schema  int               `json:"schema"`
		Catalog string            `json:"catalog_digest"`
		Plugins []json.RawMessage `json:"plugins"`
	}
	if strictData(transport.Public, &plugins) != nil {
		return m, nil, fail(Failed, "plugin-export", "invalid portable plugin metadata")
	}
	m.Domains["plugins"] = backup.Domain{Cutover: cutover, Start: cutover, End: cutover, Counts: map[string]int64{"plugins": int64(len(plugins.Plugins))}, CatalogDigest: plugins.Catalog}
	secretJSON := marshal(secrets)
	defer clear(secretJSON)
	files[backup.SecretEntry], e = backup.SealSecrets(secretJSON, pass)
	if e != nil {
		return m, nil, e
	}
	files["config.json"] = marshal(cfg)
	m.Finished = time.Now().UTC()
	m.Complete = true
	return m, files, nil
}
func (c *Controller) createBackup(path string, pass []byte) error {
	if filepath.Dir(path) != c.dir || filepath.Base(path) == "state.json" {
		return fail(Usage, "backup-destination", "backup must be a new file inside the private installation directory")
	}
	if _, e := os.Lstat(path); !errors.Is(e, os.ErrNotExist) {
		return fail(Permission, "backup-destination", "backup destination already exists or is unsafe")
	}
	if e := c.disk(5 << 30); e != nil {
		return e
	}
	if c.state.Phase != "ready" {
		return fail(NotReady, "backup-preflight", "backup requires ready state; resume the product before retrying")
	}
	if e := c.phase("backup-preflight"); e != nil {
		return e
	}
	m, files, e := c.snapshot(pass)
	defer func() {
		for _, v := range files {
			clear(v)
		}
	}()
	if e != nil {
		return e
	}
	blob, e := backup.Seal(m, files, pass)
	if e != nil {
		return fail(BackupInvalid, "backup-seal", "logical export cannot be sealed as a complete bounded backup")
	}
	defer clear(blob)
	// Independently parse/authenticate the completed bytes before publication.
	if _, verified, e := backup.Open(blob, pass); e != nil {
		return fail(BackupInvalid, "backup-inspect", "completed backup failed independent validation")
	} else {
		for _, v := range verified {
			clear(v)
		}
	}
	if e = backup.Publish(c.ctx, c.dir, filepath.Base(path), blob); e != nil {
		return fail(Failed, "backup-publish", "encrypted backup was not published; inspect destination capacity")
	}
	if e = c.phase("backup-published"); e != nil {
		return e
	}
	// Restart through the existing lifecycle, retaining the original identity.
	return c.up()
}
func (c *Controller) openRestore(path string, pass []byte) (*restoredBackup, error) {
	blob, e := backup.Read(path)
	if e != nil {
		return nil, fail(BackupInvalid, "restore-read", "cannot read private bounded backup")
	}
	defer clear(blob)
	m, files, e := backup.Open(blob, pass)
	if e != nil {
		return nil, fail(BackupInvalid, "restore-authenticate", "backup authentication or format invalid; verify the secret source and intact archive")
	}
	r := &restoredBackup{Manifest: m, Files: files}
	valid := false
	defer func() {
		if !valid {
			r.clear()
		}
	}()
	if m.ProductVersion != c.manifest.Version || m.ReleaseDigest != c.manifestHash {
		return nil, fail(ManifestError, "restore-version", "restore requires the exact source release bundle; upgrade is a separate operation")
	}
	if strictData(files["config.json"], &r.Config) != nil || r.Config.Schema != 1 || r.Config.Sessions != "invalidate-on-restore" {
		return nil, fail(BackupInvalid, "restore-config", "invalid portable configuration")
	}
	raw, e := backup.OpenSecrets(files[backup.SecretEntry], pass)
	if e != nil {
		return nil, fail(BackupInvalid, "restore-secrets", "invalid encrypted secret entry")
	}
	defer clear(raw)
	if strictData(raw, &r.Secrets) != nil || r.Secrets.Schema != 1 {
		return nil, fail(BackupInvalid, "restore-secrets", "invalid portable secret schema")
	}
	for k, v := range r.Config.Values {
		if !portableValues[k] || v == "" {
			return nil, fail(BackupInvalid, "restore-config", "unsupported portable setting")
		}
	}
	for k, v := range r.Secrets.Credentials {
		if !credentialKey(k) || len(v) < 16 {
			return nil, fail(BackupInvalid, "restore-secrets", "unsupported credential")
		}
	}
	if r.Config.MySQLSHA != release.Sum(files["mysql.sql"]) || r.Config.SearchSHA != release.Sum(files["elasticsearch.json"]) || r.Config.PluginsSHA != release.Sum(files["plugins.json"]) {
		return nil, fail(BackupInvalid, "restore-facts", "logical export digest mismatch")
	}
	if e = c.disk(uint64(len(blob))*3 + (5 << 30)); e != nil {
		return nil, e
	}
	valid = true
	return r, nil
}
func (r *restoredBackup) clear() {
	for _, v := range r.Files {
		clear(v)
	}
	clear(r.Secrets.Plugins)
	for k := range r.Secrets.Credentials {
		delete(r.Secrets.Credentials, k)
	}
}
func (c *Controller) emptyRestoreTarget() error {
	if c.state.Phase != "initialized" && c.state.Phase != "restore-failed" && c.state.Phase != "interrupted" {
		return fail(Ownership, "restore-target", "restore requires a newly initialized empty installation")
	}
	cs, e := c.owned()
	if e != nil {
		return e
	}
	if len(cs) != 0 {
		return fail(Ownership, "restore-target", "target contains product containers")
	}
	for _, kind := range []string{"volume", "network"} {
		b, e := c.docker(kind, "ls", "-q", "--filter", "label=com.docker.compose.project="+c.state.Project)
		if e != nil {
			return e
		}
		if strings.TrimSpace(string(b)) != "" {
			return fail(Ownership, "restore-target", "target contains existing product resources")
		}
	}
	return nil
}
func (c *Controller) restore(r *restoredBackup) (err error) {
	if err = c.emptyRestoreTarget(); err != nil {
		return err
	}
	for k := range r.Secrets.Credentials {
		if _, ok := c.env[k]; !ok {
			return fail(BackupInvalid, "restore-secrets", "secret does not belong to target configuration")
		}
	}
	for k := range c.env {
		if credentialKey(k) {
			if _, ok := r.Secrets.Credentials[k]; !ok {
				return fail(BackupInvalid, "restore-secrets", "required restorable credential missing")
			}
		}
	}
	c.state.Operation = random()
	if err = c.save(); err != nil {
		return err
	}
	marker := filepath.Join(c.dir, "restore-pending.json")
	if err = atomicFile(marker, marshal(map[string]any{"schema": 1, "operation_id": c.state.Operation, "source_manifest": r.Manifest.ReleaseDigest})); err != nil {
		return fail(Permission, "restore-journal", "cannot persist restore journal")
	}
	defer func() {
		if err == nil {
			return
		}
		old := c.ctx
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		c.ctx = ctx
		defer func() { c.ctx = old }()
		if _, e := c.owned(); e == nil {
			_ = c.compose("down", "--volumes", "--timeout", "20")
		}
		_ = c.phase("restore-failed")
	}()
	for k, v := range r.Config.Values {
		c.env[k] = v
	}
	for k, v := range r.Secrets.Credentials {
		c.env[k] = v
	}
	// Existing sessions are deliberately invalidated; passwords/roles remain.
	c.env["AUTH_JWT_SECRET"] = random()
	if err = atomicFile(filepath.Join(c.dir, "secrets.json"), marshal(c.env)); err != nil {
		return err
	}
	if err = atomicFile(filepath.Join(c.dir, "victoriametrics_password"), []byte(c.env["VICTORIAMETRICS_PASSWORD"])); err != nil {
		return err
	}
	if err = c.phase("restore-infrastructure"); err != nil {
		return err
	}
	if err = c.pull(); err != nil {
		return err
	}
	if err = c.compose("up", "-d", "--no-build", "--pull", "never", "--wait", "--wait-timeout", "240", "mysql", "redis", "rabbitmq", "kafka", "elasticsearch", "victoriametrics"); err != nil {
		return err
	}
	if err = c.phase("restore-mysql"); err != nil {
		return err
	}
	if _, err = c.sql(string(r.Files["mysql.sql"])); err != nil {
		return err
	}
	if err = c.phase("restore-search"); err != nil {
		return err
	}
	if err = c.searchImport(r.Files["elasticsearch.json"]); err != nil {
		return err
	}
	if err = c.phase("restore-metrics"); err != nil {
		return err
	}
	if _, err = c.request("victoriametrics", "POST", "/api/v1/import/native", "application/octet-stream", r.Files["victoriametrics.native"]); err != nil {
		return err
	}
	if err = c.phase("restore-messaging"); err != nil {
		return err
	}
	if _, err = c.request("rabbitmq", "POST", "/api/definitions/%2F", "application/json", r.Files["rabbitmq.json"]); err != nil {
		return err
	}
	if err = c.kafkaImport(r.Files["kafka.json"]); err != nil {
		return err
	}
	if err = c.phase("restore-plugins"); err != nil {
		return err
	}
	// Compose creates only this owned volume; it does not launch Monitor yet.
	if err = c.compose("create", "--no-build", "--pull", "never", "--no-deps", "monitor"); err != nil {
		return err
	}
	input := marshal(pluginTransport{r.Files["plugins.json"], r.Secrets.Plugins})
	defer clear(input)
	if _, err = c.pluginTransfer("import", input); err != nil {
		return err
	}
	if err = c.phase("restore-verify-facts"); err != nil {
		return err
	}
	dump, counts, e := c.mysqlExport()
	if e != nil {
		return e
	}
	defer clear(dump)
	if release.Sum(dump) != r.Config.MySQLSHA || string(marshal(counts)) != string(marshal(r.Config.MySQLCounts)) {
		return fail(Failed, "restore-verify-mysql", "restored SQL facts differ from cutover")
	}
	search, _, e := c.searchExport()
	if e != nil {
		return e
	}
	if release.Sum(search) != r.Config.SearchSHA {
		return fail(Failed, "restore-verify-search", "restored search facts differ from cutover")
	}
	if _, err = c.request("victoriametrics", "GET", "/internal/force_flush", "", nil); err != nil {
		return err
	}
	metrics, e := c.metricFacts(r.Manifest.Domains["victoriametrics"].Cutover)
	if e != nil {
		return e
	}
	if metrics != r.Config.Metrics {
		return fail(Failed, "restore-verify-metrics", "restored metrics differ from cutover")
	}
	raw, e := c.pluginTransfer("export", nil)
	if e != nil {
		return e
	}
	defer clear(raw)
	var p pluginTransport
	if strictData(raw, &p) != nil || release.Sum(p.Public) != r.Config.PluginsSHA {
		return fail(Failed, "restore-verify-plugins", "restored plugin facts differ from cutover")
	}
	if err = c.phase("restore-applications"); err != nil {
		return err
	}
	// up runs migration/bootstrap and health checks; the pending marker remains
	// until they all pass, preventing an interrupted operation being resumed as ready.
	if err = c.startProduct(false); err != nil {
		return err
	}
	if err = atomicFile(filepath.Join(c.dir, "restore-result.json"), marshal(map[string]any{"schema": 1, "operation_id": c.state.Operation, "source_manifest": r.Manifest.ReleaseDigest, "cutover": r.Manifest.Domains["mysql"].Cutover, "facts_verified": true, "session_policy": r.Config.Sessions, "kafka_policy": "drained-new-topic-rebase-zero"})); err != nil {
		return err
	}
	if err = os.Remove(marker); err != nil {
		return err
	}
	return c.phase("ready")
}

func (c *Controller) recoveryDiagnostic(err error) error {
	var f *Failure
	if !errors.As(err, &f) {
		f = &Failure{Code: Failed, Stage: c.state.Phase, Message: "recovery operation failed; raw diagnostics suppressed"}
	}
	if c.ctx.Err() != nil {
		f = &Failure{Code: Interrupted, Stage: c.state.Phase, Message: "operation interrupted; inspect diagnostic; resume source with up or retry restore from the verified archive"}
	}
	if len(c.state.Token) != 64 || privateDir(c.dir) != nil {
		return f
	}
	dir := filepath.Join(c.dir, "diagnostics")
	if os.MkdirAll(dir, 0700) != nil || privateDir(dir) != nil {
		return f
	}
	path := filepath.Join(dir, c.state.Operation+".json")
	safe := map[string]any{"schema": 1, "command_scope": "backup-restore", "operation_id": c.state.Operation, "version": c.state.Version, "manifest_digest": c.manifestHash, "phase": c.state.Phase, "failure_stage": f.Stage, "exit_code": f.Code, "recovery": "source: up then retry backup; empty target: retry restore with the same verified archive; never start a pending restore"}
	if atomicFile(path, marshal(safe)) == nil {
		f.Diagnostic = path
	}
	return f
}

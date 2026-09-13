package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ray-ymq/GoPulse/lifecycle/internal/backup"
)

func TestBackupInspect(t *testing.T) {
	dir := t.TempDir()
	pass := []byte("private test-only passphrase")
	source := filepath.Join(dir, "passphrase")
	if err := os.WriteFile(source, pass, 0600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	m := backup.Manifest{ProductVersion: "1.13.4", ReleaseDigest: "sha256:" + strings.Repeat("a", 64), Operation: strings.Repeat("b", 64), Started: now, Finished: now, Complete: true, Domains: map[string]backup.Domain{}}
	for _, domain := range []string{"mysql", "elasticsearch", "victoriametrics", "rabbitmq", "kafka", "plugins"} {
		m.Domains[domain] = backup.Domain{Cutover: now, Start: now, End: now, Counts: map[string]int64{"sensitive-business-key": 1}, Offsets: map[string]int64{}, Drained: true, CatalogDigest: "sha256:" + strings.Repeat("c", 64)}
	}
	files := map[string][]byte{}
	for _, name := range []string{"config.json", "elasticsearch.json", "kafka.json", "mysql.sql", "plugins.json", "rabbitmq.json", "victoriametrics.native"} {
		files[name] = []byte("sensitive-business-data")
	}
	secret, err := backup.SealSecrets([]byte("sensitive-restorable-secret"), pass)
	if err != nil {
		t.Fatal(err)
	}
	files[backup.SecretEntry] = secret
	blob, err := backup.Seal(m, files, pass)
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "archive.gpb")
	if err = os.WriteFile(archive, blob, 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	// No Docker endpoint, installation state, or bundle is required or touched.
	args := []string{"backup-inspect", "--archive", archive, "--passphrase-file", source}
	if err = Run(context.Background(), args, "unused", "unused", &out); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if json.Unmarshal(out.Bytes(), &result) != nil || result["status"] != "passed" {
		t.Fatal("missing inspection result")
	}
	for _, s := range []string{string(pass), "sensitive", dir} {
		if strings.Contains(out.String(), s) {
			t.Fatal("private metadata leaked")
		}
	}
	blob[len(blob)-1] ^= 1
	if err = os.WriteFile(archive, blob, 0600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	err = Run(context.Background(), args, "unused", "unused", &out)
	var failure *Failure
	if !errors.As(err, &failure) || failure.Code != BackupInvalid || failure.Stage != "backup-authenticate" || out.Len() != 0 {
		t.Fatalf("unsafe failed inspection: %v", err)
	}
}

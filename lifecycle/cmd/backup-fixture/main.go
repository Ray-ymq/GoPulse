// backup-fixture is an acceptance-only program, never copied into product
// images. It consumes an actual authenticated backup; it cannot invent a
// successful product fixture. Its sole mutation is an invalid SQL failure case.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/Ray-ymq/GoPulse/lifecycle/internal/backup"
	"github.com/Ray-ymq/GoPulse/lifecycle/internal/release"
	"os"
	"path/filepath"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "backup fixture check/mutation failed (private data suppressed)")
		os.Exit(1)
	}
}
func run() error {
	archive := flag.String("archive", "", "")
	passFile := flag.String("passphrase-file", "", "")
	output := flag.String("invalid-sql-output", "", "")
	flag.Parse()
	pass, e := backup.ReadPassphrase(*passFile)
	if e != nil {
		return e
	}
	defer clear(pass)
	raw, e := backup.Read(*archive)
	if e != nil {
		return e
	}
	defer clear(raw)
	m, files, e := backup.Open(raw, pass)
	if e != nil {
		return e
	}
	defer func() {
		for _, b := range files {
			clear(b)
		}
	}()
	secret, e := backup.OpenSecrets(files[backup.SecretEntry], pass)
	if e != nil {
		return e
	}
	defer clear(secret)
	var secrets struct {
		Schema      int               `json:"schema"`
		Credentials map[string]string `json:"credentials"`
		Plugins     struct {
			Schema  int                          `json:"schema"`
			Plugins map[string]map[string]string `json:"plugins"`
		} `json:"plugins"`
	}
	if json.Unmarshal(secret, &secrets) != nil {
		return backup.ErrInvalid
	}
	values := [][]byte{pass}
	for _, v := range secrets.Credentials {
		values = append(values, []byte(v))
	}
	for _, p := range secrets.Plugins.Plugins {
		for _, v := range p {
			values = append(values, []byte(v))
		}
	}
	for name, b := range files {
		if name == backup.SecretEntry {
			continue
		}
		for _, v := range values {
			if len(v) > 8 && bytes.Contains(b, v) {
				return backup.ErrInvalid
			}
		}
	}
	if *output == "" {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"schema": 1, "status": "passed", "public_payload_secret_scan": "passed", "domains": m.Domains, "source_version": m.ProductVersion, "manifest_digest": m.ReleaseDigest})
	}
	var cfg map[string]any
	if json.Unmarshal(files["config.json"], &cfg) != nil {
		return backup.ErrInvalid
	}
	files["mysql.sql"] = []byte("THIS_IS_AN_INTENTIONAL_INVALID_SQL_FAILURE_CASE;\n")
	cfg["mysql_sha256"] = release.Sum(files["mysql.sql"])
	files["config.json"], _ = json.Marshal(cfg)
	blob, e := backup.Seal(m, files, pass)
	if e != nil {
		return e
	}
	defer clear(blob)
	if !filepath.IsAbs(*output) {
		return backup.ErrDestination
	}
	f, e := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	_, e = f.Write(blob)
	return e
}

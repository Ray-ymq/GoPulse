package control

import (
	"encoding/json"
	"flag"
	"io"

	"github.com/Ray-ymq/GoPulse/lifecycle/internal/backup"
)

// BackupInvalid deliberately does not distinguish wrong credentials from
// ciphertext tampering. Both failures occur before creating Docker resources.
const BackupInvalid = 21

func inspectBackup(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("backup-inspect", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("archive", "", "private encrypted archive")
	source := fs.String("passphrase-file", "", "explicit private passphrase source; exact bytes")
	if fs.Parse(args) != nil || fs.NArg() != 0 || *path == "" || *source == "" {
		return fail(Usage, "backup-arguments", "backup-inspect requires --archive and --passphrase-file; passphrases are never command arguments")
	}
	pass, err := backup.ReadPassphrase(*source)
	if err != nil {
		return fail(Permission, "backup-secret-source", "use a private regular passphrase file (0600), containing 16..4096 exact bytes")
	}
	defer clear(pass)
	blob, err := backup.Read(*path)
	if err != nil {
		return fail(BackupInvalid, "backup-read", "cannot read a bounded private archive; check its permissions and size")
	}
	m, files, err := backup.Open(blob, pass)
	if err != nil {
		return fail(BackupInvalid, "backup-authenticate", "backup authentication or format invalid; verify the secret source and obtain an intact archive")
	}
	defer func() {
		for _, data := range files {
			clear(data)
		}
	}()
	secret, err := backup.OpenSecrets(files[backup.SecretEntry], pass)
	if err != nil {
		return fail(BackupInvalid, "backup-secrets", "encrypted secret entry invalid; obtain an intact archive")
	}
	clear(secret)
	// Do not emit raw manifest fields supplied by logical exporters, archive
	// contents, source paths, passphrases, or configuration/credential metadata.
	return json.NewEncoder(out).Encode(map[string]any{
		"schema": 1, "command": "backup-inspect", "status": "passed",
		"format": m.Format, "platform": m.Platform, "source_product_version": m.ProductVersion,
		"release_manifest_digest": m.ReleaseDigest, "operation_id": m.Operation,
		"started": m.Started, "finished": m.Finished, "files": len(m.Files),
		"domains": len(m.Domains), "cipher": m.Cipher, "kdf": m.KDF, "iterations": m.Iterations,
		"scope": "authenticated format only; does not establish data consistency or restore readiness",
	})
}

# GoPulse Linux amd64 product bundle

The supported product environment is a **local Linux amd64 Docker server**
(Engine >=24, Compose >=2.24). Docker requires at least 2 CPUs, 6 GiB RAM;
the installation filesystem requires at least 5 GiB available. Only the edge
publishes a loopback HTTP port. Remote endpoints, native services, Kubernetes,
macOS, Windows and arm64 product operation are not supported by this lifecycle.

Extract the versioned archive into a read-only bundle directory. Verify its
external `.sha256` before extraction and `checksums` afterward. The manifest
binds all images by index/platform digest and the embedded Compose checksum.
A loopback candidate registry is not an external/public release.

Create a separate empty **0700** installation directory; never use a directory
containing user files. Mount it at exactly the same absolute path inside the
tool container (Docker resolves file secrets on the host). The bundled tool Compose entry selects the lifecycle image by its immutable
`linux/amd64` digest, never a mutable tag.
The following is an invocation example, not a second lifecycle implementation:

```bash
# Set absolute BUNDLE and INSTALL paths. No repository checkout is needed.
mkdir -m 700 "$INSTALL"
export GOPULSE_BUNDLE_DIR="$BUNDLE" GOPULSE_INSTALL_DIR="$INSTALL"
export GOPULSE_TOOL_UID="$(id -u)" GOPULSE_TOOL_GID="$(id -g)"
export GOPULSE_SOCKET_GID="$(stat -c %g /var/run/docker.sock)"
docker compose -p gopulse-tool -f "$BUNDLE/compose.yaml" run --rm -T lifecycle \
  doctor --endpoint unix:///var/run/docker.sock --install "$INSTALL" --port 18080
```

Use the same invocation with `init`, `up`, `verify`, `status`,
`logs --service backend --tail 100`, `down`, then `up`. `--port` is set during
`init` (default 18080). Subsequent operations use the persisted port.
Only the short-lived tool has access to the Docker endpoint; endpoint access
is equivalent to Docker administration. Do not expose the socket to products.

All lifecycle output is JSON, schema 1. `state.json` and `secrets.json` are private
0600 files inside the 0700 installation directory. Do not print, archive without
encryption, commit, or publish these files. Repeated `init` explicitly refuses
the initialized directory and never rotates secrets. `up` pulls selected digests,
starts infrastructure, runs idempotent migration/Kafka/search jobs, then starts
services. Application registration remains the existing business flow; this
batch does not create a default administrator or ship a default password.

`verify` is read-only: no pulls, repairs, restarts, config writes or business
mutations. It checks ownership, service/job state and the unique edge binding.
`logs` accepts only bundle service names, optional RFC3339 `--since`/`--until`,
and `--tail 1..1000`; known installation credentials are redacted. Docker raw
errors and environment/config dumps are deliberately suppressed.

`down` retains data. `down --purge --confirm PROJECT` explicitly removes only
owned volumes; installation configuration is retained. Repeating `down` is safe.
Every mutating command uses a nonblocking installation lock and operation id.
Failure or SIGINT/SIGTERM records a non-ready phase and retains owned resources
for diagnosis/retry, never silently deletes data. Run `status` then retry `up`
or `down`. A foreign label/digest/name collision blocks cleanup rather than
adopting or deleting the foreign resource.

Exit codes: 0 success; 2 arguments/confirmation; 10 manifest/checksum/digest;
11 Docker/Compose unavailable or too old; 12 wrong server OS/arch; 13 disk/memory;
14 installation permissions/state; 15 occupied port; 16 installation lock;
17 ownership; 18 execution failure; 19 not ready; 20 interrupted operation;
21 invalid backup authentication/format.

## Consistent backup and empty-project restore

Use the same lifecycle invocation with:

```text
backup --install ABS_INSTALL --archive ABS_INSTALL/product.gpb --passphrase-file ABS_INSTALL/passphrase
backup-inspect --archive ABS_INSTALL/product.gpb --passphrase-file ABS_INSTALL/passphrase
restore --install ABS_INSTALL --archive ABS_INSTALL/product.gpb --passphrase-file ABS_INSTALL/passphrase
```

Keep the passphrase in a private 0600 regular file (16..4096 exact bytes), never
in command arguments or environment values. The archive is authenticated and
encrypted, bounded to 256 MiB. Keep an independent secure copy of the passphrase.
Backup takes a full maintenance window: edge writes are blocked, asynchronous
work drains, then SQL, search, metrics, messaging topology and plugin state are
exported at one cutover. Source service is resumed afterward.

Restore requires the **exact source bundle manifest**, Linux amd64 and a newly
initialized empty target. Run `init` with a new private installation directory
and unused port; copy the encrypted archive and private passphrase there before
`restore`. The tool mounts only the selected installation, not another source
installation. Never restore over an existing deployment. Users and roles persist;
JWT sessions are invalidated, Redis caches rebuild. Acknowledged Kafka history
is in ES/VM; the empty target topic rebases offsets to zero, retaining true source
offsets as backup evidence, rather than manufacturing broker messages.

A failed restore does not publish ready. Only new strongly owned target resources
are cleaned; retain the archive and retry `restore` after fixing the reported
cause. A pending restore cannot be bypassed with ordinary `up` or `verify`.
`doctor --diagnostics-dir ABS_PRIVATE_DIRECTORY` writes bounded metadata-only
JSON diagnostics in an existing private directory. Do not publish installation
state or secrets with a support report.

Legacy 1.9.4 upgrade is not implemented in this batch.
The registered legacy plugin remains upgrade input only. Existing Bash scripts
are development/acceptance tools, not the product installation path; historical
PowerShell files remain frozen at 0.2.1.

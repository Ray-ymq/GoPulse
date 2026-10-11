# Immutable release artifacts

From a committed source tree, one native entry point builds all ten images (nine
products plus the lifecycle tool) from the same Git archive, verifies them and
promotes the identical digests:

```bash
make package                                   # local amd64 candidate
make package RUNTIME=1                         # adds the real runtime and Compose gate
make package PLATFORM=linux/amd64,linux/arm64 RUNTIME=1 PROMOTE=1   # CI: both platforms, then promote
```

`make package` starts a uniquely named loopback registry when `REGISTRY` is not
given and removes only that container when it exits, so a clean checkout needs no
manual preparation. BuildKit needs arm64 build support; emulation is build
infrastructure only, not real arm64 product acceptance, so a dual-platform
candidate runs in CI. The runtime gate invokes the fixed full Compose acceptance
once with immutable candidate references and its existing cleanup and ownership
checks. It requires a real matching Linux amd64 server; every other platform
records a metadata-only receipt, and `PROMOTE=1` refuses to run without the
matching receipt of every platform in the candidate. Run from a clean
checkout/worktree; no user files are removed.

Exit codes: `0` success; `2` argument or precondition error (uncommitted source
tree, invalid registry namespace or platform, missing manifest, existing complete
candidate); `1` runtime failure. The implementation is
`lifecycle/cmd/gopulse-package`, which reuses the release manifest contract in
`lifecycle/internal/release`.

Do not reuse an output directory containing a complete manifest. Failed builds
leave build diagnostics, not a complete manifest. A Git archive prevents untracked
workspace contents from entering images. OCI platform configs, layers, labels,
ELF architecture and plugin digests are checked before bundle assembly.

`third-party.lock.json` records exact index and platform references. Build bases
are locked in `build-bases.lock.json` and Dockerfiles. Update these only for a
required compatibility change and record the affected regression. No third-party
version upgrade was required for Phase-16-01.

The manifest schema is closed. Validation also checks cross-field identity,
platform sets and catalog uniqueness. The Linux-only lifecycle module reads the
same v2 runtime contract, rejects duplicate JSON keys and checks tool/server
architecture without mutating the Docker server. Backend image aliases for
`backend-2` and `platform-api` resolve to the one manifest entry while lifecycle
labels preserve their runtime roles. Phase-16-02 adds the shared Linux amd64 product lifecycle via the Bundle
root `compose.yaml`; see `BUNDLE-README.md` for the Docker-only installation
contract. Linux amd64 backup and same-bundle empty-project restore use the shared lifecycle; see the [backup and restore contract](../../dev/operations/backup-restore.md). Legacy upgrade remains a separate batch.

For bundle checksum semantics and current limitations, see `BUNDLE-README.md`.
The detached archive checksum is the final transport checksum; the embedded
manifest records a non-circular payload checksum. Registry promotion copies the
same index and asserts byte-identical digest; it never rebuilds.

The historical `redis-1.9.4-source.tar.gz` is a deterministic `git archive` of
`exporters/redis` at `102aa4fa9bb5256dfd0733f42454b0ec5deaf043`. Its binary is built
with the locked Go 1.26.0 image, linux/amd64, CGO disabled, `-trimpath`,
`-buildvcs=false`, and `-ldflags='-s -w -buildid='`. Only the amd64 catalog registers
it; it does not imply that upgrade has been implemented.

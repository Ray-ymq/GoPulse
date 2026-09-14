# Immutable release artifacts

From a committed source tree, build all ten images (nine products plus the
lifecycle tool) from the same Git archive:

```bash
python3 scripts/ci/release_artifacts.py build --registry 127.0.0.1:15001/gopulse
scripts/verify-release-artifacts.sh --self-test
scripts/verify-release-artifacts.sh --manifest dist/release-manifest.json --platform linux/amd64 --runtime
scripts/verify-release-artifacts.sh --manifest dist/release-manifest.json --platform linux/arm64 --metadata-only
python3 scripts/ci/release_artifacts.py promote --manifest dist/release-manifest.json
```

The caller owns the registry and must expose a local probe registry on loopback
only. BuildKit needs arm64 build support; emulation is build infrastructure only,
not real arm64 product acceptance. The runtime gate invokes the fixed full Compose
acceptance once with immutable candidate references and its existing cleanup and
ownership checks. Run from a clean checkout/worktree; no user files are removed.

Do not reuse an output directory containing a complete manifest. Failed builds
leave build diagnostics, not a complete manifest. A Git archive prevents untracked
workspace contents from entering images. OCI platform configs, layers, labels,
ELF architecture and plugin digests are checked before bundle assembly.

`third-party.lock.json` records exact index and platform references. Build bases
are locked in `build-bases.lock.json` and Dockerfiles. Update these only for a
required compatibility change and record the affected regression. No third-party
version upgrade was required for Phase-16-01.

The manifest schema is closed. Python validation also checks cross-field identity,
platform sets and catalog uniqueness. The Linux-only lifecycle module reads the
same contract, rejects duplicate JSON keys and checks tool/server architecture
without mutating the Docker server. Phase-16-02 adds the shared Linux amd64 product lifecycle via the Bundle
root `compose.yaml`; see `BUNDLE-README.md` for the Docker-only installation
contract. Linux amd64 backup and same-bundle empty-project restore use the shared lifecycle; see `docs/releases/backup-restore.md`. Legacy upgrade remains a separate batch.

For bundle checksum semantics and current limitations, see `BUNDLE-README.md`.
The detached archive checksum is the final transport checksum; the embedded
manifest records a non-circular payload checksum. Registry promotion copies the
same index and asserts byte-identical digest; it never rebuilds.

The historical `redis-1.9.4-source.tar.gz` is a deterministic `git archive` of
`exporters/redis` at `102aa4fa9bb5256dfd0733f42454b0ec5deaf043`. Its binary is built
with the locked Go 1.26.0 image, linux/amd64, CGO disabled, `-trimpath`,
`-buildvcs=false`, and `-ldflags='-s -w -buildid='`. Only the amd64 catalog registers
it; it does not imply that upgrade has been implemented.

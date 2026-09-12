# GoPulse candidate bundle

This single LF-only bundle is independent of host operating system. Images run
on Linux containers, not as native macOS or Windows services. Product images,
third-party services and the lifecycle tool are selected by immutable OCI digest.

Phase 16-01 provides only `version` and manifest inspection. This is NOT yet a
complete install/up/down/backup/upgrade product. macOS arm64 and Windows amd64
support, an authorized external registry and full lifecycle operations remain
unaccepted until their designated Phase 16 batches.

The Compose file intentionally has no build context or acceptance-only service.
It preserves existing configuration requirements; it ships no credentials.
Do not start the application without separately supplied private configuration.

`checksums` covers the embedded manifest and assets. The detached
`gopulse-VERSION-bundle.tar.gz.sha256` covers the final archive. The manifest's
`bundle_sha256` is the SHA256 of sorted payload lines (`hex-sha256  POSIX-path` plus
LF), excluding `release-manifest.json` and `checksums`, to avoid circular hashing.
The payload consists of this README and `deploy/product/compose.yaml`.

The current release registers a historical Redis 1.9.4 amd64 archive only as an
upgrade input. `supported_upgrade_sources` remains empty until upgrade acceptance;
its presence does not mean an upgrade has already been implemented or accepted.

A local loopback-registry candidate is not an external release. Promotion must
preserve exact index/platform digests and the archive checksum without rebuilding.

# Redis release inputs

`redis-1.10.6-source.tar.gz` is a source-only `git archive` of
`exporters/redis` at `f1276484944eb933d2ebb40dc7c960a6a414b5b7`.
It contains no binary, runtime volume, configuration instance or credential.
The Monitor Dockerfile reproduces the historical Linux amd64 binary with
Go 1.26.0, `CGO_ENABLED=0`, `-trimpath -buildvcs=false -ldflags='-s -w -buildid='`,
and explicitly packages Manifest v1. The build rejects any mismatch against:

- archive SHA-256: `b992b0dfa80a0983b9af63e4c2a4770216bfd7fcb718af2cd451281cf3306727`
- executable SHA-256: `20978fc780e7531d6c130caf542d7e8ba6fe0e6842ba0610825d5e716941f2fa`
- schema digest: not applicable (v1).

The production `monitor` target compiles one current v2 package and this exact
legacy package into its release catalog. It does not silently upgrade migrated
state; an explicit administrator update selects the registered current package.
Unknown historical versions/content fail closed and retain their volume.
This batch establishes Linux amd64 support only, not the Phase 16 platform matrix.

The separate `monitor-acceptance` target also registers a deterministic failing
v2 `1.11.2` package and successful v2 `1.11.3` package. It uses the same verifier
and transaction code. The production target contains neither acceptance package,
and there is no runtime trust-bypass option.

Build the impacted images with version `1.11.1` before running
`scripts/verify-plugin-metrics.sh --sources redis --migration`. The focused runner
uses the proven `gopulse/monitor:1.10.6` and business/Playwright acceptance baseline
images plus the current `backend`, `frontend`, `router`, `marshaller` and
`monitor-acceptance` images. It always creates and removes its own random Compose
project/volumes and never mounts the daily product volume.

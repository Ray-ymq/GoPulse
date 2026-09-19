# Migration and message state reliability

`backend/migrations` is the only schema source. The current schema target remains
13; product version changes do not require placeholder DDL.

## Migration CLI

- `migrate validate`: validate contiguous numbered, unique matching up/down pairs;
  output JSON `binary_target` and `state=valid`. No database connection is needed.
- `migrate status`: output JSON `binary_target`, `database_version`, and one of
  `clean`, `behind`, `current`, `dirty`, `ahead`. An empty schema uses database
  version `-1`. Dirty/ahead are nonzero exits, not permission to repair data.
- `migrate up`: validate and acquire the MySQL migration lock, read the version
  under that lock, and apply only forward migrations. Each migration first records
  its dirty marker and clears it only after successful application. MySQL DDL is
  not generally transactional. A failed migration remains dirty.
- `migrate down`: **local development only**, one backward step. It is not a
  product rollback or recovery mechanism. Use the Phase 16 backup/restore commands.

Exit codes: 0 success/no change, 2 usage/configuration, 3 database connection,
4 dirty, 5 ahead, 6 migration lock timeout, 8 apply failure, 9 invalid source.
Error output contains fixed safe reasons, never the underlying SQL or DSN.

The Compose one-shot executes validate, status, up. Status exit 4 may proceed to
up, but up rejects every dirty version except the specifically resumable version
12. This does not authorize arbitrary force/clear. Backend, Worker and Indexer
remain dependent on successful completion of the job; Backend additionally checks
schema readiness.

`python3 scripts/ci/verify_migration_state.py` tests isolated real MySQL
empty/concurrent/current/repeat and dirty/ahead refusal. Its receipt is explicitly
**not** a substitute for the full Phase-17-04 candidate acceptance or the required
1.13.6 backup/restore upgrade evidence.

## Message processing

Rabbit retry/dead publications must be confirmed and the publication context must
still be valid before acknowledging the original. A cancellation requeues the
original. If a confirm races with cancellation, duplicate delivery is preferable
to silent loss and existing notification/search idempotency remains required.

Marshaller commits a stored record at most three times with bounded exponential
backoff, without repeating storage between commit attempts. Cancellation and lease
loss stop retries. Exhaustion returns the terminal commit error to the existing
nonzero-exit path; Compose retains its bounded restart policy. Storage backoff
also observes shutdown, not just ownership revocation.

Alert evaluation retains transactional lease/revision checks and persisted
unknown/stale behavior. Round-level panic returns to scheduling rather than
silently terminating the scheduler; evaluation failures use fixed reason codes.
Source evaluation-known and last-success metrics have only the three bounded
`alert_source` label values `metrics`, `logs`, `events` and do not change social
readiness. The reserved provenance label remains `source=backend`; alert sources
must not overwrite it. The admin frontend catalog mirrors these two gauges.

## Immutable candidate state acceptance

Run `scripts/verify-phase17-state.sh --from-manifest <1.13.6 bundle manifest>
--manifest <1.14.5 bundle manifest> --work <private directory>` on Linux amd64.
The source registry and candidate registry must be reachable. The runner validates
both complete bundles, binds its private journal to their SHA-256 identities, and
requires a clean checkout of the candidate revision (discovered from Git worktrees
or supplied with `--candidate-source`) for the existing Compose gate.

The data path uses the **source** release lifecycle to generate real data, back up,
inspect and restore it. Candidate `validate/status/up` then run against the restored
MySQL, followed by candidate Backend readiness, ordinary-user role denial and a new
searchable write. A dirty-version rejection is not repaired: a fresh formal source
restore must recover the pre-acceptance facts. This is a direct schema/data
compatibility test, not a new lifecycle upgrade command or an in-place deployment
upgrade guarantee. Existing dependencies in the restored installation remain owned
by the source lifecycle; full candidate integration is tested separately.

Business, Marshaller and alert verifiers accept `GOPULSE_RELEASE_MANIFEST` to use
manifest-pinned binaries/images rather than rebuilding production applications.
Test probes may still be compiled from the checkout. The unified runner executes
these and the candidate Compose gate serially, compares Docker resource inventories,
and writes private per-gate receipts. Successful gates are reused only for the same
manifest pair and verifier implementation with the retained log checksum intact.
`--migration-only` performs the scoped data path and deliberately leaves the overall
receipt incomplete. A failed gate never creates whole-batch success. Logs and
credentials stay in the private work directories; publish only the allowlisted
receipt, not journals, environment files or backup contents.

# GoPulse runtime contract v2 (2.3.2)

`deploy/runtime-contracts.json` is the machine-readable inventory of all thirteen
long-running Go processes. `deploy/runtime-contracts.schema.json` defines its
shape. Environment variables remain the only configuration input; the inventory
is not a second runtime configuration service. Validate changes with:

```bash
python3 scripts/ci/verify_runtime_contracts.py --contract deploy/runtime-contracts.json --compose deploy/compose.yaml --env .env.example --candidate 2.3.2
scripts/verify-runtime-contracts.sh --candidate 2.3.2
```

## Configuration and readiness

Each process validates its typed configuration before opening its listener or
starting consumption. Errors name the invalid key, never its value. Runtime
mode, timeout ceilings and credential separation are checked by shared
primitives. Existing keys remain canonical; this batch introduces no aliases.
The inventory records each process's actual keys, sensitivity, listeners,
hard/soft dependencies and shutdown budget.

All processes serve GET `/startup`, `/live`, `/ready` and `/health` on their
existing listener. `/health` aliases `/live`. Responses contain `status` and
`contract_version`, use JSON and `Cache-Control: no-store`. Non-GET requests
return 405, query/body requests 400. Readiness returns 503 until initialization,
on a hard dependency failure, and once shutdown begins. Liveness never performs
external I/O. Readiness checks share one slot, a finite timeout and a short cache;
a checker ignoring cancellation cannot create an unbounded number of checkers.

Backend readiness requires MySQL, current schema and bootstrap validity, not
Redis, RabbitMQ or observability availability. Worker and Indexer require their
actual consumer session and stores. Router requires Kafka; Marshaller requires
Kafka and its destination stores. Monitor requires initialized local state and
catalog; a temporarily unavailable publisher is recoverable. Exporters stay
ready when a target source fails and report their existing `up=0` metric.

Backend requests under `/api/v1` use finite, role-local admission slots. The
`business` and `combined` roles use `BACKEND_HTTP_MAX_CONCURRENCY` (default
`128`); the `platform` role uses `PLATFORM_API_HTTP_MAX_CONCURRENCY` (default
`32`). A saturated slot fails immediately with `503 backend_busy`; requests to
`/startup`, `/live`, `/ready` and `/health` remain on the probe contract and
still apply their own startup, dependency and stopping semantics. Compose
Backend healthchecks call
`http://127.0.0.1:8080/ready` directly inside each private container, without
Frontend, Nginx or the business admission path.

## Phase 21 service-role assembly

`BACKEND_SERVICE_ROLE` accepts `combined`, `business`, or `platform`; an empty
value defaults to `combined`. Role validation occurs before listeners,
connections, or background tasks are created. All roles use the shared MySQL
account store, JWT/Cookie session validation, request probes, structured logs
and private metrics. `business` additionally owns Redis, business
Elasticsearch, RabbitMQ and Outbox dispatch/sampling. `platform` additionally
owns observability Elasticsearch, VictoriaMetrics, Monitor, management APIs,
plugin control and alert evaluation. Unused role-specific configuration may be
omitted, while configuration for the selected role remains strictly validated.

The platform role uses `PLATFORM_API_MYSQL_MAX_OPEN_CONNS` (default `4`), while
business uses `MYSQL_MAX_OPEN_CONNS` (default `8`) and legacy combined mode keeps
its standalone default of `10`. Role-local
routes are registered only by their owning role; an API sent to the other role
therefore returns `404`. The deployed `backend` and `backend-2` services use
`business`; singleton `platform-api` uses `platform` and the same Backend image
digest. Their private container namespaces may reuse the Backend probe and
metrics ports without adding host ports.

Backend capacity diagnostics are fixed and low-cardinality: in-flight requests,
the configured concurrency limit, and rejected requests are exported as
`gopulse_backend_http_requests_in_flight`,
`gopulse_backend_http_concurrency_limit`, and
`gopulse_backend_http_rejected_total`. Request latency retains the historical
count and duration-total families and adds the fixed
`gopulse_backend_http_request_duration_seconds_bucket`, `_count`, and `_sum`
families. Bucket labels are `0.005`, `0.01`, `0.025`, `0.05`, `0.1`, `0.25`,
`0.5`, `1`, `2`, `5`, `10`, and `+Inf`; dimensions remain only method, route
template, status class, and the fixed `le` vocabulary. URL/query/identity and
request-correlation values are not labels.

No new host ports are published. The edge blocks new `/startup` and `/live`
paths and `/internal/`. Existing `/health` and `/ready` edge paths remain for
compatibility. Their body now uses runtime contract v2; the development status
page accepts it without inventing per-dependency status (unknown when omitted).
Use authenticated source-status APIs for detailed source health.

The machine contract is authoritative for process roles, Compose ownership,
replica identity, listener privacy, diagnostic paths, and finite connection,
queue, in-flight, and shutdown budgets. Diagnostics are direct private probes
against each named process; they do not route through Monitor and do not add
host-published ports. `GOPULSE_INSTANCE_ID` is the preferred bounded identity
source, with the contract's deterministic fallback used only for managed
singletons that have no standalone Compose service.

## Business replicas and bounded budgets

Compose runs two uniquely identified business Backend, Business Worker and Search Indexer
instances. The frontend uses one private upstream pool for Backend requests;
session routing does not depend on stickiness. Monitor receives explicit endpoint
lists (`backend,backend-2,platform-api` for Backend) so each named replica is
scraped rather than selected by DNS resolution. The platform alias uses Backend
component-metrics families and the `platform-api-1` identity.
`GOPULSE_INSTANCE_ID` is bounded, non-sensitive and diagnostic-only. Per-process
HTTP concurrency, MySQL pool size, worker prefetch and shutdown budgets remain
finite; the configured total MySQL budget must cover the declared replica count.
Outbox leases and Worker consumer tags are instance-scoped, while alert rule
leases remain single-owner facts in MySQL. This document does not claim that
external stateful stores are replicated.

The observability compute plane also runs two named Router and Marshaller
instances. Router publishes to a four-partition topic and rejects records when
its finite client buffer is full. Marshaller members use one consumer group,
manual commits and generation-scoped ownership; processing is concurrent across
owned partitions but remains serialized within each partition. In-flight and
retry slots are finite, and a revoked lease cannot write or commit.

Business search Elasticsearch and observation Elasticsearch are separate
services with separate named volumes and network membership. Backend uses
`elasticsearch` for business search and `observability-elasticsearch` for
logs/events; Search Indexer uses `elasticsearch` on the business network.
Marshaller logs/events use `observability-elasticsearch` on the observability
network; no Marshaller target can select the business search client. Metrics continue to use VictoriaMetrics,
and a failure in one observation target leaves other partitions and targets
bounded and diagnosable rather than creating an unbounded global retry queue.

## Shutdown

The first SIGTERM/SIGINT withdraws readiness before draining. HTTP requests,
consumer work, private listeners, shippers and child processes share bounded
shutdown deadlines rather than receiving sequential full budgets. Monitor
stops plugin slots concurrently. Successful drains exit zero; deadline or fatal
server/consumer failures exit nonzero. A second signal restores normal OS
termination behavior. Compose grace periods exceed the application budgets.
Managed plugin grace values inherit Monitor's shared deadline.

## Correlation and safe errors

The edge replaces caller-supplied `X-Request-ID` with 32 lowercase hex digits.
Internal HTTP clients propagate validated IDs. Responses and logs correlate
with `{ "error": { "code": "...", "message": "...", "request_id": "..." } }`.
IDs are diagnostic only and never confer authorization. Both frontends retain
status/code behavior and generic messages for unknown errors. Do not display
raw response bodies. A panic after headers are committed logs safely without
attempting to replace the response.

## Logs and release evidence

Go logs are single-line JSON with `log_schema_version`, UTC `timestamp`, `level`,
`service`, `module`, `message`, `event`, `version`, `revision`, `instance_id`,
`runtime_contract_version` and `runtime_mode`. Event names are finite:
`startup`, `shutdown`, `http_request`, `http_panic`, `dependency_up`,
`dependency_down`, `operation`. Repeated dependency failures are rate limited;
recovery resets suppression. Secrets, URL userinfo, payloads and raw filesystem
paths must not appear. Current official plugin logs reach Monitor stdout.
Monitor's shipping validator and Marshaller's vocabulary/mapping accept the
same fields. Existing strict daily indices receive only the additive runtime
keyword mapping before one deterministic-ID retry; incompatible records must
not be silently discarded to pass acceptance.

Release manifests and Bundles >=1.14.3 include both runtime files and their
checksums. Lifecycle manifest loading validates their identities and payload
checksums; older releases retain their original compatibility rules. Runtime
acceptance writes owned-project evidence with contract digest, probe/fault/
signal results and log digests under `.run/gopulse-runtime-*/`. Local private
environment files are never committed or included in release archives.

## Existing-installation caveat

Kafka explicitly uses `/var/lib/kafka/data`, its declared Compose named-volume
mount. The fixed full-product gate verifies that replacement preserves the
Topic ID and that a new correlated log becomes queryable before idle signal
checks. The previous image default wrote under `/tmp/kraft-combined-logs`; an
older live installation may therefore hold Kafka data in its container writable
layer. Do not recreate such a container assuming the named volume contains all
data. Preserve that data and establish an offline migration procedure first.
This batch does not claim historical automatic-upgrade compatibility or change
Kafka commit/rebalance semantics. The candidate is not externally promoted.

Backend recognizes persisted runtime metadata when decoding logs but retains
the existing public log-page projection, keeping both frontend validators and
management behavior compatible.

## Phase 18-05 closure evidence

The final closure candidate uses one immutable contract digest and executes the
fixed matrix exactly twice. Each run covers normal concurrency, business and
observability scale-up/down, short RabbitMQ/Kafka faults, both Elasticsearch
fault domains, VictoriaMetrics fault, single-instance SIGTERM, service
rebuild, and terminal closure. The runner records direct private startup,
liveness, readiness, and health probes, capacity signals, command output,
failure stages, cleanup ownership, and arithmetic averages without retrying a
third time. `target_met`, `boundary_found`, and `execution_failed` are honest
result classifications; a boundary or execution failure remains valid evidence
when both runs and their receipts are complete.

Use the following command for the fixed contract gate:

```bash
scripts/verify-runtime-contracts.sh --candidate 2.0.5
```

The Phase-18-05 closure gate (`scripts/verify-phase18-scale-closure.sh --repetitions 2`
followed by `python3 scripts/verify-phase18-evidence.py --closure <closure-directory>`) is
retired together with the Phase 18-20 formal matrix. It rejected missing run-2 evidence, any
run-3 artifact, candidate drift, mismatched numeric averages, command failures without retained
failure records, and ledger entries outside the Phase-18-05 file scope, and never inferred
successful capacity from a failed or boundary run. Source and original receipts remain in Git
history and [Phase-18 logs](../../logs/Phase-18/).

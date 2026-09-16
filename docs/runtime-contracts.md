# GoPulse runtime contract v1 (1.14.3)

`deploy/runtime-contracts.json` is the machine-readable inventory of all twelve
long-running Go processes. `deploy/runtime-contracts.schema.json` defines its
shape. Environment variables remain the only configuration input; the inventory
is not a second runtime configuration service. Validate changes with:

```bash
python3 scripts/ci/verify_runtime_contracts.py --contract deploy/runtime-contracts.json --compose deploy/compose.yaml --env .env.example
scripts/verify-runtime-contracts.sh --candidate 1.14.3
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

No new host ports are published. The edge blocks new `/startup` and `/live`
paths and `/internal/`. Existing `/health` and `/ready` edge paths remain for
compatibility. Their body now uses runtime contract v1; the development status
page accepts it without inventing per-dependency status (unknown when omitted).
Use authenticated source-status APIs for detailed source health.

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
`service`, `module`, `message`, `event`, `version`, `revision`,
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

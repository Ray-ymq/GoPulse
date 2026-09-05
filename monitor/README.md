# Monitor

Monitor is GoPulse's loopback-only plugin lifecycle and metrics collection service. It provides immediate and periodic Redis Exporter scrapes, strict Prometheus validation, Envelope v1 generation, and a bounded HTTP Publisher. Phase 7 formally connects that Publisher to the Message Router and Kafka transport.

## Runtime

Required configuration:

- `MONITOR_API_TOKEN`: internal Bearer token with at least 32 bytes;
- `MONITOR_PLUGIN_ROOT`: trusted absolute installation root;
- `REDIS_HOST`, `REDIS_PORT`, `REDIS_PASSWORD`, and `REDIS_DB`: the managed Exporter's Redis target.

Metrics collection defaults to `MONITOR_SCRAPE_INTERVAL=15s` and `MONITOR_SCRAPE_TIMEOUT=3s`; the timeout must be strictly less than the interval. For the normal Phase 7 lifecycle, `MONITOR_ROUTER_URL` points to the loopback Router and `MONITOR_ROUTER_TOKEN` equals `ROUTER_API_TOKEN`. Monitor posts Envelope v1 JSON to `/internal/v1/messages` with Bearer authentication and `Idempotency-Key`, accepting only `202 Accepted`. Publishing is not retried or persisted; a failure affects only that scrape and the next scrape proceeds normally. Monitor never imports a Kafka SDK or selects a Topic.

The default listener is `127.0.0.1:9090`. `GET /health` is public and reports process liveness. `GET /ready` and all `/internal/v1/exporter-plugins` routes require the Bearer token.

The plugin root contains an atomic `registry.json`, release directories, a relative `current` symlink, and a runtime process identity record. Registry and API data never include credentials, internal tokens, process arguments, or installation paths.

## Plugin lifecycle

Only the `redis-exporter` Manifest v1 contract is accepted. Installation and update enforce archive size/type/path limits, strict JSON fields, Linux/architecture matching, and the entrypoint SHA-256. Installation auto-starts the Exporter and commits only after `/health` succeeds. Start and stop are idempotent; updates preserve desired state and roll back to the previous release if the new process fails. Startup reconciles persisted desired state and restores a single owned process. A running plugin creates the fixed `redis-exporter-local` target; install/start/update trigger an immediate scrape, stop/update/shutdown cancel collection before the process changes, and plugin status exposes only recent scrape/success timestamps plus bounded safe errors.

Use `scripts/package-redis-exporter.sh` to create a deterministic package and `scripts/verify-monitor.sh` for isolated real-Redis lifecycle, strict metrics, target-failure, recovery, and HTTP Publisher contract acceptance. Use `scripts/verify-router.sh` for the real Redis Exporter → MetricsMonitor → Router → Kafka → bounded Consumer transport loop.

Phase 8 keeps Monitor's publishing contract unchanged and adds the downstream Marshaller/VictoriaMetrics closure. `scripts/verify-marshaller.sh` is the real Redis → Exporter → Monitor → Router → Kafka → Marshaller → VictoriaMetrics acceptance. Success, target-unavailable, recovery, and the record used for deterministic replay come from the real Monitor path; fixture production is limited to three representative permanent-invalid records used to prove safe continuation without storage writes.

## Application log ingest

`POST /internal/v1/logs` accepts one Schema v1 log from the fixed `backend`, `business-worker`, `search-indexer`, or `search-reindex` service vocabulary, up to `MONITOR_LOG_MAX_BYTES` (default 65536). It requires the dedicated `LOG_MONITOR_INGEST_TOKEN`, a unique 32-character lowercase hexadecimal `Idempotency-Key`, exact `application/json`, and no content encoding. Valid logs are strictly cleaned and published as the matching `logs/<service>` Envelope v1 source; only Router `202 Accepted` becomes Monitor `202 Accepted`. Invalid entries receive a safe 4xx response and unavailable transport receives `503 transport_unavailable`. Request IDs remain Backend-request scoped, while Worker/Indexer correlation uses the existing event ID.

## Lifecycle Events

Successful Redis Exporter install, start, stop, and update transitions are recorded after the Plugin Manager commits the final runtime and persistent state. The in-process EventMonitor validates the fixed Events v1 vocabulary, creates a stable 32-character lowercase hexadecimal message ID, and places the canonical `events/monitor` Envelope in a bounded queue. `Record` never waits for the Router and an enqueue or transport failure never changes the plugin API result. A single worker retries temporary Router failures with bounded backoff, skips deterministic 4xx rejections, and drains accepted records for at most `MONITOR_EVENT_SHUTDOWN_TIMEOUT` during shutdown. Queue and transport state logs never contain event bodies, URLs, tokens, or underlying errors.

The queue defaults to 256 entries (`MONITOR_EVENT_QUEUE_CAPACITY`), retry bounds default to `250ms` and `5s`, shutdown drain defaults to `5s`, and `MONITOR_EVENT_MAX_BYTES` is fixed at 16384. Monitor shutdown itself does not emit a plugin-stopped event. See `docs/events-v1.md` and `scripts/verify-events.sh`.

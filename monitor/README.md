# Monitor

Monitor is GoPulse's plugin lifecycle and metrics collection service. Direct source execution is loopback-only by default; the explicit container mode allows its internal Compose listener and validated service-DNS dependencies. It provides immediate and periodic Redis Exporter scrapes, strict Prometheus validation, Envelope v1 generation, and a bounded HTTP Publisher. Phase 7 formally connects that Publisher to the Message Router and Kafka transport.

## Runtime

Required configuration:

- `GOPULSE_RUNTIME_MODE`: defaults to `host`; Compose sets `container` explicitly;
- `MONITOR_API_TOKEN`: internal Bearer token with at least 32 bytes;
- `MONITOR_PLUGIN_ROOT`: trusted absolute installation root;
- `MONITOR_BOOTSTRAP_PACKAGE`: optional trusted absolute package path used for image-bundled startup reconciliation;
- `REDIS_HOST`, `REDIS_PORT`, `REDIS_PASSWORD`, and `REDIS_DB`: the managed Exporter's Redis target.

Metrics collection defaults to `MONITOR_SCRAPE_INTERVAL=15s` and `MONITOR_SCRAPE_TIMEOUT=3s`; the timeout must be strictly less than the interval. Host mode uses a loopback Router, while Compose uses `http://router:9091`; `MONITOR_ROUTER_TOKEN` equals `ROUTER_API_TOKEN` in both cases. Monitor posts Envelope v1 JSON to `/internal/v1/messages` with Bearer authentication and `Idempotency-Key`, accepting only `202 Accepted`. Publishing is not retried or persisted; a failure affects only that scrape and the next scrape proceeds normally. Monitor never imports a Kafka SDK or selects a Topic.

The host-mode listener defaults to `127.0.0.1:9090`. Container mode permits `0.0.0.0:9090`, but Compose exposes it only on internal networks and publishes no host port. The Monitor-owned Exporter still binds `127.0.0.1:9121` inside the same container, while its Redis target may be `redis:6379`. Unknown modes, fixed-IP or loopback container dependencies, unsafe origins, and control characters are rejected. `GET /health` is public and reports process liveness. `GET /ready` and all `/internal/v1/exporter-plugins` routes require the Bearer token.

The plugin root contains an atomic `registry.json`, release directories, a relative `current` symlink, and a runtime process identity record. Registry and API data never include credentials, internal tokens, process arguments, or installation paths.

## Plugin lifecycle

Only the `redis-exporter` Manifest v1 contract is accepted. Installation and update enforce archive size/type/path limits, strict JSON fields, Linux/architecture matching, and the entrypoint SHA-256. Installation auto-starts the Exporter and commits only after `/health` succeeds. Start and stop are idempotent; updates preserve desired state and roll back to the previous release if the new process fails. Startup reconciles persisted desired state and restores a single owned process. A running plugin creates the fixed `redis-exporter-local` target; install/start/update trigger an immediate scrape, stop/update/shutdown cancel collection before the process changes, and plugin status exposes only recent scrape/success timestamps plus bounded safe errors.

The `gopulse/monitor:<VERSION>` image runs as numeric user `10005:10001`, uses a read-only root filesystem plus the dedicated `monitor_plugin_data` volume, and embeds `/opt/gopulse/packages/gopulse-redis-exporter.tar.gz`. On an empty volume, startup installs and starts that package through the normal Plugin Manager path. A same-version restart preserves desired state, a newer embedded package performs the normal guarded update, and an older image is rejected rather than silently downgrading newer persisted state. The embedded package and `gopulse/redis-exporter:<VERSION>` image contain the same deterministic binary digest; Monitor remains the only runtime owner and never requires the Docker socket.

Use `scripts/package-redis-exporter.sh` to create a deterministic package and `scripts/verify-monitor.sh` for isolated real-Redis lifecycle, strict metrics, target-failure, recovery, and HTTP Publisher contract acceptance. Use `scripts/verify-router.sh` for the real Redis Exporter → MetricsMonitor → Router → Kafka → bounded Consumer transport loop.

Use the no-argument `scripts/verify-compose.sh` for the authoritative Phase-12-03 full-stack proof. It validates first-start bootstrap, single managed-Exporter ownership, same-volume replacement and down/up recovery, Router/Monitor failure isolation with social availability, administrator browser management, the blank-volume install/start/stop/update flow, image/package identity, bounded shutdown, and strongly owned cleanup.

Phase 8 keeps Monitor's publishing contract unchanged and adds the downstream Marshaller/VictoriaMetrics closure. `scripts/verify-marshaller.sh` is the real Redis → Exporter → Monitor → Router → Kafka → Marshaller → VictoriaMetrics acceptance. Success, target-unavailable, recovery, and the record used for deterministic replay come from the real Monitor path; fixture production is limited to three representative permanent-invalid records used to prove safe continuation without storage writes.

## Application log ingest

`POST /internal/v1/logs` accepts one Schema v1 log from the fixed `backend`, `business-worker`, `search-indexer`, or `search-reindex` service vocabulary, up to `MONITOR_LOG_MAX_BYTES` (default 65536). It requires the dedicated `LOG_MONITOR_INGEST_TOKEN`, a unique 32-character lowercase hexadecimal `Idempotency-Key`, exact `application/json`, and no content encoding. Valid logs are strictly cleaned and published as the matching `logs/<service>` Envelope v1 source; only Router `202 Accepted` becomes Monitor `202 Accepted`. Invalid entries receive a safe 4xx response and unavailable transport receives `503 transport_unavailable`. Request IDs remain Backend-request scoped, while Worker/Indexer correlation uses the existing event ID.

## Lifecycle Events

Successful Redis Exporter install, start, stop, and update transitions are recorded after the Plugin Manager commits the final runtime and persistent state. The in-process EventMonitor validates the fixed Events v1 vocabulary, creates a stable 32-character lowercase hexadecimal message ID, and places the canonical `events/monitor` Envelope in a bounded queue. `Record` never waits for the Router and an enqueue or transport failure never changes the plugin API result. A single worker retries temporary Router failures with bounded backoff, skips deterministic 4xx rejections, and drains accepted records for at most `MONITOR_EVENT_SHUTDOWN_TIMEOUT` during shutdown. Queue and transport state logs never contain event bodies, URLs, tokens, or underlying errors.

The queue defaults to 256 entries (`MONITOR_EVENT_QUEUE_CAPACITY`), retry bounds default to `250ms` and `5s`, shutdown drain defaults to `5s`, and `MONITOR_EVENT_MAX_BYTES` is fixed at 16384. Monitor shutdown itself does not emit a plugin-stopped event. See `docs/events-v1.md` and `scripts/verify-events.sh`.

## Phase-14-02 多 source

官方包目录现交付 Redis、MySQL、RabbitMQ。回环端口依次为 9121/9122/9123，配置、进程、
collector、desired state、事件和恢复按 plugin ID 隔离。新 MySQL/RabbitMQ 配置不会继承 Redis
凭据；每个子进程只注入本 source 的固定字段。每种成功快照必须通过其专属完整契约，
不允许用另一 source 的快照通过启动试验。账号部署见 `deploy/plugins/README.md`。

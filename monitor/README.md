# Monitor

Monitor is GoPulse's loopback-only plugin lifecycle and metrics collection service. Phase 6-03 adds immediate and periodic Redis Exporter scrapes, strict Prometheus validation, Envelope v1 generation, and an optional bounded HTTP Publisher.

## Runtime

Required configuration:

- `MONITOR_API_TOKEN`: internal Bearer token with at least 32 bytes;
- `MONITOR_PLUGIN_ROOT`: trusted absolute installation root;
- `REDIS_HOST`, `REDIS_PORT`, `REDIS_PASSWORD`, and `REDIS_DB`: the managed Exporter's Redis target.

Metrics collection defaults to `MONITOR_SCRAPE_INTERVAL=15s` and `MONITOR_SCRAPE_TIMEOUT=3s`; the timeout must be strictly less than the interval. `MONITOR_ROUTER_URL` is optional. When set, `MONITOR_ROUTER_TOKEN` must contain at least 32 bytes and Monitor posts Envelope v1 JSON to `/internal/v1/messages` with Bearer authentication and `Idempotency-Key`, accepting only `202 Accepted`. Publishing is not retried or persisted.

The default listener is `127.0.0.1:9090`. `GET /health` is public and reports process liveness. `GET /ready` and all `/internal/v1/exporter-plugins` routes require the Bearer token.

The plugin root contains an atomic `registry.json`, release directories, a relative `current` symlink, and a runtime process identity record. Registry and API data never include credentials, internal tokens, process arguments, or installation paths.

## Plugin lifecycle

Only the `redis-exporter` Manifest v1 contract is accepted. Installation and update enforce archive size/type/path limits, strict JSON fields, Linux/architecture matching, and the entrypoint SHA-256. Installation auto-starts the Exporter and commits only after `/health` succeeds. Start and stop are idempotent; updates preserve desired state and roll back to the previous release if the new process fails. Startup reconciles persisted desired state and restores a single owned process. A running plugin creates the fixed `redis-exporter-local` target; install/start/update trigger an immediate scrape, stop/update/shutdown cancel collection before the process changes, and plugin status exposes only recent scrape/success timestamps plus bounded safe errors.

Use `scripts/package-redis-exporter.sh` to create a deterministic package and `scripts/verify-monitor.sh` for isolated real-Redis lifecycle, strict metrics, target-failure, recovery, and HTTP capture acceptance.

# Monitor

Monitor is GoPulse's loopback-only plugin lifecycle service. Phase 6-02 adds the authenticated Plugin Manager and makes it the sole owner of the managed Redis Exporter process.

## Runtime

Required configuration:

- `MONITOR_API_TOKEN`: internal Bearer token with at least 32 bytes;
- `MONITOR_PLUGIN_ROOT`: trusted absolute installation root;
- `REDIS_HOST`, `REDIS_PORT`, `REDIS_PASSWORD`, and `REDIS_DB`: the managed Exporter's Redis target.

The default listener is `127.0.0.1:9090`. `GET /health` is public and reports process liveness. `GET /ready` and all `/internal/v1/exporter-plugins` routes require the Bearer token.

The plugin root contains an atomic `registry.json`, release directories, a relative `current` symlink, and a runtime process identity record. Registry and API data never include credentials, internal tokens, process arguments, or installation paths.

## Plugin lifecycle

Only the `redis-exporter` Manifest v1 contract is accepted. Installation and update enforce archive size/type/path limits, strict JSON fields, Linux/architecture matching, and the entrypoint SHA-256. Installation auto-starts the Exporter and commits only after `/health` succeeds. Start and stop are idempotent; updates preserve desired state and roll back to the previous release if the new process fails. Startup reconciles persisted desired state and restores a single owned process.

Use `scripts/package-redis-exporter.sh` to create a deterministic package and `scripts/verify-monitor.sh` for isolated real-Redis lifecycle acceptance.

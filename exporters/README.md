# Exporters

`exporters` contains independent pull-based metric adapters. Each exporter owns its module, configuration, process lifecycle, and public endpoint contract rather than importing Backend internals.

Phase 5 provides the first implementation:

- [`redis/`](redis/README.md) collects a fresh Redis 7.2.x `INFO` snapshot only when `/metrics` is requested;
- target failures are isolated from process health and return `503` with only `gopulse_redis_up 0`;
- no background polling, historical cache, active push, multi-target routing, or Monitor envelope is implemented here.

Phase 6 may make Plugin Manager the process owner and consume these metrics through MetricsMonitor, but it must preserve the Phase 5 executable, environment, endpoint, shutdown, and process-identity contracts.

# Exporters

`exporters` contains independent pull-based metric adapters. Each exporter owns its module, configuration, process lifecycle, and public endpoint contract rather than importing Backend internals.

Phase 5 provides the first implementation:

- [`redis/`](redis/README.md) collects a fresh Redis 7.2.x `INFO` snapshot only when `/metrics` is requested;
- target failures are isolated from process health and return `503` with only `gopulse_redis_up 0`;
- no background polling, historical cache, active push, multi-target routing, or Monitor envelope is implemented here.

Phase 5 integration acceptance runs the isolated Redis matrix separately from the full business stack, then validates the daily `dev.sh → verify.sh → down.sh` lifecycle. Use `scripts/verify-exporter.sh` for the real Redis success/failure/recovery contract and `scripts/verify-business.sh` for the retained Phase 0–4 regression.

Phase 6 may make Plugin Manager the process owner and consume these metrics through MetricsMonitor, but it must preserve the Phase 5 executable, environment, endpoint, shutdown, and process-identity contracts. MetricsMonitor can treat non-`200` `/metrics` responses as failed scrapes and parse successful responses as Prometheus text exposition 0.0.4; construction of the GoPulse metrics envelope remains Phase 6 work.

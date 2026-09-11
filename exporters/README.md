# Exporters

`exporters` contains independent pull-based metric adapters. Each exporter owns its module, configuration, process lifecycle, and public endpoint contract rather than importing Backend internals.

Phase 5 provides the first implementation:

- [`redis/`](redis/README.md) collects a fresh Redis 7.2.x `INFO` snapshot only when `/metrics` is requested;
- target failures are isolated from process health and return `503` with only `gopulse_redis_up 0`;
- no background polling, historical cache, active push, multi-target routing, or Monitor envelope is implemented here.

Phase 5 integration acceptance runs the isolated Redis matrix separately from the full business stack, then validates the daily `dev.sh → verify.sh → down.sh` lifecycle. Use `scripts/verify-exporter.sh` for the real Redis success/failure/recovery contract and `scripts/verify-business.sh` for the retained Phase 0–4 regression.

Phase 6 may make Plugin Manager the process owner and consume these metrics through MetricsMonitor, but it must preserve the Phase 5 executable, environment, endpoint, shutdown, and process-identity contracts. MetricsMonitor can treat non-`200` `/metrics` responses as failed scrapes and parse successful responses as Prometheus text exposition 0.0.4; construction of the GoPulse metrics envelope remains Phase 6 work.

## MySQL / RabbitMQ

Phase-14-02 新增 `mysql/`（10 families / 11 samples）与 `rabbitmq/`（9 families / 10 samples），
各自一个固定目标、独立回环端口及进程。使用通用 Manifest v2/config/Secret 管理，不复制 Manager。
源目录 README 记录锁定上游映射、最小权限和失败快照；账号交付见 `deploy/plugins/README.md`。

## Kafka / Elasticsearch

Phase-14-03 adds `kafka/` (7 families / 7 samples) and `elasticsearch/`
(12 families / 14 samples), with independent loopback ports 9124/9125. Kafka
requires formal Marshaller committed offsets and never initializes them.
Elasticsearch reads primary docs/store aggregates; yellow/red health is distinct
from an unreachable target. Exact source/target/producer labels are registered
at each metrics boundary; Redis's historical storage labels remain unchanged.
Use `scripts/verify-plugin-metrics.sh --sources kafka,elasticsearch` for the
owned real-target gate; see the batch development record for validation results.

### VictoriaMetrics

`victoriametrics/` adds the sixth independent official module. It authenticates
only to the locked target's `/metrics`, selects runtime counters/gauges rather
than stored `gopulse_*` series, and emits exactly nine label-free samples. See its
README for exact ingestion/query allowlists, last-hour active-series semantics,
row/byte scopes and the non-retention-specific storage deletion counter.

# GoPulse Redis Exporter

The Redis Exporter is an independent Go module and long-running process. It connects to one configured Redis target and performs exactly one `INFO server clients memory stats cpu keyspace` command for every accepted `GET /metrics` request. Startup and `/health` never probe Redis.

## Run

```bash
cd exporters/redis
REDIS_HOST=127.0.0.1 \
REDIS_PORT=6379 \
REDIS_PASSWORD=gopulse-redis \
REDIS_DB=0 \
REDIS_EXPORTER_HTTP_HOST=127.0.0.1 \
REDIS_EXPORTER_HTTP_PORT=9121 \
REDIS_EXPORTER_SCRAPE_TIMEOUT=2s \
REDIS_EXPORTER_SHUTDOWN_TIMEOUT=5s \
go run ./cmd/redis-exporter
```

The normal repository lifecycle builds `.run/bin/gopulse-redis-exporter`, starts it after Redis becomes healthy, and records `.run/redis-exporter.json`. `scripts/verify.sh` checks the process identity and both endpoints; `scripts/down.sh` validates the record before sending a signal. Existing `.env` files may omit the `REDIS_EXPORTER_*` keys when the documented defaults are acceptable; `dev.sh` resolves those defaults without rewriting the local file.

## Endpoints

- `GET /health` always returns HTTP `200` and `{"status":"ok","service":"redis-exporter"}` while the process can serve HTTP. It does not collect or express target readiness.
- `GET /metrics` returns Prometheus text exposition 0.0.4 with `Cache-Control: no-store`. A complete current snapshot returns HTTP `200` and `gopulse_redis_up 1`.
- Connection, authentication, timeout, command, or strict parsing failures return HTTP `503` and only the `HELP`, `TYPE`, and sample for `gopulse_redis_up 0`. Partial and previous successful values are never returned.
- Query strings and request bodies are rejected; non-GET methods return `405` and unknown paths return `404`.

## Metrics

| Metric | Type | Redis source |
| --- | --- | --- |
| `gopulse_redis_up` | gauge | current scrape result |
| `gopulse_redis_uptime_seconds` | gauge | `uptime_in_seconds` |
| `gopulse_redis_connected_clients` | gauge | `connected_clients` |
| `gopulse_redis_used_memory_bytes` | gauge | `used_memory` |
| `gopulse_redis_commands_processed_total` | counter | `total_commands_processed` |
| `gopulse_redis_keyspace_hits_total` | counter | `keyspace_hits` |
| `gopulse_redis_keyspace_misses_total` | counter | `keyspace_misses` |
| `gopulse_redis_cpu_seconds_total{mode="user|system"}` | counter | `used_cpu_user`, `used_cpu_sys` |
| `gopulse_redis_db_keys{db="N"}` | gauge | configured `dbN.keys` |
| `gopulse_redis_db_expiring_keys{db="N"}` | gauge | configured `dbN.expires` |

Only the fixed `mode` label and configured numeric `db` label are emitted. Target addresses, credentials, Redis errors, command names, keys, and raw `INFO` content are excluded from metrics and logs.

## Focused validation

```bash
(cd exporters/redis && go test -count=1 ./...)
scripts/verify-exporter.sh --self-test
scripts/verify-exporter.sh
```

The real acceptance uses a random, ownership-validated Compose project and Redis 7.2.5 volume. It proves live values, stopped-target isolation, authentication failure, timeout, recovery without exporter restart, bounded SIGTERM shutdown, and cleanup without changing the daily stack. Run `scripts/verify-business.sh` separately for the cross-component Phase 0–4 regression, and use the normal `dev.sh → verify.sh → down.sh` lifecycle to validate shared process ownership.

Phase 6 can launch the same executable with these environment variables, use `/health` for process liveness, and scrape `/metrics` periodically. It must avoid starting a second copy while `dev.sh` owns the process and must preserve the HTTP status, Prometheus 0.0.4, PID identity, and signal-shutdown boundaries described here.

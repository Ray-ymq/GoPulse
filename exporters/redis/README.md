# GoPulse Redis Exporter

The Redis Exporter is an independent Go module and long-running process. It connects to one configured Redis target and performs exactly one `INFO server clients memory stats cpu keyspace` command for every accepted `GET /metrics` request. Startup and `/health` never probe Redis. Direct source execution defaults to loopback-only host mode; explicit container mode permits a wildcard listener and validated Redis service DNS.

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

`GOPULSE_RUNTIME_MODE` defaults to `host`. Host mode requires `REDIS_EXPORTER_HTTP_HOST` and `REDIS_HOST` to remain loopback; container mode accepts `0.0.0.0` plus validated service DNS such as `redis`, while rejecting fixed IPs, `host.docker.internal`, control characters, and unknown modes. The Exporter managed by Monitor intentionally still uses `127.0.0.1:9121` inside the Monitor container, so only its parent can scrape it. The standalone `exporter` Compose profile uses the same binary in `gopulse/redis-exporter:<VERSION>` and exposes port 9121 only to the internal `business` network.

The final image runs `/usr/local/bin/gopulse-redis-exporter` as numeric user `10004:10001`, uses a read-only root filesystem, and publishes no host port. `scripts/package-redis-exporter.sh --binary ... --arch ...` can package an already-built executable deterministically. The Monitor image uses that path during its build, and acceptance verifies that the package entrypoint digest equals the standalone image binary digest.

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

The focused real acceptance uses a random, ownership-validated Compose project and Redis 7.2.5 volume. It proves live values, stopped-target isolation, authentication failure, timeout, recovery without exporter restart, bounded SIGTERM shutdown, and cleanup without changing the daily stack.

The no-argument Phase-12-03 `scripts/verify-compose.sh` verifies the standalone image against real Redis success, `up 0`, authentication failure, same-process recovery, and SIGTERM. The default complete stack does not start that profile as a duplicate runtime: Monitor bootstraps and remains the single owner of the embedded package, restores desired state from `monitor_plugin_data`, and preserves the HTTP status, Prometheus 0.0.4, process ownership, and signal-shutdown boundaries described here.

## Phase 14 one-shot check and package preparation

`gopulse-redis-exporter --check` uses the same Redis environment as the HTTP
exporter, collects and validates one real INFO snapshot, then exits without an
HTTP listener or persistent files. Success prints `{"reachable":true}` and exits
0. A target/authentication/INFO failure prints
`{"reachable":false,"code":"target_unavailable"}` and exits 1; configuration and
argument failures use `invalid_configuration` and `invalid_arguments`. Raw Redis
errors and credentials are never printed by this path.

`REDIS_EXPORTER_CONNECT_TIMEOUT` optionally sets a separate dial budget, from
100ms up to `REDIS_EXPORTER_SCRAPE_TIMEOUT`. If omitted, the historical scrape
budget remains the dial default. The check also applies an overall scrape context
deadline. Monitor independently bounds and reaps this command in its authenticated
connection-test API; the command itself never manages persistent plugin state.

An explicit v2 package can be prepared with:

```bash
bash scripts/package-redis-exporter.sh --contract-version 2 --version 1.11.1 \
  --output .run/packages/redis-v2.tar.gz
```

This uses the Monitor module's canonical schema generator and requires Go even
with `--binary`. The archive contains only the manifest, schema and executable;
it contains no configuration instance or credentials. The default is now v2. Use `--contract-version 1` only when reproducing an
explicitly supported historical package from its original source and toolchain. Package metadata and
checksums do **not** establish release trust: the v2 artifact must still be pinned
in an image-built release catalog before a manager can execute it. The batch development record contains the separate runtime and migration
acceptance evidence.

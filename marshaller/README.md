# Marshaller

Marshaller is GoPulse's loopback-only Kafka consumer and metrics transformation service. Phase 8-03 closes the fixed observability Topic and single-node VictoriaMetrics as a recoverable at-least-once pipeline:

```text
Redis -> Redis Exporter -> MetricsMonitor -> Message Router
      -> gopulse-observability-v1 -> Marshaller -> VictoriaMetrics
```

## Consumer and delivery contract

Marshaller uses franz-go with consumer group `gopulse-marshaller-metrics-v1`, initial offset `earliest`, automatic commits disabled, and Topic auto-creation disabled. Records are handled one at a time per the current single-partition Topic. The Kafka key must be the same 32-character lowercase hexadecimal `message_id` contained in the value.

A valid record is committed only after strict decoding, deterministic transformation, a `204 No Content` response with an empty body from `POST /api/v1/import/prometheus`, and a still-valid partition ownership lease. A permanently invalid record is not sent to storage and is committed only while ownership remains valid so the next record can proceed. Network, timeout, authentication, redirect, non-204, and unexpected-response failures are temporary: the current record remains uncommitted and is retried with bounded cancellable backoff. A commit failure while ownership is still valid halts partition progress, keeps liveness available, makes readiness fail, and requires a controlled process restart so the formal group offset remains the recovery source. Revoke or lost-partition cancellation during an in-flight commit is classified as ownership loss instead of a permanent commit failure; the old generation cannot commit and the consumer continues after replacement assignment.

Kafka and VictoriaMetrics outages do not change the recovery source of truth: the formal consumer group's committed offset remains authoritative. `/health` stays live while a dependency is unavailable, `/ready` fails with a bounded check, and the same process reconnects after a short outage. If Marshaller terminates after an HTTP result is unknown or before a commit, the replacement process re-fetches from the committed offset. Replayed records can therefore be written more than once; no local file or process state is used to infer storage acceptance.

This is an at-least-once pipeline. Deterministic series labels, millisecond timestamps, and VictoriaMetrics `-dedup.minScrapeInterval=1ms` provide limited idempotence for replayed points; they are not a Kafka/HTTP transaction or an exactly-once guarantee.

## Envelope and metrics contract

The decoder limits each Kafka value to 1 MiB, requires valid UTF-8 and one JSON object, recursively rejects duplicate object keys, and then applies `DisallowUnknownFields` to the typed Envelope, payload, samples, and labels. Envelope v1 currently accepts only `type=metrics`, `source=redis`, UTC RFC3339Nano timestamps no more than five minutes in the future, `redis-exporter`, stable three-part plugin versions, and `redis-exporter-local`.

A successful scrape contains the fixed 11-sample Redis set: up, uptime, connected clients, used memory, commands, hits, misses, user/system CPU, DB keys, and DB expiring keys. `target_unavailable` contains only `gopulse_redis_up=0`. Family, kind, label, finite-number, non-negative counter, sample-count, CPU mode, and shared DB label rules are validated again independently of Monitor.

Each sample becomes one sorted Prometheus import line with only the fixed `source="redis"`, `target_id="redis-exporter-local"`, and allowed `mode` or `db` labels. Values use `strconv.FormatFloat(..., 'g', -1, 64)`. All samples use the Envelope Unix timestamp truncated from nanoseconds to milliseconds. Samples and labels are sorted, escaped deterministically, terminated by `\n`, and limited to a 2 MiB body per Envelope. Replaying the same record therefore produces byte-identical import text.

## Runtime configuration

Required secrets are `MARSHALLER_API_TOKEN` (at least 32 bytes) and `MARSHALLER_VM_PASSWORD` (at least 16 bytes). Important fixed/default values are:

| Variable | Default or constraint |
| --- | --- |
| `MARSHALLER_HTTP_HOST` | `127.0.0.1`; IPv4 or IPv6 loopback IP only |
| `MARSHALLER_HTTP_PORT` | `9093` |
| `MARSHALLER_KAFKA_BROKERS` | `127.0.0.1:9092` |
| `MARSHALLER_KAFKA_TOPIC` | fixed `gopulse-observability-v1` |
| `MARSHALLER_KAFKA_GROUP` | fixed `gopulse-marshaller-metrics-v1` |
| `MARSHALLER_KAFKA_COMMIT_TIMEOUT` | `3s` |
| `MARSHALLER_VM_URL` | `http://127.0.0.1:8428`; loopback origin, no credentials/query/fragment |
| `MARSHALLER_VM_USERNAME` | `gopulse-marshaller` |
| `MARSHALLER_VM_TIMEOUT` | `3s` |
| `MARSHALLER_RETRY_MIN` / `MARSHALLER_RETRY_MAX` | `250ms` / `5s` |
| `MARSHALLER_READINESS_TIMEOUT` | `2s` |
| `MARSHALLER_SHUTDOWN_TIMEOUT` | `10s` |
| `MARSHALLER_FUTURE_SKEW` | `5m` |

Kafka polling is canceled only by the Marshaller run context; there is no separate application poll-timeout setting.

`GET /health` is public and reports process liveness only. `GET /ready` requires `Authorization: Bearer <MARSHALLER_API_TOKEN>` and performs bounded Kafka Topic and authenticated VictoriaMetrics checks. Browser cookies and Backend JWTs are not accepted as service identity. Logs never include message values, storage response bodies, or credentials.

## Lifecycle and validation

`scripts/dev.sh` starts Kafka, the explicit Topic initializer, and VictoriaMetrics before Router and Marshaller; Monitor and its managed Exporter start only after those dependencies are ready. `scripts/down.sh` stops Monitor/Exporter, Marshaller, and Router before Compose while preserving daily named volumes. `scripts/verify.sh` performs read-only process, dependency, readiness, volume, and fixed-query checks.

Focused validation:

```bash
(cd marshaller && test -z "$(gofmt -l .)")
(cd marshaller && go test -count=1 ./...)
(cd marshaller && go vet ./...)
(cd marshaller && go test -race -count=1 ./...)
scripts/verify-marshaller.sh --self-test
scripts/verify-marshaller.sh
```

The default acceptance uses a random owned Compose project, temporary credentials, and loopback ports. It seeds real Redis key, TTL, hit, miss, and command activity; verifies all 10 families/11 success samples and their fixed labels against Redis/Exporter evidence; captures a real Kafka record with bounded partition/offset/timestamp metadata; proves three representative structural, key/ID, and payload-contract rejections add no VictoriaMetrics rows before a later real message continues; checks target-unavailable/recovery without restarting Router, Marshaller, or Monitor; rejects browser cookies, Backend-style JWT/query tokens, and wrong internal credentials; retains offsets during VictoriaMetrics failure; proves same-process storage recovery, explicit uncommitted-record recovery after Marshaller restart, Kafka broker restart/formal-group rejoin, and a captured-real replay with one stable millisecond point; and finishes with unchanged `vm_rows_invalid_total` plus complete process/container/network/volume cleanup. The shell scenarios use observable dependency and offset transitions; exact delayed-acceptance and revoke/lost races remain covered by deterministic Consumer tests.

## Backend log storage

Marshaller dispatches the shared Envelope v1 stream through explicit `metrics/redis` and `logs/backend` handlers. Valid Backend logs are revalidated and written idempotently with the Envelope message ID as `_id` to `gopulse-logs-v1-YYYY.MM.DD`. The fixed `gopulse-logs-v1-template` installs a strict mapping and the `gopulse-logs-v1-read` alias. `MARSHALLER_ELASTICSEARCH_URL` must be a loopback HTTP origin and should match the Backend `ELASTICSEARCH_URL`; the default request timeout is 3 seconds.

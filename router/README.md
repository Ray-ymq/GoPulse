# Message Router

The Router is GoPulse's loopback-only internal transport boundary for observability messages. It accepts MetricsMonitor Envelope v1 JSON over HTTP, validates only the routing envelope, selects the fixed Kafka topic, and waits for Kafka acknowledgement before returning `202 Accepted`.

## Runtime contract

The Go module is `github.com/Ray-ymq/GoPulse/router` and builds two commands:

- `cmd/router`: the long-running HTTP Router;
- `cmd/verify-consumer`: a bounded acceptance-only Kafka reader that requires an explicit partition and `[start,end)` offset range and never commits offsets.

Endpoints:

| Method | Path | Authentication | Meaning |
| --- | --- | --- | --- |
| `GET` | `/health` | none | process liveness only; never queries Kafka |
| `GET` | `/ready` | `ROUTER_API_TOKEN` Bearer token | broker and fixed-topic metadata readiness |
| `POST` | `/internal/v1/messages` | `ROUTER_API_TOKEN` Bearer token | validate, route, produce, and wait for acknowledgement |

`POST` accepts only an unencoded `application/json` body up to `ROUTER_MAX_MESSAGE_BYTES`. The top-level JSON object must contain exactly `schema_version`, `message_id`, `type`, `source`, `timestamp`, and `payload`, without duplicates or trailing data. Phase 7 supports only schema `1`, type `metrics`, source `redis`, a 32-character lowercase hexadecimal message ID, a UTC RFC3339Nano timestamp, and a non-null object payload. `Idempotency-Key` must occur exactly once and equal `message_id`.

The Router does not validate or transform metric samples. On success, Kafka record key is the exact `message_id`, and record value is the original HTTP body byte sequence; it is never re-marshaled.

## Kafka contract

The only route is:

```text
metrics -> gopulse-observability-v1
```

Clients cannot select a topic through headers, query parameters, or payload fields. The franz-go producer uses `acks=all`, idempotent protocol writes, non-blocking admission against the configured 256-record and 8 MiB default client buffers, and a 3-second default delivery window. A full record/byte buffer is rejected immediately; canceling one HTTP caller does not globally abort other accepted records. Topic auto-creation is disabled by omission in the client and by the Kafka broker configuration. Compose creates the topic explicitly with one partition and replication factor one.

A timed-out in-flight request is uncertain: Kafka may have stored the record even though the Router did not return `202`. The Router has no background retry, disk spool, application-level deduplication, or transaction. Consumers must retain `message_id` and tolerate possible duplicates.

## Configuration

| Variable | Default or constraint |
| --- | --- |
| `ROUTER_HTTP_HOST` | `127.0.0.1`; must be an IP address |
| `ROUTER_HTTP_PORT` | `9091`; `1..65535` |
| `ROUTER_API_TOKEN` | required, at least 32 bytes, no CR/LF |
| `ROUTER_REQUEST_TIMEOUT` | `5s`; `1s..30s` |
| `ROUTER_SHUTDOWN_TIMEOUT` | `10s`; `1s..60s` |
| `ROUTER_MAX_MESSAGE_BYTES` | `1048576`; `1 KiB..1 MiB` |
| `ROUTER_KAFKA_BROKERS` | `127.0.0.1:9092`; unique bounded `host:port` list |
| `ROUTER_KAFKA_TOPIC` | fixed `gopulse-observability-v1` |
| `ROUTER_KAFKA_PRODUCE_TIMEOUT` | `3s`; `100ms..10s` and less than request timeout |
| `ROUTER_KAFKA_MAX_BUFFERED_RECORDS` | `256`; `1..1024` |
| `ROUTER_KAFKA_MAX_BUFFERED_BYTES` | `8388608`; `1 MiB..64 MiB` and not smaller than message limit |

The checked-in token in `.env.example` is for local development only.

## Lifecycle and validation

`scripts/dev.sh` starts healthy Kafka, runs the idempotent topic initializer, builds and starts Router, waits for authenticated readiness, and only then starts Monitor. `scripts/down.sh` stops Monitor and its Exporter before Router and finally stops Compose while preserving daily named volumes. `scripts/verify.sh` performs read-only ownership and readiness checks and never consumes a record.

Run focused validation with:

```bash
(cd router && go test -count=1 ./...)
(cd router && go vet ./...)
(cd router && go test -race -count=1 ./...)
scripts/verify-router.sh --self-test
scripts/verify-router.sh
```

The default Router acceptance uses a random isolated Compose project, loopback ports, Kafka volume, Redis target, plugin root, process set, and bounded Consumer identity. It proves direct byte integrity, invalid-request non-production, real Monitor `success` and `target_unavailable` Envelopes, Kafka outage/recovery without Router or Monitor restart, and ownership-safe cleanup. Its bounded JSON evidence lines retain the tested offset ranges, message IDs, record keys, value/body SHA-256 digests, scrape states, HTTP outage statuses, and stable Router/Monitor PIDs without printing tokens or raw message bodies.

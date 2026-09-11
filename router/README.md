# Message Router

The Router is GoPulse's internal transport boundary for observability messages. Direct source execution is loopback-only by default; explicit container mode permits its internal Compose listener and Kafka service DNS. It accepts MetricsMonitor Envelope v1 JSON over HTTP, validates only the routing envelope, selects the fixed Kafka topic, and waits for Kafka acknowledgement before returning `202 Accepted`.

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
| `GOPULSE_RUNTIME_MODE` | `host`; only `host|container` are accepted |
| `ROUTER_HTTP_HOST` | `127.0.0.1`; host mode requires loopback, container mode permits wildcard binding |
| `ROUTER_HTTP_PORT` | `9091`; `1..65535` |
| `ROUTER_API_TOKEN` | required, at least 32 bytes, no CR/LF |
| `ROUTER_REQUEST_TIMEOUT` | `5s`; `1s..30s` |
| `ROUTER_SHUTDOWN_TIMEOUT` | `10s`; `1s..60s` |
| `ROUTER_MAX_MESSAGE_BYTES` | `1048576`; `1 KiB..1 MiB` |
| `ROUTER_KAFKA_BROKERS` | `127.0.0.1:9092`; host mode requires loopback, container mode accepts validated service DNS such as `kafka:19092` |
| `ROUTER_KAFKA_TOPIC` | fixed `gopulse-observability-v1` |
| `ROUTER_KAFKA_PRODUCE_TIMEOUT` | `3s`; `100ms..10s` and less than request timeout |
| `ROUTER_KAFKA_MAX_BUFFERED_RECORDS` | `256`; `1..1024` |
| `ROUTER_KAFKA_MAX_BUFFERED_BYTES` | `8388608`; `1 MiB..64 MiB` and not smaller than message limit |

The checked-in token in `.env.example` is for local development only.

## Lifecycle and validation

`scripts/dev.sh` builds `gopulse/router:<VERSION>` and starts it after healthy Kafka and the idempotent Topic initializer. The image runs as numeric user `10002:10001`, uses `/usr/local/bin/router` as PID 1, has a read-only root filesystem, and publishes no host port. Router joins only the internal `observability` network. `scripts/down.sh` stops the complete Compose project while preserving daily named volumes, and `scripts/verify.sh` performs read-only ownership, image, port, and readiness checks without consuming a record.

Run focused validation with:

```bash
(cd router && go test -count=1 ./...)
(cd router && go vet ./...)
(cd router && go test -race -count=1 ./...)
scripts/verify-router.sh --self-test
scripts/verify-router.sh
```

The default Router acceptance uses a random isolated Compose project, loopback ports, Kafka volume, Redis target, plugin root, process set, and bounded Consumer identity. It proves direct byte integrity, invalid-request non-production, real Monitor `success` and `target_unavailable` Envelopes, Kafka outage/recovery without Router or Monitor restart, and ownership-safe cleanup. Its bounded JSON evidence lines retain the tested offset ranges, message IDs, record keys, value/body SHA-256 digests, scrape states, HTTP outage statuses, and stable Router/Monitor PIDs without printing tokens or raw message bodies.

The authoritative Phase-12-03 full-stack gate is the no-argument `scripts/verify-compose.sh`. It validates Router's image and internal-only network contract, Bearer identity, service-DNS Kafka connection, Browser → Backend isolation, Router failure as a localized observability degradation while social writes continue, same-volume Kafka/Router recovery, bounded shutdown, and strongly owned Compose cleanup. The focused `verify-router.sh` remains useful for byte-level source diagnostics but is not the completion gate.

Phase 8 keeps Router as the byte-preserving producer for the unchanged record contract. Marshaller independently validates the original bytes, writes accepted metrics to VictoriaMetrics, and commits through `gopulse-marshaller-metrics-v1`; the Phase 8-03 acceptance captures a real Router-produced record for deterministic replay and confirms Router never parses, cleans, stores, or commits metrics payloads.

Router accepts `metrics/redis` plus the fixed `logs/backend`, `logs/business-worker`, `logs/search-indexer`, and `logs/search-reindex` Envelope v1 combinations. Every accepted type uses the single fixed `gopulse-observability-v1` Topic; the message ID remains the Kafka key and the HTTP request body remains the exact Kafka value bytes. Source, query, header, or payload values can never select a Topic.

## Events routing

The routing allowlist includes `events/monitor` in addition to the established Metrics and Logs combinations. Every accepted Events Envelope is produced unchanged to the fixed `gopulse-observability-v1` Topic with its `message_id` as the Kafka key. Unknown Events sources, schema/type/source mismatches, browser credentials, request-selected topics, and payload-selected topics remain unsupported.


## Phase 14 component runtime metrics

Protected component metrics use separate internal listeners, not the public/API
listener. Configure the distinct `*_METRICS_TOKEN` values in `.env.example`;
Monitor holds the six read-only tokens. Compose publishes no metrics ports.
The exact family/label/initial-value contracts, shutdown behavior, source/target
identities and focused acceptance command are in `docs/component-metrics.md`
(relative to the repository root). The shared standard-library-only
`componentmetrics` module is required alongside this module for source builds;
Docker builds copy it explicitly.

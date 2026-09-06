# GoPulse Backend

The Backend exposes authenticated social APIs and administrator-only operational APIs under `/api/v1`.

## Exporter management boundary

All `GET`/`POST /exporter-plugins...` routes execute authentication and database-authoritative administrator authorization before contacting Monitor. Successful Monitor bodies are limited to 1 MiB and recursively reject duplicate keys, unknown fields, trailing content, invalid types, non-UTC timestamps, unstable versions, unknown states, unsafe error text, and impossible state/time combinations. The Backend constructs the public `ExporterStatus` and `SafeError` DTOs only after validation; redirect, timeout, network, oversized, malformed, or unexpected-success responses map to `503 monitor_unavailable`. The exact safe-error allowlist covers both Plugin Manager lifecycle failures and Monitor's fixed metrics scrape/publish failures, so a localized Router or target outage remains visible as a safe Exporter fact instead of making an otherwise valid status untrusted. Known Plugin Manager business errors retain their stable public status and code.

Install and update accept exactly one streaming multipart field named `package`, enforce the Backend 64 MiB limit, and forward a newly generated multipart boundary. Browsers never receive the Monitor token, internal URL, process identity, paths, commands, environment, or raw response.

## Observability queries

All three endpoints execute `RequireAuthentication` followed by the database-authoritative `RequireAdmin` middleware:

- `GET /observability/metrics?metric=<fixed-family>&range=15m|1h|6h|24h`
- `GET /observability/logs`
- `GET /observability/events`

Metrics queries are built exclusively from the server catalog in `internal/metricquery`. The VictoriaMetrics client uses `POST /prometheus/api/v1/query_range`, Basic Auth from Backend configuration, redirect rejection, a bounded timeout, and a 2 MiB response limit. The response validator accepts only matrix data with the fixed Redis provenance labels and family-specific public labels. VictoriaMetrics is intentionally not part of `/ready`; its failure maps only the Metrics API to `503 metrics_unavailable`.

Required local settings are documented in the root `.env.example`:

```text
BACKEND_VICTORIAMETRICS_URL=http://127.0.0.1:8428
BACKEND_VICTORIAMETRICS_USERNAME=gopulse-marshaller
BACKEND_VICTORIAMETRICS_PASSWORD=<at-least-32-bytes>
BACKEND_VICTORIAMETRICS_QUERY_TIMEOUT=3s
BACKEND_METRIC_QUERY_DEFAULT_RANGE=15m
BACKEND_METRIC_QUERY_MAX_RANGE=24h
```

The Backend query identity must match the local VictoriaMetrics identity used by Marshaller. Credentials and raw upstream bodies must never be logged or returned by the API.

## Browser and lifecycle acceptance

Phase 11 established the administrator browser and trust-boundary contract with `scripts/verify-observability-ui.sh`. Phase-12-02 carries that contract into the complete container topology through `scripts/verify-compose.sh --observability`: Chromium reaches only Frontend/Backend, ordinary users remain denied before internal calls, administrators query real Metrics/Logs/Events and manage the Monitor-owned Exporter, and representative observability failures remain localized.

## Phase 12 container runtime contract

Direct source commands default to `GOPULSE_RUNTIME_MODE=host`, which retains loopback listener and dependency-origin restrictions. Compose sets `GOPULSE_RUNTIME_MODE=container`; in that mode the Backend listens on `0.0.0.0:8080` and accepts validated service DNS only. The complete topology uses `mysql:3306`, `redis:6379`, `rabbitmq:5672`, `elasticsearch:9200`, `monitor:9090`, and `victoriametrics:8428`; Backend, Business Worker, and Search Indexer ship bounded log copies to Monitor without adding it to business readiness. Loopback or fixed-IP container dependencies, `host.docker.internal`, unknown modes, malformed origins, URL paths, query strings, fragments, embedded HTTP credentials, and control characters are rejected as applicable.

`gopulse/backend:<VERSION>` runs `/usr/local/bin/server` by default and also contains `/usr/local/bin/migrate`, `/usr/local/bin/search-reindex`, and `/usr/local/bin/admin-role` for explicit one-shot Compose commands. `gopulse/business-worker:<VERSION>` and `gopulse/search-indexer:<VERSION>` are independent final images. All three runtime images use numeric UID/GID `10001:10001`, run the application as PID 1, and write version/revision/source OCI labels.

The default Compose topology exposes no MySQL, Redis, RabbitMQ, Elasticsearch, Kafka, VictoriaMetrics, Monitor, Router, Marshaller, or Exporter host port. Use `scripts/dev.sh`, `scripts/verify.sh`, and `scripts/down.sh` for the daily complete container lifecycle, `scripts/verify-compose.sh --business` for the focused social/search regression, and `scripts/verify-compose.sh --observability` for the authoritative Phase-12-02 browser-backed closure. `deploy/compose.debug.yaml` exists only for historical source-level regression scripts.

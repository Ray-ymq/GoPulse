# GoPulse Backend

The Backend exposes authenticated social APIs and administrator-only operational APIs under `/api/v1`.

## Exporter management boundary

All `GET`/`POST /exporter-plugins...` routes execute authentication and database-authoritative administrator authorization before contacting Monitor. Successful Monitor bodies are limited to 1 MiB and recursively reject duplicate keys, unknown fields, trailing content, invalid types, non-UTC timestamps, unstable versions, unknown states, unsafe error text, and impossible state/time combinations. The Backend constructs the public `ExporterStatus` and `SafeError` DTOs only after validation; redirect, timeout, network, oversized, malformed, or unexpected-success responses map to `503 monitor_unavailable`. Known Plugin Manager business errors retain their stable public status and code.

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

## Phase 11 final browser and lifecycle acceptance

`scripts/verify-observability-ui.sh` now performs the Milestone 3 integration matrix against an isolated random Compose project and `/tmp` process/plugin root. It starts Monitor without preinstalling a plugin, installs and upgrades the real Redis Exporter through the browser, validates fixed Metrics plus paged Logs/Events, applies owned VictoriaMetrics/Monitor/Elasticsearch fault windows, and confirms current-role authorization after an isolated SQL demotion. `scripts/verify.sh` and `scripts/down.sh` accept the same restricted `GOPULSE_PROJECT_NAME`, `GOPULSE_ENV_FILE`, and `GOPULSE_RUN_DIR` overrides so the final `dev.sh → verify.sh → down.sh` sequence never targets the daily stack.

# GoPulse Backend

The Backend exposes authenticated social APIs and administrator-only operational APIs under `/api/v1`.

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

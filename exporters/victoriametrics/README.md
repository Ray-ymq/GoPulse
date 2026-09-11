# VictoriaMetrics Exporter

Independent official single-target plugin for Compose's locked
`victoriametrics/victoria-metrics:v1.151.0`. The runtime performs only authenticated
`GET /metrics`; it never queries stored `gopulse_*` series, imports data, deletes
series, or triggers merges. One bounded 1 MiB snapshot produces exactly nine
families / nine samples with no labels. Any selected missing/duplicate/nonfinite
or negative value fails the entire snapshot: HTTP 503 and only
`gopulse_victoriametrics_up 0`. Upstream errors, responses and credentials are not
returned. Unknown upstream metrics are ignored, not forwarded.

## Locked mapping

All output names below have prefix `gopulse_victoriametrics_`. Each selected
upstream sample must occur exactly once; no missing-to-zero convention is used.
Counters retain upstream resets. `S` means the three fixed type values
`storage/inmemory`, `storage/small`, `storage/big`.

| Output | Upstream / exact selected labels | Aggregation / unit / kind |
| --- | --- | --- |
| up | Full authenticated snapshot validated | 1 on success / boolean / gauge |
| rows_inserted_total | `vm_rows_inserted_total{type=...}`: csvimport, datadogsketches, datadogv1, datadogv2, graphite, influx, native, newrelic, opentelemetry, opentsdb, opentsdbhttp, prometheus, promremotewrite, promscrape, vmimport, zabbixconnector | Sum 16 ingestion protocol counters / rows / counter; received by the ingestion layer, not a promise every row was stored |
| query_requests_total | `vm_http_requests_total{path="/api/v1/query"}` and `{path="/api/v1/query_range"}` | Sum 2 / requests / counter; excludes metrics, import, admin and all other HTTP paths |
| active_timeseries | `vm_cache_entries{type="storage/hour_metric_ids"}` | One sample / series / gauge; upstream last-hour active-series window, not cumulative series creation or immediate deletion visibility |
| storage_rows | `vm_rows{type=S}` | Sum 3 / data-point rows / gauge; storage tiers including in-memory, excludes indexdb rows |
| storage_size_bytes | `vm_data_size_bytes{type=...}`: S, storage/metaindex, indexdb/inmemory, indexdb/file, indexdb/metaindex | Sum 7 / bytes / gauge; upstream data+index size accounting including metadata and in-memory tiers, not filesystem usage or process memory |
| free_disk_space_bytes | `vm_free_disk_space_bytes{path="/victoria-metrics-data"}` | One sample / bytes / gauge; exact locked storage path; path never emitted |
| active_merges | `vm_active_merges{type=...}`: S, indexdb/inmemory, indexdb/file | Sum 5 / active merge operations / gauge; storage and indexdb |
| storage_rows_deleted_total | `vm_rows_deleted_total{type=S}` | Sum 3 / rows / counter; deletion reported by these storage merge tiers, NOT retention-only and not all expiry cleanup paths |

The locked official dashboard `dashboards/victoriametrics.json` (tag v1.151.0)
uses `storage/hour_metric_ids` for active series, non-indexdb `vm_rows` for data
points, `vm_data_size_bytes` for data/index accounting, and `vm_active_merges`
for running merges. Exact label sets and explicit cold-start zero samples were
observed on the pinned image. The Phase-14-04 log records evidence and the
user-approved deletion-counter correction. New upstream types are not implicitly
added. No path/matcher/URL can be supplied in plugin configuration.

## Configuration and lifecycle

Schema: host, port, username, password (Secret), connect_timeout, scrape_timeout.
Container origin is only `http://victoriametrics:8428`; host mode allows only
loopback at 8428, with the same locked storage-path contract. Basic Auth is
mandatory. Proxy, redirects and response compression are disabled. Timeouts
are 100 ms–10 s with connect <= scrape. The child listens only on loopback
port 9126; `/health` identifies the process, `/metrics` reports the target, and
`--check` runs the same full snapshot validation without installation.

When VictoriaMetrics itself is unavailable, Monitor's safe target state and
Events (when deliverable) are current evidence; Backend metrics queries can be
unavailable for every source. No local persistent queue, second storage or
synthetic outage points are created. After recovery, query a newly collected
up=1 and the full snapshot; do not claim up=0 was stored while storage was down.

```bash
(cd exporters/victoriametrics && go test ./...)
bash scripts/package-redis-exporter.sh --source victoriametrics --version 1.11.4
```

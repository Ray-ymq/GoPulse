# Elasticsearch Exporter

The official single-target plugin is packaged as Manifest v2, managed by Monitor,
and registered through Router, Marshaller and the Backend metric catalog.
See the Phase-14-03 development record for actual acceptance status and evidence.

Only GET `/_cluster/health` and `/_stats/docs,store?level=cluster` are used.
The latter supplies `_all.primaries.docs.count` and
`_all.primaries.store.size_in_bytes`, never replica-inclusive `_all.total`.
Missing fields, reported shard failures, health timeouts, authentication,
transport, and parsing errors fail safely. Yellow/red health alone is not a
transport failure; unassigned replicas need not make successful shard count
equal total shard count.

Twelve gauge families (14 samples) use the `gopulse_elasticsearch_` prefix:
`up`, `cluster_health_status`, `nodes`, `data_nodes`, `active_primary_shards`,
`active_shards`, `relocating_shards`, `initializing_shards`, `unassigned_shards`,
`pending_tasks`, `documents`, `store_size_bytes`. Only health has labels:
three one-hot `status=green|yellow|red` samples. Failure is HTTP 503 with only
`gopulse_elasticsearch_up 0`. No node, index, or shard names are emitted.

Required environment: `ELASTICSEARCH_HOST`, `ELASTICSEARCH_PORT`,
`ELASTICSEARCH_EXPORTER_CONNECT_TIMEOUT`, `ELASTICSEARCH_EXPORTER_SCRAPE_TIMEOUT`.
Optional authentication requires both `ELASTICSEARCH_USERNAME` and
`ELASTICSEARCH_PASSWORD`. Container mode restricts the origin to
`elasticsearch:9200`; host mode allows loopback port 9200 only. HTTP binds to
loopback 9125. Redirects, environment proxies, and compressed responses are
not followed/accepted. Responses are bounded to 1 MiB. No custom API paths,
queries, search bodies, index operations, or TLS paths are configurable.

## Metric mapping

| Suffix | Upstream field (health unless stated) | Unit / samples |
| --- | --- | --- |
| up | Required reads and complete fields | boolean / 1 |
| cluster_health_status | status, one-hot green/yellow/red | boolean / 3 |
| nodes / data_nodes | number_of_nodes / number_of_data_nodes | count / 1 each |
| active_primary_shards / active_shards | Same-named health fields | count / 1 each |
| relocating_shards / initializing_shards / unassigned_shards | Same-named health fields | count / 1 each |
| pending_tasks | number_of_pending_tasks | count / 1 |
| documents | `_all.primaries.docs.count` from index stats | count / 1 |
| store_size_bytes | `_all.primaries.store.size_in_bytes` from index stats | bytes / 1 |

The ordinary locked Compose target has security disabled, so both authentication
fields are omitted. When authentication is enabled, the documented named
privileges are cluster `monitor` for health and index `monitor` for stats on the
complete monitored index scope. No `read`/`write`/`manage` index privilege or
security administration privilege is needed. Index stats is not a search or
`_source` read. The owned gate uses a separate private real 9.5.2 security-enabled
target to check allowed reads, forbidden index creation/search, wrong-password
failure and recovery without restarting the Exporter; it never enables security
on the normal business target.

Validation: `go test ./...`; from repository root:
`bash scripts/verify-plugin-metrics.sh --sources kafka,elasticsearch`.

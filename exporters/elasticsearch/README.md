# Elasticsearch Exporter — Phase-14-03 foundation (not released)

This module is not yet packaged, registered, or enabled in GoPulse. Phase-14-03
is incomplete; see its development record. Unit fixtures do not establish real
Elasticsearch or end-to-end acceptance.

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

Local validation: `go test ./...`. The real 9.5.2 primary aggregation,
authentication permissions, yellow/red recovery, and all product integration
remain pending.

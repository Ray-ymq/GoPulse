# Kafka Exporter

The official single-target plugin is packaged as Manifest v2, managed by Monitor,
and registered through Router, Marshaller and the Backend metric catalog.
See the Phase-14-03 development record for actual acceptance status and evidence.

The client uses the existing franz-go v1.21.0 / kmsg v1.13.1 dependencies,
pinning request shapes to Kafka 2.8 (OffsetFetch v7). It issues only Metadata,
ListOffsets (log end), and OffsetFetch requests for `gopulse-observability-v1`
and `gopulse-marshaller-metrics-v1`. Metadata also checks that the internal
`__consumer_offsets` topic already exists before requesting a coordinator; this
prevents the broker from initializing its internal topic as a side effect of a
cold connection test. Auto-creation is disabled; it neither
produces/consumes records nor commits offsets. Every partition requires a valid
committed offset; lag is the sum of `max(log_end - committed, 0)`.

Seven gauge families have no labels: `gopulse_kafka_` + `up`, `brokers`,
`controller_available`, `partitions`, `under_replicated_partitions`,
`offline_partitions`, `consumer_group_lag`. Failed collection returns HTTP 503
with only `gopulse_kafka_up 0`; `/health` reports process liveness.

Required environment: `KAFKA_HOST`, `KAFKA_PORT`, `KAFKA_TOPIC`,
`KAFKA_CONSUMER_GROUP`, `KAFKA_EXPORTER_CONNECT_TIMEOUT`,
`KAFKA_EXPORTER_SCRAPE_TIMEOUT`. `GOPULSE_RUNTIME_MODE=container` restricts the
origin to `kafka:19092`; host mode allows only loopback port 9092. Advertised
broker dialing is also restricted to the configured origin. The HTTP listener
is loopback-only on 9124. `--check` prints only a safe reachability result.

## Metric mapping

All values are gauges; only the fixed application topic contributes partition/lag values.

| Suffix | Protocol field / formula | Unit |
| --- | --- | --- |
| up | All required reads and fields succeeded | boolean |
| brokers | Metadata brokers length | count |
| controller_available | Metadata controller ID belongs to returned brokers | boolean |
| partitions | Fixed topic partitions length | count |
| under_replicated_partitions | Count of partitions with fewer ISR than assigned replicas | count |
| offline_partitions | Count of partitions without a leader; only publish if the entire offset snapshot remains readable | count |
| consumer_group_lag | Sum of `max(ListOffsets latest - OffsetFetch committed, 0)` | count |

No broker/partition/client/topic/group labels, record payloads, or upstream errors
are emitted. PLAINTEXT is the supported Compose mode; no unused SASL/TLS fields
or collector account with mutation privileges are added. A configured origin is
also the entire dial allowlist, including advertised broker addresses.

Validation: `go test ./...`; full owned-source gate:
`bash scripts/verify-plugin-metrics.sh --sources kafka,elasticsearch` from the
repository root. Only that gate may temporarily add a same-version follower to
prove a real under-replicated snapshot. It restores one replica and removes its
resources; this does not expand product support to multiple brokers/targets.

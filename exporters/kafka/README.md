# Kafka Exporter — Phase-14-03 foundation (not released)

This module is not yet packaged, registered, or enabled in GoPulse. Phase-14-03
is incomplete; see its development record for the real single-broker
partial-topology acceptance blocker. Unit fixtures do not satisfy that gate.

The client uses the existing franz-go v1.21.0 / kmsg v1.13.1 dependencies,
pinning request shapes to Kafka 2.8 (OffsetFetch v7). It issues only Metadata,
ListOffsets (log end), and OffsetFetch requests for `gopulse-observability-v1`
and `gopulse-marshaller-metrics-v1`. Auto-creation is disabled; it neither
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

Local validation: `go test ./...`. Real broker/offset recovery, partial topology,
packaging, management, and full metrics-chain acceptance remain unverified.

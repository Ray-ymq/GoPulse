# Events v1

GoPulse product version 1.7.2 extends the Events v1 path from successful Redis Exporter lifecycle transitions to operational failure and recovery episodes. Events remain observability records rather than an authoritative business audit log: plugin operations and metrics status updates complete independently of best-effort source recording, while accepted Kafka records are retried until their target store accepts them or a bounded permanent validation result allows the consumer to continue.

## Event vocabulary

All records use `event_schema_version=1`, `source=monitor`, UTC RFC3339Nano timestamps, fixed English messages, and `metadata.plugin_id=redis-exporter`.

| Event name | Severity | Required transition metadata |
| --- | --- | --- |
| `exporter_plugin_installed` | `info` | version, `install`, `not_installed -> running` |
| `exporter_plugin_started` | `info` | version, `start`, `stopped|failed -> running` |
| `exporter_plugin_stopped` | `info` | version, `stop`, `running|failed -> stopped` |
| `exporter_plugin_updated` | `info` | current/previous versions, `update`, unchanged running or stopped state |
| `exporter_plugin_failed` | `error` | version, terminal operation, safe error code, final state |
| `exporter_plugin_exited` | `error` | version, `start`, `process_exited`, final `failed` state |
| `metrics_collection_failed` | `warn` | `scrape|publish` and a safe collection error code |
| `metrics_collection_recovered` | `info` | `scrape_status=success` |
| `metrics_target_unavailable` | `warn` | published `scrape_status=target_unavailable` |
| `metrics_target_recovered` | `info` | published `scrape_status=success` |

Plugin error codes are limited to `start_failed`, `stop_failed`, `update_failed`, `rollback_failed`, `recovery_failed`, `recovery_invalid`, and `process_exited`. Collection error codes are limited to `scrape_timeout`, `network_failed`, `response_too_large`, `parse_failed`, `contract_invalid`, `content_invalid`, `http_invalid`, `scrape_failed`, `message_id_failed`, and `publish_failed`.

## Episode semantics

Collection failure and Redis target availability are independent low-cardinality states. A continuous collection failure emits one failed event and only clears after a complete metrics publish succeeds. A successfully published `target_unavailable` result emits one unavailable event; subsequent unavailable scrapes are suppressed until a published success emits one recovered event. Stopping or disabling the plugin clears episode state without creating synthetic recovery events.

Event recording never blocks plugin or scrape control flow. A full EventMonitor queue may drop the remote observability copy, but it does not roll back the source transition. Temporary Router failures retain the queue head, reuse its message ID, and retry with bounded backoff; deterministic `4xx` rejection drops only that item and permits later items to continue.

## Storage and querying

Marshaller revalidates every Events payload, writes deterministic Elasticsearch document IDs from the message ID, and maintains the strict `gopulse-events-v1-*` mapping and `gopulse-events-v1-read` alias. Existing v1 indexes are extended in place with the reserved keyword fields `metadata.error_code` and `metadata.scrape_status` before new documents are written. Temporary Elasticsearch failures hold the Kafka offset; permanent invalid records do not call the Events store and are safely committed so a following valid record can proceed.

Only authenticated administrators may query `/api/v1/observability/events`. Filters accept the fixed source, event, severity, plugin, operation, and error-code vocabulary and reject impossible event/operation/error combinations. Metrics, Logs, and Events continue to share the formal observability topic and consumer group while retaining independent validators and stores.

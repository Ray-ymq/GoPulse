# Events v1 contract

GoPulse product version 1.7.1 adds an end-to-end administrator query path for successful Redis Exporter lifecycle events. Events are observability records rather than an authoritative business audit log: the Plugin Manager state transition succeeds independently of the best-effort source queue, while accepted Kafka records are retried until Elasticsearch accepts and verifies the target contract.

## Lifecycle vocabulary

| Event name | Fixed message | Operation | State transition |
| --- | --- | --- | --- |
| `exporter_plugin_installed` | `exporter plugin installed` | `install` | `not_installed → running` |
| `exporter_plugin_started` | `exporter plugin started` | `start` | `stopped|failed → running` |
| `exporter_plugin_stopped` | `exporter plugin stopped` | `stop` | `running|failed → stopped` |
| `exporter_plugin_updated` | `exporter plugin updated` | `update` | `running → running` or `stopped → stopped` |

All four events use `source=monitor` and `severity=info`. Install does not also emit started; update does not expose its internal stop/start; an idempotent no-op and Monitor shutdown emit nothing.

## Payload and Envelope

The payload contains exactly `event_schema_version`, `event_name`, `source`, `severity`, `timestamp`, `message`, and `metadata`. Metadata contains the fixed `plugin_id=redis-exporter`, three-part `plugin_version`, `operation`, `from_state`, and `to_state`; update also requires `previous_plugin_version`. Timestamps use UTC RFC3339Nano. Free text, unknown or duplicate fields, nested metadata, control characters, URLs, credentials, paths, process details, and underlying errors are rejected.

The outer Envelope contains `schema_version=1`, a random 32-character lowercase hexadecimal `message_id`, `type=events`, `source=monitor`, the same timestamp, and the payload. Monitor sends it to the existing authenticated Router endpoint. Router preserves the HTTP bytes and produces them to `gopulse-observability-v1` with `message_id` as the Kafka key.

## Storage

Marshaller performs an independent validation pass and writes only:

```text
@timestamp, event_schema_version, event_name, source, severity, message, metadata
```

The fixed resources are:

- template: `gopulse-events-v1-template`;
- daily index: `gopulse-events-v1-YYYY.MM.DD`;
- read alias: `gopulse-events-v1-read`.

Both the root mapping and metadata object use `dynamic: strict`. The Envelope ID is used as Elasticsearch `_id` for replay idempotency but is not stored in `_source` or returned by Backend.

## Administrator query API

`GET /api/v1/observability/events` runs behind the existing session authentication and database-backed live administrator check. Unauthenticated and ordinary-user requests return `401 authentication_required` and `403 permission_denied` before any Elasticsearch call.

The first page defaults to the latest 15 minutes and accepts a maximum 24-hour range. `limit` defaults to 50 and allows 1 through 100. Exact filters are limited to `source`, `event_name`, `severity`, `plugin_id`, `operation`, and the versioned `error_code` vocabulary (no lifecycle success error code exists in this batch). Pagination uses a signed Events-domain cursor with an Elasticsearch PIT and fixed `@timestamp desc, _shard_doc desc` ordering; a continuation request may contain only `cursor`.

Responses contain only `timestamp`, `event_name`, `source`, `severity`, `message`, and strict metadata. A missing read alias returns an empty page. Unavailable or untrusted storage responses return `503 events_unavailable` without exposing an index, PIT, query, URL, or response body.

## Validation

Use `scripts/verify-events.sh --self-test` for local safety/configuration checks. `scripts/verify-events.sh` creates an isolated random Compose project, triggers real install/stop/start/update operations through the Backend admin API, queries all four events through the Backend Events API, verifies authorization, strict mapping and alias isolation, and removes only resources whose generated ownership identity matches the acceptance run.

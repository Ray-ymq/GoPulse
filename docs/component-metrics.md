# Component metrics contract (Phase-14-05)

The six built-in long-running components publish only their fixed operational
metrics. `componentmetrics.Catalog` is the shared producer/Monitor/Marshaller/
Backend allowlist; `componentmetrics/cmd/catalog` exports it without runtime
configuration. The browser contract in `frontend/src/services/componentMetrics.ts`
is generated from the same catalog and checked by the focused self-test.

## Private endpoints and lifecycle

| Component / source / producer ID | Internal port | Token variable | Target ID |
| --- | --- | --- | --- |
| `backend` | 19101 | `BACKEND_METRICS_TOKEN` | `backend-local` |
| `business-worker` | 19102 | `BUSINESS_WORKER_METRICS_TOKEN` | `business-worker-local` |
| `search-indexer` | 19103 | `SEARCH_INDEXER_METRICS_TOKEN` | `search-indexer-local` |
| `monitor` | 19104 | `MONITOR_METRICS_TOKEN` | `monitor-local` |
| `router` | 19105 | `ROUTER_METRICS_TOKEN` | `router-local` |
| `marshaller` | 19106 | `MARSHALLER_METRICS_TOKEN` | `marshaller-local` |

- Each process serves exactly `GET /internal/v1/metrics` on its separate listener.
  Host mode binds loopback; container mode binds the internal container network.
  Monitor uses only the six fixed service DNS names and ports. Compose publishes
  none of these ports, including Backend's; the public Backend router does not
  register or proxy this endpoint. The browser queries the authenticated Backend
  metric catalog/range API, never a component endpoint or VictoriaMetrics.
- Supply six **different** tokens, each at least 32 bytes, and do not reuse API,
  JWT or log-ingestion credentials. Generate each independently, for example
  with `python3 -c 'import secrets; print(secrets.token_hex(32))'`. Replace the
  deliberately development-only defaults in `.env.example`. Each producer gets
  only its own token; Monitor gets all six read-only tokens.
- Missing/wrong/repeated Authorization returns 401 before routing checks.
  After authentication, unknown path returns 404; wrong method on the known
  path returns 405; GET query (including a bare `?`) or body returns 400.
  Unknown-length/chunked bodies are rejected without waiting to consume them.
  Responses never include credentials, configuration or raw errors.
- Successful responses are Prometheus text 0.0.4, at most 262144 bytes per
  component. HTTP request/header/read/write deadlines and header size are bounded.
  Bind errors fail startup. Runtime endpoint/collection failures do not enter
  business readiness, cancel consumers, undo committed facts, or undo an ack.
- The listener uses the process root context. HTTP/consumer/outbox cleanup uses
  one shared shutdown deadline; Worker/Indexer log-shipper draining uses the
  remaining time, rather than starting another full timeout after the consumer.

## Fixed families, units, labels and bounds

Counter tuples are lazy. A count and its matching duration are published from
one immutable atomic completion sample; neither is a last-duration gauge.
Every unlabelled/fixed-gauge tuple, including dependency and last-success tuples,
is present. Last success/ack/commit starts at Unix seconds 0. Dependency values
are -1 (unobserved), 0 (last actual interaction failed) and 1 (succeeded); idle
processes retain the last observation. No business IDs, content, raw routes,
query strings, errors, queue/topic/index names or runtime dumps are dimensions.

### backend: 6 families, 577 maximum samples

| Exact family | Kind | Unit | Label keys | Maximum tuples |
| --- | --- | --- | --- | --- |
| `gopulse_backend_http_requests_total` | counter | count | `method`, `route`, `status_class` | 285 |
| `gopulse_backend_http_request_duration_seconds_total` | counter | seconds | `method`, `route`, `status_class` | 285 |
| `gopulse_backend_outbox_pending` | gauge | count | none | 1 |
| `gopulse_backend_outbox_oldest_age_seconds` | gauge | seconds | none | 1 |
| `gopulse_backend_outbox_last_publish_success_timestamp_seconds` | gauge | unix_seconds | none | 1 |
| `gopulse_backend_dependency_up` | gauge | state | `dependency` | 4 |
### business-worker: 6 families, 37 maximum samples

| Exact family | Kind | Unit | Label keys | Maximum tuples |
| --- | --- | --- | --- | --- |
| `gopulse_business_worker_messages_total` | counter | count | `event_type`, `result` | 16 |
| `gopulse_business_worker_message_processing_duration_seconds_total` | counter | seconds | `event_type`, `result` | 16 |
| `gopulse_business_worker_messages_in_flight` | gauge | count | none | 1 |
| `gopulse_business_worker_prefetch_limit` | gauge | count | none | 1 |
| `gopulse_business_worker_last_success_timestamp_seconds` | gauge | unix_seconds | none | 1 |
| `gopulse_business_worker_dependency_up` | gauge | state | `dependency` | 2 |
### search-indexer: 6 families, 24 maximum samples

| Exact family | Kind | Unit | Label keys | Maximum tuples |
| --- | --- | --- | --- | --- |
| `gopulse_search_indexer_messages_total` | counter | count | `operation`, `result` | 9 |
| `gopulse_search_indexer_message_processing_duration_seconds_total` | counter | seconds | `operation`, `result` | 9 |
| `gopulse_search_indexer_messages_in_flight` | gauge | count | none | 1 |
| `gopulse_search_indexer_retrying` | gauge | count | none | 1 |
| `gopulse_search_indexer_last_success_timestamp_seconds` | gauge | unix_seconds | none | 1 |
| `gopulse_search_indexer_dependency_up` | gauge | state | `dependency` | 3 |
### monitor: 7 families, 112 maximum samples

| Exact family | Kind | Unit | Label keys | Maximum tuples |
| --- | --- | --- | --- | --- |
| `gopulse_monitor_scrapes_total` | counter | count | `scraped_producer_kind`, `scraped_target_id`, `result` | 48 |
| `gopulse_monitor_scrape_duration_seconds_total` | counter | seconds | `scraped_producer_kind`, `scraped_target_id`, `result` | 48 |
| `gopulse_monitor_last_scrape_success_timestamp_seconds` | gauge | unix_seconds | `scraped_producer_kind`, `scraped_target_id` | 12 |
| `gopulse_monitor_event_queue_length` | gauge | count | none | 1 |
| `gopulse_monitor_plugins_running` | gauge | count | none | 1 |
| `gopulse_monitor_dependency_up` | gauge | state | `dependency` | 1 |
| `gopulse_monitor_event_queue_dropped_total` | counter | count | none | 1 |
### router: 6 families, 112 maximum samples

| Exact family | Kind | Unit | Label keys | Maximum tuples |
| --- | --- | --- | --- | --- |
| `gopulse_router_messages_total` | counter | count | `type`, `message_source`, `result` | 54 |
| `gopulse_router_produce_duration_seconds_total` | counter | seconds | `type`, `message_source`, `result` | 54 |
| `gopulse_router_buffered_records` | gauge | count | none | 1 |
| `gopulse_router_buffered_bytes` | gauge | bytes | none | 1 |
| `gopulse_router_last_kafka_ack_timestamp_seconds` | gauge | unix_seconds | none | 1 |
| `gopulse_router_dependency_up` | gauge | state | `dependency` | 1 |
### marshaller: 7 families, 260 maximum samples

| Exact family | Kind | Unit | Label keys | Maximum tuples |
| --- | --- | --- | --- | --- |
| `gopulse_marshaller_records_total` | counter | count | `type`, `message_source`, `stage`, `result` | 126 |
| `gopulse_marshaller_record_processing_duration_seconds_total` | counter | seconds | `type`, `message_source`, `stage`, `result` | 126 |
| `gopulse_marshaller_records_in_flight` | gauge | count | none | 1 |
| `gopulse_marshaller_retrying` | gauge | count | none | 1 |
| `gopulse_marshaller_last_storage_success_timestamp_seconds` | gauge | unix_seconds | `storage` | 2 |
| `gopulse_marshaller_last_commit_success_timestamp_seconds` | gauge | unix_seconds | none | 1 |
| `gopulse_marshaller_dependency_up` | gauge | state | `dependency` | 3 |

### Label value sets and update points

- Backend: 47 registered method/template pairs are frozen in
  `componentmetrics.BackendRoutes()`, including Phase-15-01 user lookup, role update,
  and audit query templates. There are ten fixed `_unmatched` method
  buckets (`GET POST PUT PATCH DELETE HEAD OPTIONS CONNECT TRACE unknown`),
  and five status classes (`1xx` through `5xx`). The HTTP middleware records
  after Gin completion/recovery, using `FullPath()`, never the request URL.
  Its maximum is `2 × (47 + 10) × 5 + 7 = 577` samples. Dependencies are exactly
  `mysql redis rabbitmq elasticsearch`.
- Backend outbox: one aggregate SQL query every 5 seconds, with a 1-second
  timeout, reads only count and oldest creation time for pending/leased rows.
  No successful snapshot yet or a failed sample makes the endpoint return 503;
  it does not report a fictitious empty queue or serve a stale backlog.
  Publish-success time is updated only after the RabbitMQ publisher succeeds.
  Other dependency updates use existing ping/cache/HTTP/AMQP interactions.
- Worker: `event_type=comment.created|post.liked|user.followed|unknown` and
  `result=success|retry|failure|ack`. Handler completion records the whole
  operation duration; `ack` separately records a successful `Ack(false)` call
  and its own duration. Successful self-event handling counts as success.
  In-flight brackets Handle, prefetch is configured credit, and dependencies
  are `mysql|rabbitmq`. Notification insert observes actual MySQL results.
- Indexer: `operation=create|update|delete` from fixed routing keys and
  `result=success|retry|failure`. Invalid unknown operations do not create a new
  tuple. Handler completion includes the original ack; retrying brackets retry
  publication. MySQL reads/transaction operations, Elasticsearch requests and
  RabbitMQ sessions/ack/retry writes supply dependency results.
- Monitor: twelve fixed `(scraped_producer_kind,scraped_target_id)` pairs:
  six `exporter_plugin/<source>-exporter-local` and six
  `component/<component>-local`. Results are `scrape_success|scrape_failure|
  publish_success|publish_failure`, each with its corresponding operation's
  duration. Last scrape success records the completed successful collection;
  publish failures do not invent component business zeros. Event queue length
  and drops use actual enqueue/dequeue/full events; running plugins come from
  the six-plugin manager state. Router dependency uses actual HTTP publication.
- Router: results `accepted|rejected|produced`. Accepted is after strict envelope
  and idempotency validation; rejected untrusted input uses `(unknown,unknown)`;
  Kafka callbacks record produced or rejected for the fixed accepted identity.
  Durations cover the corresponding validation/production stage. Buffer gauges
  come from franz-go's public in-memory `BufferedProduceRecords` and
  `BufferedProduceBytes` (including keys/headers, not just JSON body length).
  Last Kafka ack and dependency update at the real producer callback.
- Router/Marshaller message pairs: twelve `metrics/<fixed source>` pairs,
  `logs/backend|business-worker|search-indexer|search-reindex`, `events/monitor`,
  and the explicit `unknown/unknown` bucket: 18 pairs, never a Cartesian product
  admitting unsupported type/source combinations.
- Marshaller stage/result pairs are exactly `consume/consumed`,
  `validate/validated`, `validate/rejected`, `store/stored`, `store/retried`,
  `commit/committed`, `commit/failure`. Decode completion supplies consumption
  and validation timings, each write attempt supplies storage timing, and each
  commit attempt supplies commit timing. In-flight brackets Handle; retrying
  brackets retry backoff. Successful writes update the appropriate fixed
  `storage=victoriametrics|elasticsearch` timestamp. The commit timestamp only
  advances after a successful commit with valid partition ownership. Poll,
  commit and storage operations update fixed dependencies.

## Transport, validation and queries

Component snapshots use schema 2, `type=metrics`, `producer_kind=component`,
`producer_id=source=<component>` and `target_id=<component>-local`, with the
three-part product version. A successful complete snapshot has
`scrape_status=success`; an unavailable endpoint emits **no** business snapshot.
Component targets never enter the plugin Registry or install/start/stop/update
APIs. Each collector has an independent loop and bounded HTTP/publish timeouts.

Monitor and Marshaller both reject extra family/label/key/value, duplicate
series, non-finite values, excess samples and unpaired operation counters.
Fixed gauge tuples are mandatory, but genuinely absent lazy counter tuples
are valid. Storage adds trusted source/target/producer labels; these keys are
never accepted from scraped samples. Monitor's observed target labels remain
`scraped_*`; Router/Marshaller retain `message_source`. Redis alone retains its
existing storage-label identity without the new producer labels.

Backend catalog entries and range expressions are server-controlled. Component
query series limits are the exact family tuple count, and point budgets are
that count times 97 (the largest fixed query-range grid); plugin limits remain
unchanged. Values and all returned component labels are checked again. The
Frontend accepts only the generated matching definitions and label tuples.

## Focused acceptance and Phase-14-06 handoff

Run `bash scripts/verify-component-metrics.sh --self-test` without Docker. After
building current product images, run `bash scripts/verify-component-metrics.sh`
for the owned Compose scenario. It creates two users and multiple posts, drives
notifications and create/update/delete search projection, queries every family,
checks endpoint auth/port isolation and bounded labels, queries through the real
Frontend, injects one Redis dependency outage and one Backend endpoint 503, then
checks Worker/Indexer SIGTERM and replacement consumption. Six-plugin and
Logs/Events/admin-authorization regressions are included. It removes only its
owned containers/networks/volumes and preserves pre-existing resources.

The acceptance-only proxy changes only Monitor's resolution of the fixed
Backend metrics service name inside the owned project, forwarding normal
requests to the real protected listener. Fault mode returns a fixed 503. It is
not a product option or a token/authentication bypass.

Phase-14-06 uses the six fixed source/target IDs above. Representative family
pairs are requests/outbox-last-publish (Backend), messages/last-success (Worker
and Indexer), scrapes/last-scrape-success (Monitor), messages/last-Kafka-ack
(Router), and records/last-storage-success (Marshaller). Also assert Monitor's
self-scraped `component/monitor-local` tuple and the Router/Marshaller
`type=metrics,message_source=backend` production/storage tuples.

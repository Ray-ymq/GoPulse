# Phase 20-02 trace and freshness contract

This batch verifies one bounded business path: HTTP post creation, the
transactional MySQL fact and Outbox row, RabbitMQ publication, Search Indexer
consumption, Elasticsearch indexing, and a real business search hit. It does
not claim full-site tracing or a production search freshness SLO.

The Go SDK is OpenTelemetry `v1.37.0`. The acceptance-only Collector is
`otel/opentelemetry-collector-contrib:0.138.0` pinned to the Linux/amd64 image
digest in `deploy/phase20-trace.yaml`. It listens only on the private
`observability` network and writes raw spans to the acceptance volume through
the file exporter. Normal operation uses a 10% ParentBased TraceIDRatio
sample, a 2048 item queue, 256 item batches, a 1 second batch timeout, a 2
second export timeout, and a 5 second shutdown timeout. The controlled
acceptance overlay uses a 100% sample for its exclusive flow.

Propagation is limited to W3C `traceparent` and optional `tracestate`. HTTP
extraction, the JSON Envelope, and RabbitMQ headers all validate these fields
strictly. `x-gopulse-attempt-id` is a separate bounded 32 hexadecimal value
for each publish or consume attempt. The Envelope stores the optional context
inside the existing JSON payload, so no migration or second event write is
needed. A legacy message without these fields remains valid. A malformed
optional field is removed, counted, and processed as a new root while the
business Envelope still has to pass its ordinary validation.

The frozen span graph is:

```text
http.server
└── post.commit
    └── outbox.publish
        └── worker.consume
            └── search.process
                └── search.index
```

`post.commit` is created inside the same request context as the MySQL fact and
Outbox insert. Its persisted W3C parent is used after a process restart.
`outbox.publish` creates a new attempt span for each lease/publish attempt;
RabbitMQ carries that span as the next parent. `worker.consume` and the search
spans keep the event ID, post ID, and content revision as bounded technical
attributes. Delete projection uses `search.index.delete` with the same parent
relationship. Export failure cannot block an HTTP response, an Outbox state
transition, or a message acknowledgement because the SDK processor and every
export operation have bounded capacity and deadlines.

The chain records these millisecond observation points:

`t_request_start`, `t_accept`, `t_commit`, `t_publish_start`, `t_publish_ack`,
`t_consume_start`, `t_consume_end`, `t_index_start`, `t_index_ack`, and
`t_visible`. The maximum cross-process clock error is 250 ms. A search probe
polls at most once per second, records a preceding miss, and sets `t_visible`
to the observation that first returns the expected post ID and content
revision. An Elasticsearch write acknowledgement alone never proves
visibility. A raw evidence set is capped at 1 MiB per chain and is bound to a
candidate manifest digest.

Freshness metrics are fixed low-cardinality families. Their only labels are
`stage` and `result`; stage values are `commit`, `publish`, `consume`, `index`,
and `visible`, and result values are `success` and `failure`. Trace IDs, span
IDs, event IDs, Outbox IDs, attempt IDs, request IDs, post IDs, revisions,
message content, credentials, and arbitrary baggage are rejected as metric
labels. `trace_context_invalid_total` is an unlabelled counter on Backend,
Business Worker, and Search Indexer.

The evidence verifier in `scripts/ci/phase20_chain.py` recomputes all seven
cases. It checks identity stability, parent links, span intervals, log
coverage, metric labels, clock bounds, retry attempt separation, strict old
and malformed-message handling, authorization responses, and the final
business search observation. It uses `scripts/verify-phase20-chain.sh` for
candidate binding and `scripts/verify-phase20-evidence.py --chain` for the
same work directory.

The covered cases are C01 normal creation, C02 a 30 second index dependency
delay, C03 a 30 second private Collector outage, C04 duplicate delivery and a
controlled retry, C05 an old message and malformed optional context, C06 a
post-commit restart with the Outbox context persisted, and C07 authorization
and field-boundary checks. C02 and C06 distinguish retry or restart waiting
from storage execution time. C03 records queue peak, export timeouts, and
shutdown duration. The result supports this chain's propagation and
freshness behavior only.

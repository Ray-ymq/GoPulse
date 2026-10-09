# Phase 19 capacity methodology

> **Executor status**: the Phase 18–20 formal-matrix executors
> (`scripts/verify-phase*-*.sh`, `scripts/ci/phase1x_*.py`) were retired and deleted. This
> document keeps the method and the recorded conclusions; sources and raw receipts remain in
> Git history. Current entries are listed in [capability status](../../status/capability-status.md).

Phase 19 capacity evidence is a bounded statement about one frozen GoPulse
candidate, one deterministic Phase 18 data recipe, and the host described by
[`loadtest/capacity-profile.json`](../../../loadtest/capacity-profile.json). The
profile is the machine-readable source for the recipe identity, 1,024 virtual
users, the fixed workload mix, route/status contract, host minimums, windows,
gates, safe-stop conditions, sampling interval, and statistics. Its exact file
bytes are bound with a `sha256:` digest in every load report and evidence
directory.

## Execution shape

Formal execution owns three independent repetitions. Every repetition contains
the same four open-loop stages: 50, 100, 150, and 200 RPS. Each stage has an
explicit warmup, measurement, and bounded recovery window. The load generator
records accepted scheduled slots, dropped slots, scheduling lag, achieved RPS,
the P50/P95/P99/max latency summary, and outcome counts for the measurement
window. Recovery is recorded separately and is not mixed into the latency
distribution.

The request outcome categories are deliberately disjoint:

- successful expected statuses;
- explicit 429 and 503 rejection, retained both as a total and by status;
- request timeout;
- transport failure; and
- any other unexpected HTTP or workload error.

An explicit rejection is not a timeout or a generic error. A dropped scheduler
slot is also not a request failure: it is retained as scheduler evidence and
fails the synchronous gate when the profile requires no drops.

The resource sampler is a separate process from the load generator. Each raw
record retains host CPU/memory/swap, load-process CPU/RSS/scheduling lag, SUT
container CPU/RSS/restarts/OOM, private component saturation metrics, and
RabbitMQ/MySQL/Kafka signals. Records are fsync'd as JSONL and a summary is
derived only after the raw record count and interval contract pass.

## Statistics and capacity status

Each stage retains one measurement value from each repetition. The verifier
computes median, minimum, maximum, and population coefficient of variation
(`population standard deviation / mean * 100`) independently for achieved RPS,
each latency summary value, error rates, and scheduling lag. It never merges
the three latency summaries into a synthetic request sample and never invents a
percentile from those three values.

`complete` means that all three repetitions, all four stages, bindings, raw
resources, recovery receipts, and owned cleanup are present and verifiable.
Only a complete execution may be classified as `target_met` or
`boundary_found`. An incomplete execution is caused only by the profile's
safe-stop classes—OOM, ownership loss, unsafe cleanup, or a profile hard
error—and retains every unexecuted stage explicitly. It is never converted to
a capacity conclusion.

`--calibration` checks arrival accounting, signal shape, and safe-stop wiring
with bounded synthetic input. It writes no formal summary and consumes no
formal repetition. Use `scripts/verify-phase19-capacity.sh --self-test` for the
runner, sampler, evidence, and Go loadtest self-tests; use
`scripts/verify-phase19-evidence.py --directory <evidence-dir>` for the
read-only evidence check.

The Phase-19-03 implementation freezes the repaired acceptance path and its
raw-evidence contracts. Its bounded preflight is infrastructure validation only:
it must report `formal=false` and `capacity_status=null`, and its output cannot
be reused as Phase-19-04 capacity evidence. The Phase-19-03 implementation does
not publish a throughput, tail-latency, `target_met`, or `boundary_found` result;
formal certification remains assigned to Phase-19-04 against the frozen `2.1.3`
candidate.

"""Recompute Phase 20 business-chain evidence from raw bounded observations."""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import tempfile
from pathlib import Path
from typing import Any

SCHEMA = "gopulse.phase20.chain.v1"
TARGET_VERSION = "2.2.2"
CASES = tuple(f"C0{number}" for number in range(1, 8))
REQUIRED_TIMES = (
    "t_request_start", "t_accept", "t_commit", "t_publish_start",
    "t_publish_ack", "t_consume_start", "t_consume_end", "t_index_start",
    "t_index_ack", "t_visible",
)
SPAN_NAMES = {"http.server", "post.commit", "outbox.publish", "worker.consume", "search.process", "search.index"}
TRACE_ID = re.compile(r"^[0-9a-f]{32}$")
SPAN_ID = re.compile(r"^[0-9a-f]{16}$")
ATTEMPT_ID = re.compile(r"^[0-9a-f]{32}$")
REQUEST_ID = re.compile(r"^[0-9a-f]{32}$")
EVENT_ID = re.compile(r"^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$")
DIGEST = re.compile(r"^sha256:[0-9a-f]{64}$")
REVISION = re.compile(r"^[0-9a-f]{40}$")
FORBIDDEN_LABEL = re.compile(r"(?:trace|span|event|attempt|request|post|revision|content|outbox)", re.I)
ALLOWED_LOG_FIELDS = {
    "trace_id", "span_id", "event_id", "post_id", "content_revision",
    "outbox_id", "attempt_id", "stage", "result", "message",
}


class ChainError(ValueError):
    """Raised when raw chain evidence cannot prove the frozen contract."""


def _require(condition: bool, message: str) -> None:
    if not condition:
        raise ChainError(message)


def _nonzero(value: str) -> bool:
    return value.strip("0") != ""


def file_digest(path: Path) -> str:
    return "sha256:" + hashlib.sha256(path.read_bytes()).hexdigest()


def _load_json(path: Path) -> dict[str, Any]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise ChainError(f"cannot read JSON {path}: {exc}") from exc
    _require(isinstance(value, dict), f"JSON document {path} must be an object")
    return value


def _candidate_from_manifest(path: Path) -> dict[str, str]:
    manifest = _load_json(path)
    candidate = manifest.get("candidate", manifest)
    _require(isinstance(candidate, dict), "candidate manifest binding must be an object")
    version, revision = candidate.get("version"), candidate.get("revision")
    _require(version == TARGET_VERSION, "candidate manifest version is not 2.2.2")
    _require(isinstance(revision, str) and REVISION.fullmatch(revision), "candidate revision must be a 40-character git revision")
    return {"version": version, "revision": revision, "manifest_sha256": file_digest(path)}


def _check_identifier(value: Any, pattern: re.Pattern[str], name: str, *, nonzero: bool = True) -> str:
    _require(isinstance(value, str) and pattern.fullmatch(value) is not None, f"{name} has invalid format")
    if nonzero:
        _require(_nonzero(value), f"{name} must not be all zero")
    return value


def _check_artifacts(raw: dict[str, Any]) -> int:
    artifacts = raw.get("artifacts")
    _require(isinstance(artifacts, list) and artifacts, "raw artifact inventory is required")
    total = 0
    for artifact in artifacts:
        _require(isinstance(artifact, dict), "artifact entry must be an object")
        relative = artifact.get("path")
        _require(isinstance(relative, str) and relative and not Path(relative).is_absolute() and ".." not in Path(relative).parts, "artifact path must stay inside the evidence directory")
        size, records = artifact.get("bytes"), artifact.get("records")
        _require(type(size) is int and 0 <= size <= 1_048_576, "artifact size is outside the 1 MiB bound")
        _require(type(records) is int and records > 0, "artifact record count must be positive")
        total += size
    _require(total <= 1_048_576, "combined raw artifact size exceeds the 1 MiB bound")
    return total


def _check_times(case: dict[str, Any]) -> dict[str, int]:
    timestamps = case.get("timestamps")
    _require(isinstance(timestamps, dict), "timestamps are required")
    values: dict[str, int] = {}
    for name in REQUIRED_TIMES:
        value = timestamps.get(name)
        _require(type(value) is int and value >= 0, f"{name} must be a non-negative millisecond timestamp")
        values[name] = value
    clock_error, probe_overhead = timestamps.get("clock_error_ms"), timestamps.get("probe_overhead_ms")
    _require(type(clock_error) is int and 0 <= clock_error <= 250, "clock error exceeds 250 ms")
    _require(type(probe_overhead) is int and 0 <= probe_overhead <= 1_000, "search probe overhead is unbounded")
    for previous, current in zip(REQUIRED_TIMES, REQUIRED_TIMES[1:]):
        _require(values[current] + clock_error >= values[previous], f"timestamp order is outside the clock error: {previous} -> {current}")
    return values


def _check_business(case: dict[str, Any]) -> tuple[dict[str, Any], str, str, str]:
    business = case.get("business")
    _require(isinstance(business, dict), "business identity record is required")
    request_id = _check_identifier(business.get("request_id"), REQUEST_ID, "request_id")
    event_id = _check_identifier(business.get("event_id"), EVENT_ID, "event_id")
    trace_id = _check_identifier(business.get("trace_id"), TRACE_ID, "trace_id")
    post_id, revision, outbox_id = business.get("post_id"), business.get("content_revision"), business.get("outbox_id")
    _require(type(post_id) is int and post_id > 0, "post_id must be positive")
    _require(type(revision) is int and revision > 0, "content_revision must be positive")
    _require(type(outbox_id) is int and outbox_id > 0, "outbox_id must be positive")
    return business, request_id, event_id, trace_id


def _check_spans(case: dict[str, Any], event_id: str, trace_id: str) -> tuple[dict[str, dict[str, Any]], set[str]]:
    raw, indexed, attempts = case.get("raw"), {}, set()
    _require(isinstance(raw, dict), "raw observations are required")
    spans = raw.get("spans")
    _require(isinstance(spans, list) and spans, "raw spans are required")
    for span in spans:
        _require(isinstance(span, dict), "span entry must be an object")
        _require(_check_identifier(span.get("trace_id"), TRACE_ID, "span trace_id") == trace_id, "span trace ID does not match business trace")
        span_id, name = _check_identifier(span.get("span_id"), SPAN_ID, "span_id"), span.get("name")
        _require(span_id not in indexed, "duplicate span ID")
        _require(name in SPAN_NAMES, f"unsupported span name: {name}")
        start, end = span.get("start_ns"), span.get("end_ns")
        _require(type(start) is int and type(end) is int and 0 <= start < end, f"invalid span interval for {name}")
        parent = span.get("parent_span_id")
        if parent is not None:
            _check_identifier(parent, SPAN_ID, "parent_span_id")
        attributes = span.get("attributes", {})
        _require(isinstance(attributes, dict), "span attributes must be an object")
        if name != "http.server":
            _require(attributes.get("gopulse.event_id") == event_id, f"span {name} is not bound to event_id")
            _require(attributes.get("gopulse.post_id") == case["business"]["post_id"], f"span {name} is not bound to post_id")
            _require(attributes.get("gopulse.content_revision") == case["business"]["content_revision"], f"span {name} is not bound to content_revision")
        if name in {"outbox.publish", "worker.consume"}:
            attempts.add(_check_identifier(attributes.get("gopulse.attempt_id"), ATTEMPT_ID, "attempt_id"))
        indexed[span_id] = span
    roots = [span for span in indexed.values() if span.get("parent_span_id") is None]
    _require(len(roots) == 1 and roots[0].get("name") == "http.server", "span graph must have one http.server root")
    for span in indexed.values():
        parent = span.get("parent_span_id")
        if parent is not None:
            _require(parent in indexed, f"span parent {parent} is missing")
            _require(indexed[parent]["trace_id"] == span["trace_id"], "cross-trace span parent")
    names = {span["name"] for span in indexed.values()}
    _require({"http.server", "post.commit", "outbox.publish", "worker.consume", "search.process", "search.index"} <= names, "required business spans are missing")
    return indexed, attempts


def _check_logs(case: dict[str, Any], event_id: str, trace_id: str, spans: dict[str, dict[str, Any]]) -> None:
    logs = case["raw"].get("logs")
    _require(isinstance(logs, list) and logs, "raw logs are required")
    stages: set[str] = set()
    for entry in logs:
        _require(isinstance(entry, dict), "log entry must be an object")
        _require(set(entry) <= ALLOWED_LOG_FIELDS, "log contains an unregistered field")
        _require(_check_identifier(entry.get("trace_id"), TRACE_ID, "log trace_id") == trace_id, "log trace ID does not match business trace")
        _require(_check_identifier(entry.get("span_id"), SPAN_ID, "log span_id") in spans, "log span ID is not in the span graph")
        _require(_check_identifier(entry.get("event_id"), EVENT_ID, "log event_id") == event_id, "log event ID does not match business event")
        _require(type(entry.get("post_id")) is int and entry["post_id"] == case["business"]["post_id"], "log post ID does not match business identity")
        _require(type(entry.get("content_revision")) is int and entry["content_revision"] == case["business"]["content_revision"], "log content revision does not match business identity")
        _require(type(entry.get("outbox_id")) is int and entry["outbox_id"] == case["business"]["outbox_id"], "log Outbox ID does not match business identity")
        _check_identifier(entry.get("attempt_id"), ATTEMPT_ID, "log attempt_id")
        _require(entry.get("stage") in {"commit", "publish", "consume", "index"}, "log stage is outside the chain")
        stages.add(entry["stage"])
    _require({"commit", "publish", "consume", "index"} <= stages, "logs do not cover all required stages")


def _check_metrics(case: dict[str, Any]) -> None:
    metrics = case["raw"].get("metrics")
    _require(isinstance(metrics, list) and metrics, "raw metrics are required")
    for sample in metrics:
        _require(isinstance(sample, dict), "metric sample must be an object")
        labels = sample.get("labels")
        _require(isinstance(labels, dict), "metric labels must be an object")
        for key in labels:
            _require(FORBIDDEN_LABEL.search(key) is None, f"business identity entered metric label {key}")
        _require(set(labels) <= {"stage", "result"}, "metric label key is outside the fixed freshness contract")
        _require(labels.get("stage") in {"commit", "publish", "consume", "index", "visible"}, "metric stage is not fixed")
        _require(labels.get("result") in {"success", "failure"}, "metric result is not fixed")
        _require(type(sample.get("value")) in {int, float}, "metric value is not numeric")


def _check_search(case: dict[str, Any], timestamps: dict[str, int]) -> None:
    probes = case["raw"].get("search_probes")
    _require(isinstance(probes, list) and len(probes) >= 2, "search evidence needs a miss and a hit")
    observed = []
    for probe in probes:
        _require(isinstance(probe, dict), "search probe must be an object")
        _require(probe.get("source") == "search", "search visibility must come from a business search probe")
        at = probe.get("observed_at_ms")
        _require(type(at) is int and at >= 0, "search probe timestamp is invalid")
        observed.append(at)
        _require(type(probe.get("hit")) is bool, "search probe hit must be explicit")
        if probe["hit"]:
            _require(probe.get("post_id") == case["business"]["post_id"], "search hit returned the wrong post")
            _require(probe.get("content_revision") == case["business"]["content_revision"], "search hit returned the wrong revision")
    _require(observed == sorted(observed), "search probe timestamps are not ordered")
    _require(any(not probe["hit"] for probe in probes[:-1]), "search evidence lacks a pre-hit miss")
    final = probes[-1]
    _require(final["hit"] and final["observed_at_ms"] == timestamps["t_visible"], "t_visible is not the final search hit observation")


def _check_wire(case: dict[str, Any]) -> None:
    wire = case["raw"].get("wire_context")
    _require(isinstance(wire, dict) and set(wire) <= {"traceparent", "tracestate"}, "arbitrary baggage entered propagation evidence")
    parent = wire.get("traceparent")
    _require(isinstance(parent, str) and len(parent) == 55 and parent[2] == "-" and parent[35] == "-" and parent[52] == "-", "traceparent evidence is malformed")
    _check_identifier(parent[3:35], TRACE_ID, "wire trace_id")
    _check_identifier(parent[36:52], SPAN_ID, "wire span_id")


def _check_case(case: Any) -> dict[str, Any]:
    _require(isinstance(case, dict), "case must be an object")
    case_id = case.get("case_id")
    _require(case_id in CASES, f"unknown case ID {case_id}")
    _require(case.get("execution_status") == "complete", f"{case_id} is not complete")
    raw = case.get("raw")
    _require(isinstance(raw, dict), "raw observations are required")
    _check_artifacts(raw)
    timestamps = _check_times(case)
    business, request_id, event_id, trace_id = _check_business(case)
    _check_wire(case)
    spans, attempts = _check_spans(case, event_id, trace_id)
    _check_logs(case, event_id, trace_id, spans)
    _check_metrics(case)
    _check_search(case, timestamps)
    observations = case.get("observations")
    _require(isinstance(observations, dict), "case observations are required")
    if case_id == "C01":
        _require(observations.get("request_status") == 201, "C01 does not prove accepted post creation")
    elif case_id == "C02":
        _require(observations.get("fault_stage") == "index" and observations.get("fault_duration_ms", 0) >= 30_000, "C02 index delay is not recorded")
        _require(observations.get("retries", 0) >= 1 or observations.get("wait_duration_ms", 0) >= 30_000, "C02 has no failed/retried or waited interval")
    elif case_id == "C03":
        _require(observations.get("fault_stage") == "collector" and observations.get("fault_duration_ms", 0) >= 30_000, "C03 Collector outage is not recorded")
        _require(observations.get("queue_peak", 0) <= 2048, "C03 trace queue exceeded the contract")
        _require(type(observations.get("export_timeouts")) is int and observations["export_timeouts"] >= 0, "C03 export timeout count is missing")
        _require(type(observations.get("shutdown_ms")) is int and observations["shutdown_ms"] <= 5_000, "C03 shutdown exceeded the contract")
    elif case_id == "C04":
        retry_attempts = observations.get("attempts")
        _require(isinstance(retry_attempts, list) and len(retry_attempts) >= 2, "C04 does not contain a retry attempt set")
        _require(len({_check_identifier(item.get("attempt_id"), ATTEMPT_ID, "attempt_id") for item in retry_attempts}) == len(retry_attempts), "C04 attempt IDs are not independent")
        _require(all(item.get("event_id") == event_id for item in retry_attempts), "C04 changed event_id across retries")
        _require(len(attempts) >= 2, "C04 raw spans do not contain independent attempts")
    elif case_id == "C05":
        _require(observations.get("legacy_message_count", 0) >= 1, "C05 lacks an old message observation")
        _require(observations.get("malformed_optional_count", 0) >= 1, "C05 lacks malformed optional context")
        _require(observations.get("invalid_context_count", 0) >= 1, "C05 invalid context was not counted")
        _require(observations.get("retained_event_id") == event_id, "C05 malformed context did not retain the business event")
        _require(observations.get("invalid_business_rejected") is True, "C05 does not prove strict business validation")
    elif case_id == "C06":
        _require(observations.get("fault_stage") == "publish" and observations.get("committed_before_restart") is True, "C06 restart boundary is missing")
        _require(observations.get("persisted_trace_context") is True, "C06 did not prove persisted trace context")
    elif case_id == "C07":
        statuses = observations.get("unauthorized_statuses")
        _require(isinstance(statuses, list) and 401 in statuses and 403 in statuses, "C07 authorization boundary is incomplete")
        _require(observations.get("authorized_pagination") is True, "C07 authorized pagination is missing")
        _require(observations.get("contains_body_or_credentials") is False, "C07 found body, credential, or baggage data")
    return {"case_id": case_id, "trace_id": trace_id, "request_id": request_id, "event_id": event_id, "attempts": len(attempts)}


def verify_document(document: dict[str, Any], candidate: dict[str, str] | None = None) -> dict[str, Any]:
    _require(document.get("schema") == SCHEMA, "invalid Phase 20 chain schema")
    _require(document.get("execution_status") == "complete", "chain execution is incomplete")
    document_candidate = document.get("candidate")
    _require(isinstance(document_candidate, dict), "chain candidate binding is missing")
    _require(document_candidate.get("version") == TARGET_VERSION, "chain evidence version is not 2.2.2")
    revision, digest = document_candidate.get("revision"), document_candidate.get("manifest_sha256")
    _require(isinstance(revision, str) and REVISION.fullmatch(revision), "chain evidence revision is invalid")
    _require(isinstance(digest, str) and DIGEST.fullmatch(digest), "chain manifest digest is invalid")
    contract = document.get("contract")
    _require(isinstance(contract, dict), "chain contract values are missing")
    _require(contract.get("controlled_sample_ratio") == 1.0 and contract.get("normal_sample_ratio") == 0.10, "sampling contract drift")
    _require(contract.get("max_clock_error_ms") == 250 and contract.get("search_poll_interval_ms") == 1_000, "time contract drift")
    cases = document.get("cases")
    _require(isinstance(cases, list) and len(cases) == len(CASES), "chain must contain all seven cases")
    _require([case.get("case_id") if isinstance(case, dict) else None for case in cases] == list(CASES), "chain case order or membership drift")
    if candidate is not None:
        _require(document_candidate == candidate, "chain evidence is bound to a different candidate manifest")
    results = [_check_case(case) for case in cases]
    return {"schema": SCHEMA, "execution_status": "complete", "candidate": document_candidate, "cases": results, "case_count": len(results)}


def verify_directory(work: Path, manifest: Path | None = None) -> dict[str, Any]:
    _require(work.is_dir(), f"chain evidence directory does not exist: {work}")
    evidence = next((work / name for name in ("chain-evidence.json", "chain.json", "evidence.json") if (work / name).is_file()), None)
    _require(evidence is not None, "chain evidence JSON is missing")
    if manifest is None:
        for candidate_path in (work / "candidate.json", work / "candidate-manifest.json"):
            if candidate_path.is_file():
                manifest = candidate_path
                break
    result = verify_document(_load_json(evidence), _candidate_from_manifest(manifest) if manifest is not None else None)
    result["evidence_path"] = str(evidence)
    return result


def _span(trace_id: str, span_id: str, name: str, start: int, end: int, parent: str | None, business: dict[str, Any], attempt: str | None = None) -> dict[str, Any]:
    attributes: dict[str, Any] = {"gopulse.event_id": business["event_id"], "gopulse.post_id": business["post_id"], "gopulse.content_revision": business["content_revision"], "gopulse.outbox_id": business["outbox_id"]}
    if attempt is not None:
        attributes["gopulse.attempt_id"] = attempt
    return {"trace_id": trace_id, "span_id": span_id, "parent_span_id": parent, "name": name, "start_ns": start * 1_000_000, "end_ns": end * 1_000_000, "attributes": attributes}


def _fixture_case(case_id: str) -> dict[str, Any]:
    trace_id = ("0123456789abcdef" if case_id != "C04" else "fedcba9876543210") + ("0" * 16)
    event_id = "123e4567-e89b-12d3-a456-426614174000"
    business = {"request_id": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "post_id": 9, "content_revision": 1, "event_id": event_id, "outbox_id": 41, "trace_id": trace_id}
    spans = [_span(trace_id, "0000000000000001", "http.server", 100, 101, None, business), _span(trace_id, "0000000000000002", "post.commit", 102, 104, "0000000000000001", business), _span(trace_id, "0000000000000003", "outbox.publish", 110, 111, "0000000000000002", business, "11111111111111111111111111111111"), _span(trace_id, "0000000000000004", "worker.consume", 120, 122, "0000000000000003", business, "22222222222222222222222222222222"), _span(trace_id, "0000000000000005", "search.process", 123, 125, "0000000000000004", business), _span(trace_id, "0000000000000006", "search.index", 126, 128, "0000000000000005", business)]
    if case_id == "C04":
        spans.extend([_span(trace_id, "0000000000000007", "outbox.publish", 130, 131, "0000000000000002", business, "33333333333333333333333333333333"), _span(trace_id, "0000000000000008", "worker.consume", 140, 142, "0000000000000007", business, "44444444444444444444444444444444")])
    logs = [{"trace_id": trace_id, "span_id": span_id, "event_id": event_id, "post_id": 9, "content_revision": 1, "outbox_id": 41, "attempt_id": attempt, "stage": stage, "result": "success", "message": stage + " complete"} for stage, span_id, attempt in (("commit", "0000000000000002", "11111111111111111111111111111111"), ("publish", "0000000000000003", "11111111111111111111111111111111"), ("consume", "0000000000000004", "22222222222222222222222222222222"), ("index", "0000000000000006", "22222222222222222222222222222222"))]
    raw = {"artifacts": [{"path": f"raw/{case_id.lower()}-spans.jsonl", "records": len(spans), "bytes": 2048}, {"path": f"raw/{case_id.lower()}-logs.jsonl", "records": len(logs), "bytes": 1024}], "spans": spans, "logs": logs, "metrics": [{"name": "gopulse_backend_freshness_events_total", "labels": {"stage": stage, "result": "success"}, "value": 1} for stage in ("commit", "publish", "consume", "index", "visible")], "wire_context": {"traceparent": "00-" + trace_id + "-0000000000000002-01"}, "search_probes": [{"source": "search", "observed_at_ms": 129, "hit": False}, {"source": "search", "observed_at_ms": 130, "hit": True, "post_id": 9, "content_revision": 1}]}
    observations: dict[str, Any] = {"request_status": 201}
    if case_id == "C02": observations.update(fault_stage="index", fault_duration_ms=30_000, retries=1)
    elif case_id == "C03": observations.update(fault_stage="collector", fault_duration_ms=30_000, queue_peak=512, export_timeouts=1, shutdown_ms=120)
    elif case_id == "C04": observations["attempts"] = [{"event_id": event_id, "attempt_id": "33333333333333333333333333333333"}, {"event_id": event_id, "attempt_id": "44444444444444444444444444444444"}]
    elif case_id == "C05": observations.update(legacy_message_count=1, malformed_optional_count=1, invalid_context_count=1, retained_event_id=event_id, invalid_business_rejected=True)
    elif case_id == "C06": observations.update(fault_stage="publish", committed_before_restart=True, persisted_trace_context=True)
    elif case_id == "C07": observations.update(unauthorized_statuses=[401, 403], authorized_pagination=True, contains_body_or_credentials=False)
    timestamp_values = dict(zip(REQUIRED_TIMES, (100, 101, 104, 110, 111, 120, 122, 126, 128, 130)))
    timestamp_values.update(clock_error_ms=0, probe_overhead_ms=5)
    return {"case_id": case_id, "execution_status": "complete", "business": business, "timestamps": timestamp_values, "raw": raw, "observations": observations}


def fixture_document(candidate: dict[str, str]) -> dict[str, Any]:
    return {"schema": SCHEMA, "execution_status": "complete", "candidate": candidate, "contract": {"controlled_sample_ratio": 1.0, "normal_sample_ratio": 0.10, "max_clock_error_ms": 250, "search_poll_interval_ms": 1_000}, "cases": [_fixture_case(case_id) for case_id in CASES]}


def self_test() -> dict[str, Any]:
    with tempfile.TemporaryDirectory(prefix="gopulse-phase20-chain-") as temporary:
        root = Path(temporary)
        manifest = root / "candidate.json"
        manifest.write_text(json.dumps({"version": TARGET_VERSION, "revision": "a" * 40}) + "\n", encoding="utf-8")
        candidate = _candidate_from_manifest(manifest)
        document = fixture_document(candidate)
        verify_document(document, candidate)
        broken = json.loads(json.dumps(document))
        broken["cases"][0]["raw"]["metrics"][0]["labels"]["trace_id"] = "0" * 32
        try:
            verify_document(broken, candidate)
        except ChainError:
            return {"schema": SCHEMA, "self_test": "passed", "cases": len(CASES)}
        raise ChainError("self-test accepted a trace ID metric label")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path)
    parser.add_argument("--work", type=Path)
    parser.add_argument("--self-test", action="store_true")
    args = parser.parse_args()
    try:
        if args.self_test:
            result = self_test()
        else:
            _require(args.manifest is not None and args.work is not None, "--manifest and --work are required")
            result = verify_directory(args.work, args.manifest)
        print(json.dumps(result, indent=2, sort_keys=True))
        return 0
    except (ChainError, OSError, TypeError, KeyError) as exc:
        print(json.dumps({"schema": SCHEMA, "execution_status": "incomplete", "error": str(exc)}, sort_keys=True))
        return 1


if __name__ == "__main__":
    raise SystemExit(main())

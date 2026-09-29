import hashlib
import json
import tempfile
import unittest
from pathlib import Path

from phase19_capacity import synthetic_sample
from phase19_evidence import (
    RECIPE_DIGEST,
    RECIPE_SCHEMA,
    STAGES,
    aggregate_repetitions,
    load_profile,
    validate_evidence,
)
from phase19_sampler import summarize_samples


REQUIRED_COMPONENTS = [
    "mysql", "redis", "rabbitmq", "elasticsearch", "observability-elasticsearch",
    "kafka", "victoriametrics", "backend", "backend-2", "business-worker",
    "business-worker-2", "search-indexer", "search-indexer-2", "router", "router-2",
    "marshaller", "marshaller-2", "monitor", "frontend", "admin-frontend",
]
ASYNC_FIELDS = ("outbox_pending", "rabbit_ready", "rabbit_unacked", "kafka_lag")
OBS_FIELDS = ("metric_count", "logs_count", "events_count", "marshaller_store_counts")


def _window(name, target, requests=None, duration=1):
    if name == "recovery":
        requests = 0
    if requests is None:
        requests = int(target * duration)
    return {
        "name": name,
        "target_rps": target,
        "duration_seconds": duration,
        "scheduled_slots": requests,
        "dropped_slots": 0,
        "max_schedule_lag_ms": 1,
        "completed_requests": requests,
        "achieved_rps": requests / duration,
        "outcomes": {
            "requests": requests,
            "succeeded": requests,
            "explicit_rejects": 0,
            "rejected_429": 0,
            "rejected_503": 0,
            "timeouts": 0,
            "transport_errors": 0,
            "unexpected_errors": 0,
        },
        "statuses": {"200": requests} if requests else {},
        "latency": {"p50_ms": 10, "p95_ms": 20, "p99_ms": 30, "max_ms": 40},
    }


def _progress(directory, repeat, profile, incomplete=False):
    path = Path(directory) / ("progress-%d.jsonl" % repeat)
    events = [("run_started", "", "", "started")]
    if not incomplete:
        for stage in profile["stages"]:
            events.extend([
                ("window_started", stage["name"], "warmup", "started"),
                ("window_finished", stage["name"], "warmup", "complete"),
                ("window_started", stage["name"], "measurement", "started"),
                ("window_finished", stage["name"], "measurement", "complete"),
                ("recovery_started", stage["name"], "recovery", "started"),
                ("recovery_finished", stage["name"], "recovery", "complete"),
            ])
    events.append(("run_finished", "", "", "incomplete" if incomplete else "complete"))
    records = []
    for sequence, (event, stage, window, status) in enumerate(events):
        record = {
            "schema_version": "gopulse.phase19.progress.v1",
            "sequence": sequence,
            "event": event,
            "status": status,
            "at": "2026-09-29T00:00:%02dZ" % sequence,
        }
        if stage:
            record.update({"stage": stage, "window": window})
        if event == "window_finished":
            stage_profile = next(item for item in profile["stages"] if item["name"] == stage)
            target = stage_profile["warmup_target_rps"] if window == "warmup" else stage_profile["target_rps"]
            duration = stage_profile["warmup_seconds"] if window == "warmup" else stage_profile["measurement_seconds"]
            record.update({"scheduled_slots": int(target * duration), "dropped_slots": 0})
        records.append(record)
    path.write_text("\n".join(json.dumps(record, sort_keys=True) for record in records) + "\n", encoding="utf-8")
    path.chmod(0o600)
    return path, records


def _report(profile, profile_digest, candidate, repeat, progress_reference, incomplete=False):
    stages = []
    for stage in profile["stages"]:
        status = "not_executed" if incomplete else "complete"
        count = 0 if incomplete else None
        stages.append({
            "name": stage["name"],
            "target_rps": stage["target_rps"],
            "status": status,
            "warmup": _window("warmup", stage["warmup_target_rps"], count, stage["warmup_seconds"]),
            "measurement": _window("measurement", stage["target_rps"], count, stage["measurement_seconds"]),
            "recovery": _window("recovery", 0, 0, stage["recovery_seconds"]),
        })
    total_requests = sum(stage[window]["outcomes"]["requests"] for stage in stages for window in ("warmup", "measurement"))
    return {
        "schema_version": "gopulse.phase19.load.v1",
        "profile": {"id": profile["profile_id"], "sha256": profile_digest},
        "candidate": candidate,
        "recipe": {"schema_version": RECIPE_SCHEMA, "seed": 18002005, "digest": RECIPE_DIGEST},
        "repeat": {"number": repeat, "total": 3},
        "execution_status": "incomplete" if incomplete else "complete",
        "started_at": "2026-09-29T00:00:00Z",
        "finished_at": "2026-09-29T00:01:00Z",
        "stages": stages,
        "total": {
            "requests": total_requests,
            "succeeded": total_requests,
            "explicit_rejects": 0,
            "rejected_429": 0,
            "rejected_503": 0,
            "timeouts": 0,
            "transport_errors": 0,
            "unexpected_errors": 0,
        },
        "load_process": {"rss_bytes": 100, "goroutines": 10, "heap_alloc_bytes": 20},
        "progress": progress_reference,
    }


def _resource_files(directory, repeat, profile):
    raw = Path(directory) / ("resources-%d.raw.jsonl" % repeat)
    records = []
    for sequence in range(2):
        record = synthetic_sample(sequence, profile["sampling"]["interval_seconds"])
        record["sample_kind"] = "initial" if sequence == 0 else "scheduled"
        record["observed_at"] = 1000 + sequence * profile["sampling"]["interval_seconds"]
        record["component_resources"] = {
            component: {
                "cpu_percent": 0.0,
                "rss_bytes": 1,
                "running": True,
                "restart_count": 0,
                "oom_killed": False,
            }
            for component in REQUIRED_COMPONENTS
        }
        records.append(record)
    raw.write_text("\n".join(json.dumps(record, sort_keys=True) for record in records) + "\n", encoding="utf-8")
    raw.chmod(0o600)
    summary = Path(directory) / ("resources-%d.json" % repeat)
    _, summary_value = summarize_samples(raw, summary, expected_interval=profile["sampling"]["interval_seconds"], required_components=REQUIRED_COMPONENTS)
    return raw, summary, summary_value


def _stage_receipts(directory, repeat, stage_index):
    values = {}
    stage = STAGES[stage_index]
    for kind, schema, fields in (
        ("async", "gopulse.phase19.async-recovery.v1", ASYNC_FIELDS),
        ("obs", "gopulse.phase19.observability-recovery.v1", OBS_FIELDS),
    ):
        path = Path(directory) / ("%s-%d-%s.jsonl" % (kind, repeat, stage))
        first = {"schema": schema, "stage": stage, "observed_at": 100.0}
        last = {"schema": schema, "stage": stage, "observed_at": 101.0}
        if kind == "async":
            first.update({"outbox_pending": 1, "rabbit_ready": 1, "rabbit_unacked": 1, "kafka_lag": 1})
            last.update({"outbox_pending": 0, "rabbit_ready": 0, "rabbit_unacked": 0, "kafka_lag": 0})
        else:
            first.update({"metric_count": 0, "logs_count": 0, "events_count": 0, "marshaller_store_counts": {"metrics": 0, "logs": 0, "events": 0}})
            last.update({"metric_count": 1, "logs_count": 1, "events_count": 1, "marshaller_store_counts": {"metrics": 1, "logs": 1, "events": 1}})
        path.write_text(json.dumps(first, sort_keys=True) + "\n" + json.dumps(last, sort_keys=True) + "\n", encoding="utf-8")
        path.chmod(0o600)
        values[kind] = {
            "stage": stage,
            "raw_path": path.name,
            "sha256": "sha256:" + hashlib.sha256(path.read_bytes()).hexdigest(),
            "records": 2,
            "baseline": {key: first[key] for key in ("observed_at", *fields)},
            "terminal": {key: last[key] for key in ("observed_at", *fields)},
            "recovery_seconds": 1.0,
        }
    return values["async"], values["obs"]


def _evidence(directory, incomplete=False):
    root = Path(directory)
    source_root = Path(__file__).resolve().parents[2]
    profile_path = root / "capacity-profile.json"
    profile_path.write_bytes((source_root / "loadtest/capacity-profile.json").read_bytes())
    profile, profile_digest = load_profile(profile_path)
    candidate = {"version": "2.1.3", "revision": "b" * 40, "manifest_sha256": "sha256:" + "c" * 64}
    compose = root / "bound-compose.yaml"
    runtime = root / "bound-runtime-contracts.json"
    compose.write_bytes((source_root / "deploy/compose.yaml").read_bytes())
    runtime.write_bytes((source_root / "deploy/runtime-contracts.json").read_bytes())
    bindings = {
        "compose": {"path": compose.name, "sha256": "sha256:" + hashlib.sha256(compose.read_bytes()).hexdigest()},
        "runtime_contract": {"path": runtime.name, "sha256": "sha256:" + hashlib.sha256(runtime.read_bytes()).hexdigest()},
        "recipe_descriptor": {"schema_version": RECIPE_SCHEMA, "seed": 18002005, "digest": RECIPE_DIGEST},
        "runner": {"source_commit": "a" * 40, "binary_sha256": "sha256:" + "d" * 64},
    }
    rounds = []
    for number in range(1, 4):
        is_incomplete = incomplete and number == 1
        progress_path, progress_records = _progress(root, number, profile, incomplete=is_incomplete)
        progress = {"path": progress_path.name, "sha256": "sha256:" + hashlib.sha256(progress_path.read_bytes()).hexdigest(), "records": len(progress_records)}
        report = _report(profile, profile_digest, candidate, number, progress, incomplete=is_incomplete)
        raw, summary, summary_value = _resource_files(root, number, profile)
        asynchronous = []
        observability = []
        for stage_index in range(4):
            async_receipt, obs_receipt = _stage_receipts(root, number, stage_index)
            asynchronous.append(async_receipt)
            observability.append(obs_receipt)
        cleanup = {"status": "passed", "project": "gopulse-p19-%012x" % number, "owned": True, "global_prune": False}
        rounds.append({
            "number": number,
            "project": cleanup["project"],
            "endpoint": {"base_url": "http://127.0.0.1:%d" % (19080 + number), "port": 19080 + number},
            "execution_status": report["execution_status"],
            "recipe": {
                "receipt": {"schema_version": RECIPE_SCHEMA, "seed": 18002005, "candidate": candidate, "digest": RECIPE_DIGEST},
                "nonempty_rejection": {"exit_code": 3, "target_unchanged": True},
            },
            "load_report": report,
            "resources": {"raw_path": raw.name, "summary_path": summary.name, "sha256": "sha256:" + hashlib.sha256(raw.read_bytes()).hexdigest(), "records": summary_value["samples"], "required_components": REQUIRED_COMPONENTS},
            "asynchronous": asynchronous,
            "observability": observability,
            "cleanup": cleanup,
            "stop": {"reason": "profile_hard_error", "stage": "rps-50", "detail": "test"} if is_incomplete else None,
        })
    document = {
        "schema": "gopulse.phase19.capacity-evidence.v1",
        "execution_status": "incomplete" if incomplete else "complete",
        "capability_status": "incomplete" if incomplete else "target_met",
        "profile": {"path": profile_path.name, "id": profile["profile_id"], "sha256": profile_digest},
        "candidate": candidate,
        "recipe": {"schema_version": RECIPE_SCHEMA, "seed": 18002005, "digest": RECIPE_DIGEST},
        "bindings": bindings,
        "rounds": rounds,
        "cleanup": rounds[-1]["cleanup"],
        "capability": {"stage_gates": [] if incomplete else [{"stage": stage, "passed": True} for stage in STAGES]},
    }
    if not incomplete:
        document["aggregates"] = aggregate_repetitions(rounds)
    return document


class EvidenceTest(unittest.TestCase):
    def test_valid_evidence_recomputes_three_raw_repetitions(self):
        with tempfile.TemporaryDirectory() as directory:
            document = _evidence(directory)
            self.assertIs(validate_evidence(document, directory), document)

    def test_tampered_aggregate_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            document = _evidence(directory)
            document["aggregates"][0]["aggregates"]["p95_ms"]["median"] = 999
            with self.assertRaisesRegex(ValueError, "aggregates"):
                validate_evidence(document, directory)

    def test_profile_binding_drift_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            document = _evidence(directory)
            document["rounds"][1]["load_report"]["profile"]["sha256"] = "sha256:" + "f" * 64
            with self.assertRaisesRegex(ValueError, "profile binding"):
                validate_evidence(document, directory)

    def test_incomplete_evidence_preserves_unexecuted_stage_and_has_no_conclusion(self):
        with tempfile.TemporaryDirectory() as directory:
            document = _evidence(directory, incomplete=True)
            self.assertIs(validate_evidence(document, directory), document)
            self.assertEqual(document["capability_status"], "incomplete")


if __name__ == "__main__":
    unittest.main()

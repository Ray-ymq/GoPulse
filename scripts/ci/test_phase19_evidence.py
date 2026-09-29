import copy
import hashlib
import json
import tempfile
import unittest
from pathlib import Path

from phase19_evidence import (
    RECIPE_DIGEST,
    RECIPE_SCHEMA,
    STAGES,
    aggregate_repetitions,
    load_profile,
    validate_evidence,
)


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
        "outcomes": {"requests": requests, "succeeded": requests, "explicit_rejects": 0, "rejected_429": 0, "rejected_503": 0, "timeouts": 0, "transport_errors": 0, "unexpected_errors": 0},
        "statuses": {"200": requests} if requests else {},
        "latency": {"p50_ms": 10, "p95_ms": 20, "p99_ms": 30, "max_ms": 40},
    }


def _report(profile, profile_digest, candidate, repeat):
    stages = []
    for stage in profile["stages"]:
        stages.append({
            "name": stage["name"], "target_rps": stage["target_rps"], "status": "complete",
            "warmup": _window("warmup", stage["warmup_target_rps"], duration=stage["warmup_seconds"]),
            "measurement": _window("measurement", stage["target_rps"], duration=stage["measurement_seconds"]),
            "recovery": _window("recovery", 0, duration=stage["recovery_seconds"]),
        })
    total_requests = sum(stage[window]["outcomes"]["requests"] for stage in stages for window in ("warmup", "measurement"))
    return {
        "schema_version": "gopulse.phase19.load.v1",
        "profile": {"id": profile["profile_id"], "sha256": profile_digest},
        "candidate": {"version": "2.1.3", "revision": "b" * 40, "manifest_sha256": "sha256:" + "c" * 64},
        "recipe": {"schema_version": RECIPE_SCHEMA, "seed": 18002005, "digest": RECIPE_DIGEST},
        "repeat": {"number": repeat, "total": 3},
        "execution_status": "complete",
        "started_at": "2026-09-29T00:00:00Z", "finished_at": "2026-09-29T00:01:00Z",
        "stages": stages,
        "total": {"requests": total_requests, "succeeded": total_requests, "explicit_rejects": 0, "rejected_429": 0, "rejected_503": 0, "timeouts": 0, "transport_errors": 0, "unexpected_errors": 0},
        "load_process": {"rss_bytes": 100, "goroutines": 10, "heap_alloc_bytes": 20},
    }


def _evidence(directory, incomplete=False):
    profile_path = Path(directory) / "capacity-profile.json"
    source = Path(__file__).resolve().parents[2] / "loadtest/capacity-profile.json"
    profile_path.write_bytes(source.read_bytes())
    profile, profile_digest = load_profile(profile_path)
    rounds = []
    for number in range(1, 4):
        report = _report(profile, profile_digest, {"version": "2.1.3", "revision": "b" * 40, "manifest_sha256": "sha256:" + "c" * 64}, number)
        if incomplete and number == 1:
            report["execution_status"] = "incomplete"
            report["total"] = {"requests": 0, "succeeded": 0, "explicit_rejects": 0, "rejected_429": 0, "rejected_503": 0, "timeouts": 0, "transport_errors": 0, "unexpected_errors": 0}
            for stage in report["stages"]:
                stage["status"] = "not_executed"
                stage["warmup"] = _window("warmup", stage["warmup"]["target_rps"], 0, stage["warmup"]["duration_seconds"])
                stage["measurement"] = _window("measurement", stage["measurement"]["target_rps"], 0, stage["measurement"]["duration_seconds"])
                stage["recovery"] = _window("recovery", 0, 0, stage["recovery"]["duration_seconds"])
        raw = Path(directory) / ("r%d.raw.jsonl" % number)
        raw.write_text(json.dumps({"schema": "gopulse.phase19.resources.v1", "sequence": 0, "observed_at": number, "interval_seconds": 5}) + "\n")
        summary = Path(directory) / ("r%d.summary.json" % number)
        summary.write_text(json.dumps({"schema": "gopulse.phase19.resources.v1", "summary": {"samples": 1}}) + "\n")
        cleanup = {"status": "passed", "project": "gopulse-p19-%012x" % number, "owned": True, "global_prune": False}
        rounds.append({
            "number": number, "project": cleanup["project"], "execution_status": report["execution_status"], "load_report": report,
            "resources": {"raw_path": raw.name, "summary_path": summary.name, "sha256": "sha256:" + hashlib.sha256(raw.read_bytes()).hexdigest(), "records": 1},
            "asynchronous": [{"recovery_seconds": stage["recovery_seconds"], "outbox_pending": 0, "rabbit_ready": 0, "rabbit_unacked": 0, "kafka_lag": 0} for stage in profile["stages"]],
            "observability": [{"recovery_seconds": stage["recovery_seconds"], "metric_progress": True, "log_progress": True, "event_progress": True} for stage in profile["stages"]],
            "cleanup": cleanup, "stop": {"reason": "profile_hard_error", "stage": "rps-50", "detail": "test"} if report["execution_status"] == "incomplete" else None,
        })
    document = {
        "schema": "gopulse.phase19.capacity-evidence.v1",
        "execution_status": "incomplete" if incomplete else "complete",
        "capability_status": "incomplete" if incomplete else "target_met",
        "profile": {"path": profile_path.name, "id": profile["profile_id"], "sha256": profile_digest},
        "candidate": {"version": "2.1.3", "revision": "b" * 40, "manifest_sha256": "sha256:" + "c" * 64},
        "recipe": {"schema_version": RECIPE_SCHEMA, "seed": 18002005, "digest": RECIPE_DIGEST},
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

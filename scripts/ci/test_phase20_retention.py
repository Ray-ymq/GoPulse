import json
import tempfile
import unittest
from pathlib import Path

try:
    from phase20_evidence import Incomplete, digest, verify_retention_directory
except ModuleNotFoundError:
    from scripts.ci.phase20_evidence import Incomplete, digest, verify_retention_directory


class RetentionEvidenceTests(unittest.TestCase):
    def build_fixture(self, root: Path) -> None:
        raw = root / "raw"
        raw.mkdir()
        manifest = root / "candidate-manifest.json"
        manifest.write_text(json.dumps({"version": "2.2.4", "revision": "a" * 40}) + "\n")
        (raw / "elasticsearch-go-test.txt").write_text("ok  github.com/Ray-ymq/GoPulse/marshaller/internal/retention\n")
        (raw / "R04-go-test.txt").write_text("ok  github.com/Ray-ymq/GoPulse/marshaller/internal/elasticsearch\n")
        sequence = [
            "R01 boundary and ownership fixtures created",
            "R02 pre-delete inventory/documents recorded",
            "R02 deletion completed and current aliases queried",
            "R05 transient delete retried and permission delete remained blocked",
            "R06 two runners completed idempotent cleanup",
            "R04 writer precheck passed",
            "R04 cleanup deleted before in-flight write release",
            "R04 in-flight write released and final storage queried",
        ]
        values = {
            "R01": {"case_id": "R01", "status": "pass", "cutoff": "2026.09.24", "sequence": sequence, "fixtures": ["invalid date", "future date", "unmarked same-prefix", "business index"], "ownership_proof": ["cluster_uuid", "strict_mapping", "_meta", "fixed_alias"]},
            "R02": {"case_id": "R02", "status": "pass", "facts": {"logs_after": {"exists": False}, "events_after": {"exists": False}}, "current": {"logs_after": {"exists": True}, "events_after": {"exists": True}}, "query_aliases": ["gopulse-logs-v1-read", "gopulse-events-v1-read"], "sequence": sequence},
            "R04": {"case_id": "R04", "status": "pass", "sequence": sequence, "go_test": "R04-go-test.txt"},
            "R05": {"case_id": "R05", "status": "pass", "sequence": sequence, "catchup_deadline_seconds": 60, "permission_failure_not_hidden": True},
            "R06": {"case_id": "R06", "status": "pass", "sequence": sequence, "idempotent_404_allowed": True},
            "R07": {"case_id": "R07", "status": "pass", "aliases": ["gopulse-logs-v1-read", "gopulse-events-v1-read"], "expired_indices_empty": True, "business_fixture_preserved": True},
            "R08": {"case_id": "R08", "status": "pass", "vm": "R08-vm.json", "trace": "R08-trace.json"},
        }
        (raw / "R03-go-test.txt").write_text("ok  github.com/Ray-ymq/GoPulse/marshaller/internal/consumer\n")
        values["R03"] = {"case_id": "R03", "status": "pass", "go_test": "R03-go-test.txt", "expired_codes": ["expired_log_retention", "expired_event_retention"], "permanent_commit": True, "no_index_revival": True}
        vm = {"retention_period": "30d", "current_query": True, "within_window_submitted": True, "outside_window_submitted": True, "observed_physical_reclaim": False}
        trace = {"inventory": [{"name": "spans.jsonl", "bytes": 100, "path": "/var/lib/gopulse/trace/spans.jsonl"}, {"name": "spans.jsonl.1", "bytes": 100, "path": "/var/lib/gopulse/trace/spans.jsonl.1"}], "rotation_observed": True}
        (raw / "R08-vm.json").write_text(json.dumps(vm))
        (raw / "R08-trace.json").write_text(json.dumps(trace))
        artifacts = {key: {"id": "sha256:" + key * 64} for key in ("elasticsearch", "victoriametrics", "collector")}
        cases = []
        for case_id in ("R01", "R02", "R03", "R04", "R05", "R06", "R07", "R08"):
            path = raw / f"{case_id}.json"
            path.write_text(json.dumps(values[case_id]))
            cases.append({"case_id": case_id, "status": "pass", "path": f"raw/{path.name}", "sha256": digest(path)})
        implementation = {
            "go_integration": "raw/elasticsearch-go-test.txt",
            "config": {"logs_days": 7, "events_days": 7, "cycle_seconds": 60, "batch_indices": 16, "request_timeout_seconds": 3, "round_timeout_seconds": 15, "retry_min_seconds": 0.25, "retry_max_seconds": 5, "max_retries": 3, "catchup_deadline_seconds": 60},
            "prefixes": ["gopulse-logs-v1-", "gopulse-events-v1-"],
            "aliases": ["gopulse-logs-v1-read", "gopulse-events-v1-read"],
        }
        document = {"schema": "gopulse.phase20.retention.v1", "candidate": {"version": "2.2.4", "revision": "a" * 40, "manifest_sha256": digest(manifest)}, "execution_status": "complete", "dependency_fixtures": artifacts, "cases": cases, "implementation": implementation}
        (root / "retention.json").write_text(json.dumps(document))

    def test_verifier_recomputes_all_cases(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.build_fixture(root)
            result = verify_retention_directory(root)
            self.assertEqual(result["execution_status"], "complete")
            self.assertEqual(set(result["case_status"]), {f"R0{i}" for i in range(1, 9)})

    def test_verifier_rejects_tampered_raw_receipt(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.build_fixture(root)
            path = root / "raw/R04.json"
            path.write_text(path.read_text().replace("pass", "fail", 1))
            with self.assertRaises(Incomplete):
                verify_retention_directory(root)


if __name__ == "__main__":
    unittest.main()

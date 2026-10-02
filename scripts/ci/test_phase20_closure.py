import json
import tempfile
import unittest
from pathlib import Path

try:
    import phase20_budget as budget
    import phase20_closure as closure
except ModuleNotFoundError:
    from scripts.ci import phase20_budget as budget
    from scripts.ci import phase20_closure as closure


class ClosureTests(unittest.TestCase):
    def test_search_probe_overhead_is_per_request(self):
        probes = [
            {"observed_at_ms": 1000, "probe_overhead_ms": 12},
            {"observed_at_ms": 2000, "probe_overhead_ms": 19},
            {"observed_at_ms": 5000, "probe_overhead_ms": 7},
        ]
        self.assertEqual(closure._search_probe_overhead_ms(probes), 19)

    def test_c01_metrics_cover_all_business_replicas(self):
        targets = closure._c01_metric_targets()
        self.assertEqual([target[1] for target in targets], [
            ("backend", "backend-2"),
            ("business-worker", "business-worker-2"),
            ("search-indexer", "search-indexer-2"),
        ])
        self.assertEqual({replica for _, replicas, _, _ in targets for replica in replicas}, {
            "backend", "backend-2", "business-worker", "business-worker-2", "search-indexer", "search-indexer-2",
        })

    def test_candidate_artifact_set_is_immutable_and_complete(self):
        images = {name: {"ref": f"gopulse/{name}:test@sha256:{'a' * 64}", "id": "sha256:" + "a" * 64} for name in closure.SELF_IMAGE_NAMES}
        third_party = {name: {"ref": f"example/{name}:test@sha256:{'b' * 64}", "id": "sha256:" + "b" * 64} for name in budget.load_contract()["dependencies"]["images"] if name != "trace-collector"}
        manifest = {"images": images, "third_party": third_party, "trace_collector": {"ref": "otel/collector:test@sha256:" + "c" * 64, "id": "sha256:" + "c" * 64}}
        result = closure.check_candidate_artifacts(manifest)
        self.assertEqual(result["status"], "pass")
        images["backend"]["id"] = "not-a-digest"
        with self.assertRaises(closure.Incomplete):
            closure.check_candidate_artifacts(manifest)

    def test_closure_verifier_rejects_receipt_tampering(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            manifest = {
                "version": "2.2.5",
                "revision": "a" * 40,
                "images": {name: {"ref": f"gopulse/{name}:test@sha256:{'a' * 64}", "id": "sha256:" + "a" * 64} for name in closure.SELF_IMAGE_NAMES},
                "third_party": {name: {"ref": f"example/{name}:test@sha256:{'b' * 64}", "id": "sha256:" + "b" * 64} for name in budget.load_contract()["dependencies"]["images"] if name != "trace-collector"},
                "trace_collector": {"ref": "otel/collector:test@sha256:" + "c" * 64, "id": "sha256:" + "c" * 64},
            }
            (root / "candidate-manifest.json").write_text(json.dumps(manifest) + "\n")
            binding = {"version": "2.2.5", "revision": "a" * 40, "manifest_sha256": budget.digest(root / "candidate-manifest.json")}
            receipts = []
            values = {
                "U1": {"case_id": "U1", "status": "pass", "candidate": binding, "diagnostic": {"status": "pass"}},
                "U2": {"case_id": "U2", "status": "pass", "candidate": binding, "fault": {"stop_effective": True, "recovery_status": "pass"}, "recovery": {"status": "pass"}},
                "U3": {"case_id": "U3", "status": "pass", "candidate": binding, "executed": [{"command": ["true"], "returncode": 0}], "not_reused": True},
                "U4": {"case_id": "U4", "status": "pass", "candidate": binding, "artifacts": {"status": "pass"}},
            }
            for case_id, value in values.items():
                path = root / (case_id + ".json")
                path.write_text(json.dumps(value) + "\n")
                receipts.append({"case_id": case_id, "status": "pass", "path": path.name, "sha256": budget.digest(path)})
            document = {"schema": closure.SCHEMA, "formal": False, "candidate": binding, "contract_sha256": budget.digest(budget.CONTRACT_PATH), "receipts": receipts, "preflight": {"status": "pass"}, "execution_status": "complete"}
            (root / "closure.json").write_text(json.dumps(document) + "\n")
            self.assertEqual(closure.verify_closure_directory(root)["execution_status"], "complete")
            values["U3"]["not_reused"] = False
            (root / "U3.json").write_text(json.dumps(values["U3"]) + "\n")
            with self.assertRaises(closure.Incomplete):
                closure.verify_closure_directory(root)


if __name__ == "__main__":
    unittest.main()

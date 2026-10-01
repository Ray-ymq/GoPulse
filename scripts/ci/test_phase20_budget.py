import copy
import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

try:
    import phase20_budget as budget
except ModuleNotFoundError:
    from scripts.ci import phase20_budget as budget


class BudgetTests(unittest.TestCase):
    def test_parse_bytes_accepts_compose_units_and_rejects_unknown(self):
        self.assertEqual(budget.parse_bytes("1536m"), 1536 * 1024 * 1024)
        self.assertEqual(budget.parse_bytes("512MiB"), 512 * 1024 * 1024)
        with self.assertRaises(budget.Incomplete):
            budget.parse_bytes("TBD")

    def test_nano_cpu_match_allows_docker_conversion_rounding_only(self):
        self.assertTrue(budget.nano_cpus_match(4_499_999_744, 4_500_000_000))
        self.assertFalse(budget.nano_cpus_match(4_499_000_000, 4_500_000_000))

    def test_contract_is_finite_and_profile_bound(self):
        contract = budget.load_contract()
        capacity, sustained = budget.load_profiles(contract)
        self.assertEqual(contract["candidate_version"], "2.2.5")
        self.assertEqual(capacity["resource_budget"]["contract_id"], contract["contract_id"])
        self.assertEqual(sustained["resource_budget_contract_id"], contract["contract_id"])
        self.assertEqual(contract["platform"]["disk_free_bytes_min"], 50_000_000_000)
        disk_budget = next(item for item in contract["budgets"] if item["budget_id"] == "disk.host_free_min")
        self.assertEqual(disk_budget["threshold"], 50_000_000_000)

    def test_candidate_env_declares_recipe_bootstrap_user(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "candidate.env"
            values = budget._candidate_env({"version": "2.2.5", "revision": "a" * 40}, output)
            self.assertEqual(values["GOPULSE_BOOTSTRAP_USER_ID"], "1")
            self.assertIn("GOPULSE_BOOTSTRAP_USER_ID=1\n", output.read_text())

    def test_resource_summary_recomputes_peak_and_minimum(self):
        contract = budget.load_contract()
        rows = []
        for sequence, cpu, rss, free, ready, unacked, lag in ((0, 400, 100, 100_000_000_000, 1, 2, 4), (1, 500, 200, 99_000_000_000, 2, 3, 5)):
            rows.append({
                "missing_signals": [],
                "failure": None,
                "signals": {
                    "containers": [{"service": "backend", "cpu_percent": cpu, "rss_bytes": rss, "oom": False}],
                    "host": {"disk_free_bytes": free},
                    "rabbitmq": [{"name": "q", "ready": ready, "unacked": unacked}],
                    "kafka_lag": [{"partition": 0, "lag": lag}],
                },
                "sequence": sequence,
            })
        summary = budget.summarize_resources(rows, contract)
        self.assertEqual(summary["cpu_sut_peak_cores"], 5)
        self.assertEqual(summary["rss_sut_peak_bytes"], 200)
        self.assertEqual(summary["host_free_min_bytes"], 99_000_000_000)
        self.assertEqual(summary["rabbit_ready_peak"], 2)
        self.assertEqual(summary["kafka_lag_peak"], 5)

    def test_preflight_directory_requires_every_case_and_digest(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            manifest = root / "candidate-manifest.json"
            manifest.write_text(json.dumps({"version": "2.2.5", "revision": "a" * 40}) + "\n")
            cases = []
            raw_values = {
                "B01": {"case_id": "B01", "status": "pass", "rendered": {"status": "pass"}, "inspection": {"status": "pass"}},
                "B04": {"case_id": "B04", "status": "pass", "injection": {"effective": True}, "recovery": {"status": "pass"}},
                "B05": {"case_id": "B05", "status": "pass", "injection": {"effective": True}, "recovery": {"status": "pass"}},
                "B06": {"case_id": "B06", "status": "pass", "injection": {"effective": True}, "recovery": {"status": "pass"}},
                "B07": {"case_id": "B07", "status": "pass", "preflight": {"status": "pass"}},
            }
            for case_id, raw in raw_values.items():
                path = root / (case_id + ".json")
                path.write_text(json.dumps(raw) + "\n")
                cases.append({"case_id": case_id, "status": "pass", "path": path.name, "sha256": budget.digest(path)})
            binding = {"version": "2.2.5", "revision": "a" * 40, "manifest_sha256": budget.digest(manifest)}
            document = {"schema": "gopulse.phase20.budget.v1", "formal": False, "candidate": binding, "contract_sha256": budget.digest(budget.CONTRACT_PATH), "cases": cases, "execution_status": "complete"}
            (root / "budget.json").write_text(json.dumps(document) + "\n")
            result = budget.verify_budget_directory(root, formal=False)
            self.assertEqual(result["execution_status"], "complete")
            raw_values["B05"]["recovery"]["status"] = "fail"
            (root / "B05.json").write_text(json.dumps(raw_values["B05"]) + "\n")
            with self.assertRaises(budget.Incomplete):
                budget.verify_budget_directory(root, formal=False)

    def test_overhead_requires_four_combinations_and_three_repetitions(self):
        contract = budget.load_contract()
        value = {"formal": False, "trials": []}
        for combo in ("O0", "O1", "O2", "O3"):
            for repeat in (1, 2, 3):
                p99 = {"O0": 10, "O1": 11, "O2": 12, "O3": 12}[combo]
                value["trials"].append({"combination_id": combo, "repeat": repeat, "target_rps": 200, "warmup_seconds": 15, "measurement_seconds": 60, "execution_status": "complete", "business_errors": 0, "p99_ms": p99, "cpu_peak_cores": 1, "rss_peak_bytes": 100, "sampler_cpu_peak_cores": 0.1, "sampler_cpu_seconds": 1.0, "missing_sample_ratio": 0})
        result = budget.validate_overhead_trials(value, contract)
        self.assertEqual(result["trials"], 12)
        self.assertEqual(result["comparisons"]["normal_observability"]["p99_ms"]["baseline"], 10)
        self.assertEqual(result["comparisons"]["normal_observability"]["p99_ms"]["enabled"], 12)
        self.assertEqual(result["comparisons"]["sampler"]["p99_ms"]["delta"], -1)
        value["trials"].pop()
        with self.assertRaises(budget.Incomplete):
            budget.validate_overhead_trials(value, contract)

    def test_lightweight_counter_uses_owned_docker_stats(self):
        counter = budget.LightweightResourceCounter(
            "owned-project", Path("candidate.env"), [Path("compose.yaml")], 5, "O3-1", Path("resources.jsonl")
        )
        inspected = [{
            "Id": "container-id",
            "Config": {"Labels": {
                "com.docker.compose.project": "owned-project",
                "com.docker.compose.service": "backend",
            }},
            "State": {"OOMKilled": False, "Running": True},
        }]
        with mock.patch.object(budget, "_compose_args", return_value=["docker", "compose"]), mock.patch.object(budget, "command", return_value=object()), mock.patch.object(
            budget,
            "require",
            side_effect=["container-id\n", json.dumps(inspected), '{"ID":"container-id","CPUPerc":"12.50%","MemUsage":"10MiB / 1GiB"}\n'],
        ) as require:
            rows = counter._capture()
        self.assertEqual(rows, [{"service": "backend", "cpu_percent": 12.5, "rss_bytes": 10 * 1024 * 1024, "oom": False, "running": True}])
        self.assertEqual(require.call_count, 3)


if __name__ == "__main__":
    unittest.main()

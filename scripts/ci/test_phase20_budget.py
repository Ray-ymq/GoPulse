import copy
import json
import subprocess
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

    def test_backlog_is_nonempty_for_outbox_or_rabbit_waterline(self):
        self.assertTrue(budget._backlog_is_nonempty({"queue": {"ready": 0, "unacked": 0}, "async": {"outbox_pending": 1}}))
        self.assertTrue(budget._backlog_is_nonempty({"queue": {"ready": 2, "unacked": 0}, "async": {"outbox_pending": 0}}))
        self.assertFalse(budget._backlog_is_nonempty({"queue": {"ready": 0, "unacked": 0}, "async": {"outbox_pending": 0}}))


class B06Tests(unittest.TestCase):
    target_ids = {name: name + "-id" for name in ("business-worker", "business-worker-2", "phase20-collector")}

    def test_case_dispatch_selects_each_fault_runner(self):
        self.assertIs(budget.run_b04, {"B04": budget.run_b04, "B05": budget.run_b05, "B06": budget.run_b06}["B04"])
        self.assertIs(budget.run_b05, {"B04": budget.run_b04, "B05": budget.run_b05, "B06": budget.run_b06}["B05"])
        self.assertIs(budget.run_b06, {"B04": budget.run_b04, "B05": budget.run_b05, "B06": budget.run_b06}["B06"])

    def states(self, running=True):
        return [{"Id": container_id, "Config": {"Labels": {"com.docker.compose.project": "owned", "com.docker.compose.service": service}},
                 "State": {"Running": running, "Restarting": False, "OOMKilled": False, "ExitCode": 0}}
                for service, container_id in self.target_ids.items()]

    def test_inspect_rejects_foreign_duplicate_missing_and_oom_targets(self):
        stack = {"project": "owned"}
        for invalid in ({}, {"a": ""}, {"a": "id", "b": "id"}, {"a": "id\nother"}):
            with self.subTest(invalid=invalid), self.assertRaises(budget.Incomplete), mock.patch.object(budget, "command") as command:
                budget._b06_target_states(stack, invalid, 5)
            command.assert_not_called()
        for defect in ("foreign", "missing", "oom"):
            states = self.states()
            if defect == "foreign":
                states[0]["Config"]["Labels"]["com.docker.compose.project"] = "other"
            elif defect == "missing":
                states.pop()
            else:
                states[0]["State"]["OOMKilled"] = True
            with self.subTest(defect=defect), mock.patch.object(budget, "command", return_value=subprocess.CompletedProcess([], 0, json.dumps(states), "")), self.assertRaises(budget.Incomplete):
                budget._b06_target_states(stack, self.target_ids, 5)

    def test_wait_observes_async_exit_and_health_before_returning(self):
        starting = self.states()
        starting[0]["State"]["Health"] = {"Status": "starting"}
        for running, sequence in ((False, [self.states(), self.states(False)]), (True, [starting, self.states()])):
            with self.subTest(running=running), mock.patch.object(budget, "_b06_target_states", side_effect=sequence) as inspect, mock.patch.object(budget.time, "sleep"):
                result = budget._b06_wait_targets({}, self.target_ids, running=running, deadline=budget.time.monotonic() + 2)
            self.assertEqual(inspect.call_count, 2)
            self.assertEqual(result[0]["State"]["Running"], running)

    def test_expired_wait_does_not_inspect_or_start(self):
        with mock.patch.object(budget, "_b06_target_states") as inspect, self.assertRaises(budget.Incomplete):
            budget._b06_wait_targets({}, self.target_ids, running=False, deadline=budget.time.monotonic() - 1)
        inspect.assert_not_called()

    def test_backlog_counts_are_real_and_queries_share_deadline(self):
        stack = {"args": ["docker", "compose"]}
        deadline = budget.time.monotonic() + 2
        with mock.patch.object(budget, "command", side_effect=[subprocess.CompletedProcess([], 0, "1\n", ""), subprocess.CompletedProcess([], 0, "name messages_ready messages_unacknowledged\nq 0 0\n", "")]) as command:
            result = budget._b06_wait_backlog(stack, deadline)
        self.assertEqual(result["async"]["outbox_pending"], 1)
        self.assertTrue(all(0 < call.kwargs["timeout"] <= 2 for call in command.call_args_list))
        with mock.patch.object(budget, "command", return_value=subprocess.CompletedProcess([], 0, "invalid", "")), self.assertRaises(budget.Incomplete):
            budget._b06_wait_backlog(stack, deadline)

    def run_fixture(self, directory, *, stop_error=None, start_error=None, write_error=None):
        stack = {"project": "owned", "args": ["docker", "compose", "-p", "owned"], "api": object(), "cleaned": False}
        events = []

        def command(args, **kwargs):
            events.append(args)
            if "ps" in args:
                return subprocess.CompletedProcess(args, 0, self.target_ids[args[-1]] + "\n", "")
            return subprocess.CompletedProcess(args, 0, "", "")

        def finish(value):
            events.append(["cleanup"])
            value["cleaned"] = True
            return {"owned": True}

        def wait(*args, **kwargs):
            events.append(["await-exit"])
            if stop_error:
                raise stop_error
            return self.states(False)

        def start(*args):
            events.append(["start"])
            evidence = budget.read_json(Path(directory) / "B06" / "shutdown.json")
            self.assertEqual(evidence["status"], "pass")
            self.assertTrue(all(not item["State"]["Running"] for item in evidence["states"]))
            if start_error:
                raise start_error
            return self.states()

        with mock.patch.object(budget, "_prepare_fault_stack", return_value=stack), mock.patch.object(budget, "command", side_effect=command), mock.patch.object(budget, "_b06_target_states", return_value=self.states()), mock.patch.object(budget, "_api_post", side_effect=write_error, return_value={"status": 201}), mock.patch.object(budget, "_b06_wait_backlog", return_value={"queue": {"ready": 4}}), mock.patch.object(budget, "_b06_wait_targets", side_effect=wait), mock.patch.object(budget, "_b06_start_targets", side_effect=start), mock.patch.object(budget, "_wait_empty", return_value={"status": "pass"}) as waterline, mock.patch.object(budget, "_finish_fault_stack", side_effect=finish):
            if stop_error or start_error or write_error:
                with self.assertRaises(budget.Incomplete):
                    budget.run_b06(Path(directory), Path("manifest"), {}, Path("recipe"), {})
                waterline.assert_not_called()
            else:
                result = budget.run_b06(Path(directory), Path("manifest"), {}, Path("recipe"), {})
                self.assertEqual(result["injection"]["business_statuses"], [201] * 4)
                self.assertEqual(result["injection"]["backlog"]["queue"]["ready"], 4)
                waterline.assert_called_once_with(stack, 120)
        self.assertTrue(stack["cleaned"])
        return events, budget.read_json(Path(directory) / "B06" / "shutdown.json")

    def test_signal_exit_evidence_start_and_waterline_order(self):
        with tempfile.TemporaryDirectory() as directory:
            events, evidence = self.run_fixture(directory)
        signals = [item[3] for item in events if item[:3] == ["docker", "kill", "-s"]]
        self.assertEqual(signals, ["SIGSTOP", "SIGTERM", "SIGCONT"])
        self.assertLess(events.index(["await-exit"]), events.index(["start"]))
        self.assertTrue(all(item["State"]["Running"] for item in evidence["before_signal"]))
        self.assertFalse(any("stop" in item or "up" in item for item in events))

    def test_failed_shutdown_or_start_never_reaches_waterline_and_preserves_evidence(self):
        for failure in ("stop_error", "start_error", "write_error"):
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as directory:
                events, evidence = self.run_fixture(directory, **{failure: budget.Incomplete(failure)})
                self.assertEqual(evidence["error"], failure)
                if failure == "stop_error":
                    self.assertNotIn(["start"], events)
                if failure == "write_error":
                    self.assertTrue(any("SIGCONT" in item for item in events))

    def test_start_collector_before_workers_and_stop_on_failed_start(self):
        stack = {"args": ["docker", "compose"]}
        with mock.patch.object(budget, "command", return_value=subprocess.CompletedProcess([], 0, "", "")) as command, mock.patch.object(budget, "_b06_wait_targets", return_value=[]) as wait:
            budget._b06_start_targets(stack, self.target_ids)
        self.assertEqual([call.args[0] for call in command.call_args_list], [stack["args"] + ["start", "phase20-collector"], stack["args"] + ["start", "business-worker", "business-worker-2"]])
        self.assertEqual(wait.call_args_list[0].args[1], {"phase20-collector": "phase20-collector-id"})
        with mock.patch.object(budget, "command", return_value=subprocess.CompletedProcess([], 1, "", "failed")) as command, mock.patch.object(budget, "_b06_wait_targets") as wait, self.assertRaises(budget.Incomplete):
            budget._b06_start_targets(stack, self.target_ids)
        self.assertEqual(command.call_count, 1)
        wait.assert_not_called()

    def test_foreign_initial_target_prevents_signals_and_recovery(self):
        stack = {"project": "owned", "args": ["docker", "compose"], "cleaned": False}
        states = self.states()
        states[0]["Config"]["Labels"]["com.docker.compose.project"] = "foreign"
        outputs = [subprocess.CompletedProcess([], 0, container_id + "\n", "") for container_id in self.target_ids.values()]
        outputs.append(subprocess.CompletedProcess([], 0, json.dumps(states), ""))

        def finish(value):
            value["cleaned"] = True

        with tempfile.TemporaryDirectory() as directory, mock.patch.object(budget, "_prepare_fault_stack", return_value=stack), mock.patch.object(budget, "command", side_effect=outputs) as command, mock.patch.object(budget, "_b06_start_targets") as start, mock.patch.object(budget, "_wait_empty") as waterline, mock.patch.object(budget, "_finish_fault_stack", side_effect=finish):
            with self.assertRaisesRegex(budget.Incomplete, "ownership mismatch"):
                budget.run_b06(Path(directory), Path("manifest"), {}, Path("recipe"), {})
            self.assertEqual(command.call_count, 4)
            self.assertTrue(stack["cleaned"])
            start.assert_not_called()
            waterline.assert_not_called()


if __name__ == "__main__":
    unittest.main()

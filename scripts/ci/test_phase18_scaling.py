import copy
import hashlib
import json
import tempfile
import time
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

import phase18_scaling as phase18_scaling
from phase18_evidence import validate_scaling
from phase18_observers import parse_kafka_consumer_group, parse_mysql_json_object
from phase18_scaling import (
    BACKEND_LOAD,
    BACKEND_SATURATION,
    INDEXER_RESUME_MESSAGES,
    ObservationJournal,
    Project,
    attempt_scenario,
    candidate_binding,
    dump_database,
    metric_values,
    pair_rabbit_backlog,
    prepare_workspace,
    prune_backlog,
    release_rabbit_backlog,
    record_pair_result,
    replace_component_and_observe,
    replace_component_and_observe,
    search_indexer_resume_work_probe,
    wait_rabbit_queue_stats,
    wait_rabbit_stats,
)


def digest(character):
    return "sha256:" + character * 64


def measurement(replicas, processed, elapsed, source, input_digest="b", config="c"):
    return {
        "replicas": replicas,
        "elapsed_seconds": elapsed,
        "processed": processed,
        "counter_before": 100,
        "counter_after": 100 + processed,
        "counter_source": source,
        "input_sha256": digest(input_digest),
        "configuration_sha256": digest(config),
        "report_sha256": digest("d"),
        "raw_samples_sha256": digest("e"),
    }


def pair(operation, replicas, single_processed, single_elapsed, multi_processed,
         multi_elapsed, source):
    single = measurement(1, single_processed, single_elapsed, source)
    multi = measurement(replicas, multi_processed, multi_elapsed, source)
    single_rate = single_processed / single_elapsed
    multi_rate = multi_processed / multi_elapsed
    return {
        "operation": operation,
        "acceptance": "characterization" if operation == "backend_mixed" else "minimum_ratio",
        "threshold": None if operation == "backend_mixed" else 1.3,
        "single": single,
        "multi": multi,
        "rates_per_second": {"single": single_rate, "multi": multi_rate},
        "ratio": multi_rate / single_rate,
    }


def replacement(component, replicas, source):
    instances = ["backend-local", "backend-2", "backend-3"] if replicas == 3 else [component + "-local", component + "-2"]
    removed = instances[-1]
    before = {instance: 10 + index for index, instance in enumerate(instances)}
    during = {instance: value + (20 if instance != removed else 0) for instance, value in before.items()}
    after = {instance: value + (20 if instance != removed else 0) for instance, value in during.items()}
    after[removed] = 3
    before_external = 100
    during_external = before_external + 20
    after_external = before_external + 40
    return {
        "instance": removed,
        "accepted": 40,
        "completed": 40,
        "lost": 0,
        "duplicate_side_effects": 0,
        "counter_source": source,
        "external_counter_before": before_external,
        "external_counter_during_removal": during_external,
        "external_counter_after": after_external,
        "instance_counters_before": before,
        "instance_counters_during_removal": during,
        "instance_counters_after": after,
        "survivor_progress": sum(during[item] - before[item] for item in instances if item != removed),
        "stopped_seconds": 3.0,
        "observations_sha256": digest("f"),
    }


def scaling():
    return {
        "schema": "gopulse.phase18.scaling.v2",
        "execution_status": "complete",
        "complete": True,
        "candidate": {
            "version": "2.0.2", "revision": "a" * 40,
            "manifest_sha256": digest("1"), "bundle_sha256": digest("2"),
            "image_digests": {"backend": digest("3")},
        },
        "host": {
            "platform": "linux/amd64", "host_os": "Linux",
            "kernel": "6.6.0-microsoft-standard-WSL2", "cpu_count": 8,
            "memory_bytes": 12 * 1024 ** 3, "swap_total_bytes": 8 * 1024 ** 3,
            "disk_available_bytes": 80 * 1024 ** 3,
            "docker_server_os": "linux", "docker_server_arch": "amd64",
            "docker_server_version": "29.0", "compose_version": "v5.0",
            "active_compose_projects": [],
        },
        "topology": {
            "replicas": {
                "backend": 3, "business-worker": 2, "search-indexer": 2,
                "router": 2, "marshaller": 2,
            },
            "aliases": {
                "backend": ["backend-local", "backend-2", "backend-3"],
                "router": ["router-local", "router-2"],
            },
            "edge_bindings": [{"service": "frontend", "host_ip": "127.0.0.1", "host_port": 18082}],
        },
        "inputs": {
            "corpus_sha256": digest("a"), "snapshot_sha256": digest("b"),
            "load_source_commit": "a" * 40, "load_binary_sha256": digest("c"),
            "router_load_binary_sha256": digest("e"),
            "recipe_receipt_sha256": digest("d"),
            "execution_order": [
                "backend:replacement", "business-worker:replacement",
                "search-indexer:replacement", "router:replacement", "marshaller:replacement",
                "ownership:runtime", "ownership:tests",
                "backend:single", "backend:multi",
                "business-worker:single", "business-worker:multi",
                "search-indexer:single", "search-indexer:multi",
                "router:single", "router:multi",
                "marshaller:single", "marshaller:multi",
            ],
        },
        "pairs": {
            "backend": {
                **pair("backend_mixed", 3, 100, 10, 140, 10, "load_report"),
                "measurement_mode": "closed_loop",
            },
            "business-worker": pair("business_worker_backlog", 2, 100, 10, 140, 10, "rabbit_ack"),
            "search-indexer": pair("search_indexer_backlog", 2, 100, 10, 140, 10, "rabbit_ack"),
            "router": pair("router_concurrent_publish", 2, 100, 10, 140, 10, "kafka_end_offset"),
            "marshaller": pair("marshaller_backlog", 2, 100, 10, 140, 10, "kafka_committed"),
        },
        "replacements": {
            "backend": replacement("backend", 3, "load_report"),
            "business-worker": replacement("business-worker", 2, "rabbit_ack"),
            "search-indexer": replacement("search-indexer", 2, "rabbit_ack"),
            "router": replacement("router", 2, "kafka_end_offset"),
            "marshaller": replacement("marshaller", 2, "kafka_committed"),
        },
        "edge": {
            "requests": 30, "successful": 30,
            "instance_counts": {"backend-local": 10, "backend-2": 10, "backend-3": 10},
            "removed_instance": "backend-2",
            "counts_after_removal": {"backend-local": 20, "backend-2": 10, "backend-3": 20},
            "counts_after_restart": {"backend-local": 30, "backend-2": 3, "backend-3": 30},
            "successful_during_removal": 20, "successful_after_restart": 3,
            "observations_sha256": digest("4"),
        },
        "ownership": {
            "outbox_lease": {
                "future_owner": "phase18-foreign", "future_lease_until": "2026-09-24T00:00:00Z",
                "blocked_observation": {
                    "owner": "phase18-foreign", "lease_until": "2026-09-24T00:00:00Z",
                    "status": "leased", "updated_at": "2026-09-24T00:00:10Z",
                    "counter_before": 0, "counter_after": 0,
                },
                "reclaimed_observation": {
                    "owner": "backend-local", "lease_until": "2026-09-24T00:00:20Z",
                    "status": "published", "updated_at": "2026-09-24T00:00:20Z",
                    "counter_before": 0, "counter_after": 1,
                },
            },
            "alert_lease": {
                "future_owner": "phase18-foreign", "future_lease_until": "2026-09-24T00:00:00Z",
                "blocked_observation": {
                    "owner": "phase18-foreign", "lease_until": "2026-09-24T00:00:00Z",
                    "status": "leased", "last_evaluated_at": "",
                },
                "reclaimed_observation": {
                    "owner": "", "lease_until": "",
                    "status": "applied", "last_evaluated_at": "2026-09-24T00:00:20Z",
                },
            },
            "rabbit_ack_redelivery": {
                "published": 100, "acknowledged": 100, "redelivery_attempts": 1,
                "duplicate_side_effects": 0, "final_ready": 0, "final_unacknowledged": 0,
                "observations_sha256": digest("9"),
            },
            "kafka_rebalance_fencing": {
                "messages": 100, "committed": 100, "duplicate_commits": 0,
                "old_owner_commits_after_revoke": 0, "final_lag": 0,
                "members_before": ["member-1", "member-2"],
                "members_during_removal": ["member-1"],
                "members_after_restart": ["member-1", "member-3"],
                "observations_sha256": digest("a"),
            },
            "monitor_single_owner": {
                "second_exit_code": 1,
                "registry_sha256_before": digest("5"), "registry_sha256_after": digest("5"),
                "process_record_sha256_before": digest("6"), "process_record_sha256_after": digest("6"),
            },
        },
        "tests": {
            name: {"command": "go test " + name, "exit_code": 0, "output_sha256": digest("7")}
            for name in (
                "outbox_ownership", "alert_ownership", "rabbit_ack_redelivery",
                "kafka_rebalance_fencing", "monitor_single_owner",
            )
        },
        "cleanup": {
            "resource_inventory_before_sha256": digest("8"),
            "resource_inventory_after_sha256": digest("8"),
            "owned_projects_remaining": 0,
            "label_cleanup": "passed",
        },
        "secret_scan": "passed",
    }


class StructuredObserverParserTest(unittest.TestCase):
    HEADER = (
        "GROUP TOPIC PARTITION CURRENT-OFFSET LOG-END-OFFSET LAG "
        "CONSUMER-ID HOST CLIENT-ID"
    )

    def parse(self, rows, *, topic="gopulse-observability-v1", notice=""):
        output = "\n".join(([notice] if notice else []) + [self.HEADER] + rows) + "\n"
        return parse_kafka_consumer_group(
            output, expected_group="marshaller-group", expected_topic=topic,
        )

    def test_mysql_json_keeps_null_and_empty_string_distinct(self):
        keys = {"owner", "lease_until", "status", "updated_at", "published"}
        null_value = parse_mysql_json_object(
            '{"owner":null,"lease_until":null,"status":"published",'
            '"updated_at":"2026-09-25T10:11:12.123456Z","published":1}\n',
            keys, "Outbox lease",
        )
        empty_value = parse_mysql_json_object(
            '{"owner":"","lease_until":"","status":"leased",'
            '"updated_at":"2026-09-25T10:11:12.123456Z","published":0}\n',
            keys, "Outbox lease",
        )
        self.assertIsNone(null_value["owner"])
        self.assertIsNone(null_value["lease_until"])
        self.assertEqual(empty_value["owner"], "")
        self.assertEqual(empty_value["lease_until"], "")
        self.assertEqual(null_value["updated_at"], "2026-09-25T10:11:12.123456Z")

    def test_mysql_json_rejects_missing_rows_and_unexpected_fields(self):
        with self.assertRaisesRegex(RuntimeError, "exactly one JSON row"):
            parse_mysql_json_object("", {"owner"}, "lease")
        with self.assertRaisesRegex(RuntimeError, "unexpected JSON shape"):
            parse_mysql_json_object('{"owner":"x","extra":"y"}', {"owner"}, "lease")

    def test_kafka_parser_accepts_an_empty_group_without_inventing_members(self):
        value = self.parse([])
        self.assertEqual(value["status"], "empty")
        self.assertEqual(value["members"], [])
        self.assertEqual(value["partitions"], {})

    def test_kafka_parser_reads_single_member_and_partition_from_header(self):
        value = self.parse([
            "marshaller-group gopulse-observability-v1 0 10 10 0 member-a /127.0.0.1 client-a",
        ])
        self.assertEqual(value["status"], "active")
        self.assertEqual(value["members"], ["member-a"])
        self.assertEqual(value["partitions"][0]["member"], "member-a")
        self.assertEqual(value["partitions"][0]["lag"], 0)

    def test_kafka_parser_counts_two_members_not_four_partition_rows(self):
        rows = [
            "marshaller-group gopulse-observability-v1 0 1 1 0 member-a /127.0.0.1 client-a",
            "marshaller-group gopulse-observability-v1 1 1 1 0 member-a /127.0.0.1 client-a",
            "marshaller-group gopulse-observability-v1 2 1 1 0 member-b /127.0.0.1 client-b",
            "marshaller-group gopulse-observability-v1 3 1 1 0 member-b /127.0.0.1 client-b",
        ]
        value = self.parse(rows)
        self.assertEqual(value["status"], "active")
        self.assertEqual(value["members"], ["member-a", "member-b"])
        self.assertEqual(len(value["partitions"]), 4)
        self.assertEqual({item["member"] for item in value["partitions"].values()},
                         {"member-a", "member-b"})

    def test_kafka_parser_records_rebalance_and_unassigned_member_separately(self):
        value = self.parse([
            "marshaller-group gopulse-observability-v1 - - - - member-a - client-a",
        ], notice="Consumer group 'marshaller-group' is rebalancing.")
        self.assertEqual(value["status"], "rebalancing")
        self.assertEqual(value["members"], ["member-a"])
        self.assertEqual(value["unassigned_members"], ["member-a"])

    def test_kafka_parser_preserves_uncommitted_offset_as_unknown(self):
        value = self.parse([
            "marshaller-group gopulse-observability-v1 0 - - - member-a /127.0.0.1 client-a",
        ])
        self.assertEqual(value["status"], "active")
        self.assertIsNone(value["partitions"][0]["committed"])
        self.assertIsNone(value["partitions"][0]["log_end"])
        self.assertIsNone(value["lag"])

    def test_kafka_parser_rejects_unsupported_or_misaligned_tables(self):
        with self.assertRaisesRegex(RuntimeError, "header is missing"):
            parse_kafka_consumer_group("arbitrary output\n", expected_group="g")
        with self.assertRaisesRegex(RuntimeError, "row width"):
            self.parse(["marshaller-group gopulse-observability-v1 0 1"])
        with self.assertRaisesRegex(RuntimeError, "requested topic"):
            self.parse(["marshaller-group other-topic 0 1 1 0 member-a /127.0.0.1 client-a"])


class ReadinessProbeTest(unittest.TestCase):
    def _run_probe(self, partition_count, allow_failure=False):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name)
        inputs = {}
        for name in ("snapshot.sql", "corpus.json", "credentials.json", "load"):
            path = root / name
            path.write_text("fixture\n")
            inputs[name] = path

        order = []
        project = mock.Mock()
        project.environment = {"FRONTEND_PORT": "18082"}
        project.up.side_effect = lambda *args, **kwargs: order.append("project_up")
        partition_state = {"count": 1}
        offset_calls = {"count": 0}

        def ensure_partitions(_project, minimum):
            order.append("ensure_partitions")
            partition_state["count"] = partition_count
            return {"topic": "gopulse-observability-v1", "before": 1,
                    "after": partition_count, "required_minimum": minimum}

        def topic_offsets(_project):
            order.append("topic_offsets")
            count = partition_state["count"]
            offset_calls["count"] += 1
            value = 0 if offset_calls["count"] == 1 else 25
            return {index: value for index in range(count)}

        def run_load(command, **_kwargs):
            report_path = Path(command[command.index("--report") + 1])
            report_path.write_text(json.dumps({
                "measurement_mode": "closed_loop",
                "active_workers": BACKEND_SATURATION["active_workers"],
                "steady_target_rps": 0,
                "phases": [
                    {"name": "warmup", "counts": {"succeeded": 1}},
                    {"name": "steady", "counts": {"succeeded": 1}},
                ],
            }) + "\n")
            return SimpleNamespace(returncode=0, stdout="", stderr="")

        patches = (
            mock.patch("phase18_scaling.prepare_project", return_value=project),
            mock.patch("phase18_scaling.readiness_rabbit_lifecycle",
                       side_effect=lambda _manifest, _binding, _work, _snapshot, component:
                       {"component": component, "status": "passed"}),
            mock.patch("phase18_scaling.ensure_kafka_partitions", side_effect=ensure_partitions),
            mock.patch("phase18_scaling.kafka_topic_offsets", side_effect=topic_offsets),
            mock.patch("phase18_scaling.concurrent_router_publish", return_value={
                "accepted": 100, "report_sha256": digest("a"),
            }),
            mock.patch("phase18_scaling.subprocess.run", side_effect=run_load),
        )
        for patcher in patches:
            patcher.start()
            self.addCleanup(patcher.stop)
        project.up.side_effect = lambda *args, **kwargs: order.append("project_up")
        try:
            phase18_scaling.readiness_probe(
                {}, {"revision": "a" * 40}, root, inputs["snapshot.sql"],
                inputs["corpus.json"], inputs["credentials.json"], inputs["load"],
            )
        except RuntimeError as error:
            if not allow_failure:
                raise
            return root, order, error
        return root, order, None

    def test_readiness_expands_default_kafka_topic_before_checking_four_partitions(self):
        root, order, _error = self._run_probe(4)
        self.assertLess(order.index("project_up"), order.index("ensure_partitions"))
        self.assertLess(order.index("ensure_partitions"), order.index("topic_offsets"))
        result = json.loads((root / "evidence/readiness/readiness.json").read_text())
        self.assertEqual(result["kafka_partition_observation"]["before"], 1)
        self.assertEqual(result["kafka_partition_observation"]["after"], 4)
        self.assertEqual(result["backend_saturation"]["measurement_mode"], "closed_loop")
        self.assertEqual(
            result["backend_saturation"]["active_workers"],
            BACKEND_SATURATION["active_workers"],
        )
        journal = json.loads((root / "evidence/readiness/readiness-observations.json").read_text())
        self.assertEqual(journal["status"], "passed")

    def test_readiness_failure_keeps_partition_and_offset_observations(self):
        root, _order, error = self._run_probe(1, allow_failure=True)
        self.assertRegex(str(error), "does not have four partitions")
        journal = json.loads((root / "evidence/readiness/readiness-observations.json").read_text())
        self.assertEqual(journal["status"], "failed")
        self.assertEqual(journal["partition_observation"]["after"], 1)
        self.assertEqual(journal["before_offsets"], {"0": 0})



class ScalingEvidenceTest(unittest.TestCase):
    def test_backend_pair_uses_the_phase18_capacity_load_configuration(self):
        self.assertEqual(
            BACKEND_LOAD,
            {
                "virtual_users": 1024,
                "warmup_seconds": 300,
                "steady_seconds": 900,
                "burst_seconds": 120,
                "steady_rps": 150,
                "burst_rps": 300,
            },
        )
        self.assertEqual(
            BACKEND_SATURATION,
            {
                "virtual_users": 1024,
                "active_workers": 128,
                "warmup_seconds": 15,
                "steady_seconds": 90,
                "request_timeout_seconds": 5,
                "mode": "closed_loop",
            },
        )

    def test_backend_pair_is_recorded_as_non_blocking_characterization(self):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            item = scaling()["pairs"]["backend"]
            item["ratio"] = 0.99
            record_pair_result(work, {"revision": "a" * 40}, "backend", item)
            recorded = (work / "evidence" / "backend-pair.json").read_text()
            self.assertIn('"blocking": false', recorded)
            self.assertIn('"status": "characterized"', recorded)

    def test_async_pair_gate_records_failure_before_stopping_the_scenario(self):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            item = scaling()["pairs"]["router"]
            item["ratio"] = 1.29
            with self.assertRaisesRegex(RuntimeError, "below the 1.3x"):
                record_pair_result(work, {"revision": "a" * 40}, "router", item)
            recorded = (work / "evidence" / "router-pair.json").read_text()
            self.assertIn('"blocking": true', recorded)
            self.assertIn('"status": "failed"', recorded)

    def test_scenario_failure_does_not_prevent_an_independent_scenario(self):
        results = {}
        first = attempt_scenario(
            results, "first", lambda: (_ for _ in ()).throw(RuntimeError("broken"))
        )
        second = attempt_scenario(results, "second", lambda: "completed")
        self.assertIsNone(first)
        self.assertEqual(second, "completed")
        self.assertEqual(results["first"]["status"], "failed")
        self.assertEqual(results["second"]["status"], "passed")

    @mock.patch("phase18_scaling.container_state_snapshot", return_value={
        "search-indexer": {"status": "observed", "containers": [{"state": "running"}]},
        "search-indexer-2": {"status": "observed", "containers": [{"state": "running"}]},
    })
    @mock.patch("phase18_scaling.time.sleep")
    @mock.patch("phase18_scaling.component_counter")
    def test_replacement_requires_live_work_before_during_and_after_restart(
        self, counters, _sleep, _container_states,
    ):
        project = mock.Mock()
        project.replicas = 2
        counters.side_effect = [
            (0, {"search-indexer": 0, "search-indexer-2": 0}),
            (2, {"search-indexer": 1, "search-indexer-2": 1}),
            (1, {"search-indexer": 1}),
            (2, {"search-indexer": 2}),
            (3, {"search-indexer": 2, "search-indexer-2": 1}),
        ]
        activity = mock.Mock(side_effect=[
            {"status": "observed", "counter": 0, "remaining_work": 5},
            {"status": "observed", "counter": 0, "remaining_work": 5},
            {"status": "observed", "counter": 1, "remaining_work": 4},
            {"status": "observed", "counter": 2, "remaining_work": 3},
            {"status": "observed", "counter": 3, "remaining_work": 2},
        ])
        with tempfile.TemporaryDirectory() as directory:
            journal = ObservationJournal(Path(directory) / "replacement.json", {"component": "search-indexer"})
            result = replace_component_and_observe(
                project, "search-indexer", "search-indexer-2",
                activity_probe=activity, journal=journal,
            )
            self.assertEqual(result["survivor_progress"], 1)
            self.assertGreater(result["per_instance_after"]["search-indexer-2"], 0)
            self.assertGreater(result["activity_during_removal"]["remaining_work"], 0)
            self.assertEqual(journal.document["status"], "replacement_complete")
        self.assertEqual(activity.call_args_list, [
            mock.call(("search-indexer", "search-indexer-2")),
            mock.call(("search-indexer", "search-indexer-2")),
            mock.call(("search-indexer",)),
            mock.call(("search-indexer",)),
            mock.call(("search-indexer", "search-indexer-2")),
        ])
        project.require.assert_called_once_with("stop", "search-indexer-2", timeout=120)
        project.up.assert_called_once_with("search-indexer-2", timeout=600, dependencies=False)

    @mock.patch("phase18_scaling.container_state_snapshot", return_value={})
    @mock.patch("phase18_scaling.time.sleep")
    @mock.patch("phase18_scaling.component_counter")
    def test_replacement_can_release_fixed_work_after_a_pre_release_baseline(
        self, counters, _sleep, _container_states,
    ):
        project = mock.Mock()
        project.replicas = 2
        counters.side_effect = [
            (2, {"business-worker": 1, "business-worker-2": 1}),
            (1, {"business-worker": 1}),
            (2, {"business-worker": 2}),
            (3, {"business-worker": 2, "business-worker-2": 1}),
        ]
        release = mock.Mock()
        activity_samples = [
            {"status": "observed", "counter": 0, "remaining_work": 5000},
            {"status": "observed", "counter": 2, "remaining_work": 4998},
            {"status": "observed", "counter": 2, "remaining_work": 4998},
            {"status": "observed", "counter": 3, "remaining_work": 4997},
            {"status": "observed", "counter": 4, "remaining_work": 4996},
        ]

        def observe_after_release(services):
            self.assertEqual(release.call_count, 1)
            return activity_samples.pop(0)

        with tempfile.TemporaryDirectory() as directory:
            journal = ObservationJournal(Path(directory) / "replacement.json", {"component": "business-worker"})
            result = replace_component_and_observe(
                project, "business-worker", "business-worker-2",
                activity_probe=observe_after_release, journal=journal,
                initial_progress={"business-worker": 0, "business-worker-2": 0},
                release_work=release,
            )

        self.assertEqual(result["survivor_progress"], 1)
        self.assertEqual(journal.document["status"], "replacement_complete")
        release.assert_called_once_with()
        self.assertEqual(counters.call_count, 4)

    @mock.patch("phase18_scaling.container_state_snapshot", return_value={})
    @mock.patch("phase18_scaling.time.sleep")
    @mock.patch("phase18_scaling.component_counter")
    def test_replacement_exhaustion_before_progress_persists_failure_samples(
        self, counters, _sleep, _container_states,
    ):
        project = mock.Mock()
        project.replicas = 2
        counters.side_effect = [
            (0, {"business-worker": 0, "business-worker-2": 0}),
            (0, {"business-worker": 0, "business-worker-2": 0}),
        ]
        activity = mock.Mock(side_effect=[
            {"status": "observed", "counter": 0, "remaining_work": 1},
            {"status": "observed", "counter": 0, "remaining_work": 0},
        ])
        with tempfile.TemporaryDirectory() as directory:
            journal = ObservationJournal(Path(directory) / "replacement.json", {"component": "business-worker"})
            with self.assertRaisesRegex(RuntimeError, "replacement_work_exhausted_before_target_progress"):
                replace_component_and_observe(
                    project, "business-worker", "business-worker-2",
                    activity_probe=activity, journal=journal,
                )
            self.assertEqual(journal.document["reason_code"], "replacement_work_exhausted_before_target_progress")
            self.assertEqual(len(journal.document["before_samples"]), 1)
            self.assertEqual(journal.document["component_counters_last"]["total"], 0)
        project.require.assert_not_called()

    @mock.patch("phase18_scaling.time.sleep")
    @mock.patch("phase18_scaling.component_counter")
    def test_replacement_treats_exhausted_work_as_fixture_failure(self, counters, _sleep):
        project = mock.Mock()
        project.replicas = 2
        counters.side_effect = [
            (0, {"business-worker": 0, "business-worker-2": 0}),
            (2, {"business-worker": 1, "business-worker-2": 1}),
            (1, {"business-worker": 1}),
        ]
        activity = mock.Mock(side_effect=[
            {"status": "observed", "counter": 0, "remaining_work": 2},
            {"status": "observed", "counter": 1, "remaining_work": 2},
            {"status": "observed", "counter": 2, "remaining_work": 0},
        ])
        with tempfile.TemporaryDirectory() as directory:
            journal = ObservationJournal(Path(directory) / "replacement.json", {"component": "business-worker"})
            with mock.patch("phase18_scaling.container_state_snapshot", return_value={}):
                with self.assertRaisesRegex(RuntimeError, "replacement_work_exhausted_during_removal"):
                    replace_component_and_observe(
                        project, "business-worker", "business-worker-2",
                        activity_probe=activity, journal=journal,
                    )
            self.assertEqual(journal.document["status"], "failed")
            self.assertEqual(journal.document["reason_code"], "replacement_work_exhausted_during_removal")
        project.up.assert_not_called()

    def test_failed_workspace_cannot_be_reused_for_a_second_acceptance_run(self):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            work.mkdir(mode=0o700, exist_ok=True)
            (work / "run-error.txt").write_text("RuntimeError: failed\n")
            with self.assertRaisesRegex(ValueError, "previous attempt"):
                prepare_workspace(work, {"version": "2.0.2", "revision": "a" * 40})

    @mock.patch("phase18_scaling.verify_bundle", return_value={
        "version": "2.0.2",
        "revision": "a" * 40,
        "bundle_sha256": digest("2"),
        "images": {},
    })
    @mock.patch("phase18_scaling.run")
    def test_candidate_binding_rejects_untracked_source(self, run_command, _verify_bundle):
        run_command.side_effect = [
            SimpleNamespace(stdout="a" * 40),
            SimpleNamespace(stdout="?? backend/internal/searchlock/lock.go\n"),
        ]
        with self.assertRaisesRegex(ValueError, "committed source tree"):
            candidate_binding(Path("unused-release-manifest.json"))
        self.assertEqual(
            run_command.call_args_list[1].args[0][-2:],
            ["status", "--porcelain"],
        )

    def test_valid_document(self):
        validate_scaling(scaling())

    def test_scaling_rejects_disk_below_80_gib(self):
        document = scaling()
        document["host"]["disk_available_bytes"] = 80 * 1024 ** 3 - 1
        with self.assertRaisesRegex(ValueError, "resource contract"):
            validate_scaling(document)

    def test_backend_pair_rejects_a_fixed_rate_measurement(self):
        document = scaling()
        document["pairs"]["backend"]["measurement_mode"] = "fixed_rate"
        with self.assertRaisesRegex(ValueError, "uncapped closed-loop"):
            validate_scaling(document)

    def test_backend_pair_ratio_is_non_blocking_characterization(self):
        document = scaling()
        item = document["pairs"]["backend"]
        item["multi"]["processed"] = 90
        item["multi"]["counter_after"] = 190
        item["rates_per_second"]["multi"] = 9.0
        item["ratio"] = 0.9
        validate_scaling(document)

    def test_backend_characterization_rejects_a_threshold(self):
        document = scaling()
        document["pairs"]["backend"]["threshold"] = 1.0
        with self.assertRaisesRegex(ValueError, "must not declare a threshold"):
            validate_scaling(document)

    def test_mysql_dump_uses_the_application_user(self):
        class Project:
            def __init__(self):
                self.calls = []

            def require(self, *args, **kwargs):
                self.calls.append((args, kwargs))
                kwargs["output_path"].write_text("dump")

        with tempfile.TemporaryDirectory() as directory:
            project = Project()
            output = Path(directory) / "initial.sql"
            dump_database(project, output)
            command = project.calls[0][0][-1]
            self.assertIn('mysqldump -u"$MYSQL_USER"', command)

    @mock.patch("phase18_scaling.wait_outbox_empty")
    @mock.patch("phase18_scaling.mysql_exec")
    @mock.patch("phase18_scaling.mysql")
    def test_rabbit_backlog_stages_fixed_events_before_releasing_outbox(self, mysql, mysql_exec, wait_empty):
        rows = "1 event-1 comment.created\n2 event-2 post.liked\n"
        mysql.side_effect = ["0", rows, "2"]
        with mock.patch("phase18_scaling.BACKLOG_LIMIT", 2):
            count, selected_digest = prune_backlog(object(), "business-worker")

        self.assertEqual(count, 2)
        self.assertEqual(selected_digest, "sha256:" + hashlib.sha256(rows.strip().encode()).hexdigest())
        wait_empty.assert_called_once()
        sql = [call.args[1] for call in mysql_exec.call_args_list]
        self.assertTrue(any("DELETE FROM notifications" in statement for statement in sql))
        stage = next(statement for statement in sql if "DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 10 MINUTE)" in statement)
        release = next(statement for statement in sql if "SET available_at=UTC_TIMESTAMP(6)" in statement)
        self.assertIn("published_at=NULL", stage)
        self.assertIn("lease_owner=NULL", stage)
        self.assertIn("id IN (1,2)", stage)
        self.assertIn("id IN (1,2)", release)
        self.assertEqual(mysql.call_count, 3)

    @mock.patch("phase18_scaling.wait_outbox_empty")
    @mock.patch("phase18_scaling.mysql_exec")
    @mock.patch("phase18_scaling.mysql")
    def test_search_indexer_backlog_counter_is_read_before_outbox_release(
        self, mysql, mysql_exec, wait_empty
    ):
        rows = "10 event-10 post.created\n11 event-11 post.updated\n"
        mysql.side_effect = ["0", rows, "2"]
        with mock.patch("phase18_scaling.BACKLOG_LIMIT", 2):
            count, selected_digest = prune_backlog(object(), "search-indexer")

        self.assertEqual(count, 2)
        self.assertEqual(selected_digest, "sha256:" + hashlib.sha256(rows.strip().encode()).hexdigest())
        wait_empty.assert_called_once()
        sql = [call.args[1] for call in mysql_exec.call_args_list]
        self.assertFalse(any("DELETE FROM notifications" in statement for statement in sql))
        stage_index = next(i for i, statement in enumerate(sql) if "DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 10 MINUTE)" in statement)
        release_index = next(i for i, statement in enumerate(sql) if "SET available_at=UTC_TIMESTAMP(6)" in statement)
        self.assertLess(stage_index, release_index)
        self.assertIn("id IN (10,11)", sql[stage_index])
        self.assertIn("id IN (10,11)", sql[release_index])
        self.assertEqual(mysql.call_count, 3)

    @mock.patch("phase18_scaling.wait_outbox_empty")
    @mock.patch("phase18_scaling.mysql_exec")
    @mock.patch("phase18_scaling.mysql")
    def test_rabbit_backlog_rejects_a_seed_with_too_few_replayable_events(self, mysql, mysql_exec, _wait_empty):
        mysql.side_effect = ["0", "1 event-1 comment.created\n"]
        with mock.patch("phase18_scaling.BACKLOG_LIMIT", 2):
            with self.assertRaisesRegex(RuntimeError, "fixed published backlog"):
                prune_backlog(object(), "business-worker")
        changed = [call.args[1] for call in mysql_exec.call_args_list]
        self.assertFalse(any("UPDATE business_outbox" in statement for statement in changed))
        self.assertFalse(any("DELETE FROM notifications" in statement for statement in changed))

    @mock.patch("phase18_scaling.rabbit_queue_stats")
    @mock.patch("phase18_scaling.component_counter")
    @mock.patch("phase18_scaling.wait_search_documents")
    @mock.patch("phase18_scaling.outbox_rows_for_ids")
    @mock.patch("phase18_scaling.publish_fresh_posts")
    def test_search_indexer_resume_uses_fresh_events_and_proves_index_side_effects(
        self, publish, rows_for_ids, wait_documents, counters, queues
    ):
        project = mock.Mock()
        publish.return_value = {"post_ids": list(range(100, 100 + INDEXER_RESUME_MESSAGES)),
                                "comment_ids": [], "marker_sha256": digest("f")}
        rows_for_ids.return_value = [
            {"id": index, "event_id": "fresh-event-%d" % index,
             "event_type": "post.created", "status": "published"}
            for index in range(INDEXER_RESUME_MESSAGES)
        ]
        counters.side_effect = [
            (0, {"search-indexer-2": 0}),
            (INDEXER_RESUME_MESSAGES, {"search-indexer-2": INDEXER_RESUME_MESSAGES}),
        ]
        queues.side_effect = [
            {"ready": 0, "unacknowledged": 0, "ack": 5, "redeliver_get": 0},
            {"ready": 0, "unacknowledged": 0, "ack": 5 + INDEXER_RESUME_MESSAGES, "redeliver_get": 0},
            {"ready": 0, "unacknowledged": 0, "ack": 5 + INDEXER_RESUME_MESSAGES, "redeliver_get": 0},
        ]

        result = search_indexer_resume_work_probe(
            project, "search-indexer-2", Path("credentials.json"),
        )

        self.assertEqual(result["replacement_counter_after"], INDEXER_RESUME_MESSAGES)
        self.assertEqual(result["indexed_documents"], INDEXER_RESUME_MESSAGES)
        self.assertEqual(result["published_outbox_messages"], INDEXER_RESUME_MESSAGES)
        publish.assert_called_once_with(project, Path("credentials.json"), INDEXER_RESUME_MESSAGES)
        rows_for_ids.assert_called_once_with(
            project, "post.created", "post_id", list(range(100, 100 + INDEXER_RESUME_MESSAGES)),
        )
        self.assertEqual(result["peer_service"], "search-indexer")
        self.assertEqual(
            [call.args[:2] for call in project.require.call_args_list],
            [("pause", "search-indexer"), ("unpause", "search-indexer")],
        )
        self.assertEqual(wait_documents.call_count, 2)

    @mock.patch("phase18_scaling.publish_fresh_posts", side_effect=RuntimeError("edge unavailable"))
    @mock.patch("phase18_scaling.component_counter", return_value=(0, {"search-indexer-2": 0}))
    @mock.patch("phase18_scaling.rabbit_queue_stats", return_value={
        "ready": 1, "unacknowledged": 0, "ack": 0, "redeliver_get": 0,
    })
    def test_search_indexer_resume_probe_unpauses_peer_after_failure(
        self, _queue_stats, _component_counter, _publish
    ):
        project = mock.Mock()

        with self.assertRaisesRegex(RuntimeError, "edge unavailable"):
            search_indexer_resume_work_probe(project, "search-indexer-2", Path("credentials.json"))

        self.assertEqual(project.require.call_count, 2)
        self.assertEqual(project.require.call_args_list[-1].args[:2], ("unpause", "search-indexer"))

    @mock.patch("phase18_scaling.time.sleep")
    @mock.patch("phase18_scaling.rabbit_queue_stats")
    def test_rabbit_queue_count_waits_for_management_statistics_to_catch_up(self, queue_stats, _sleep):
        queue_stats.side_effect = [
            {"ready": 4960, "unacknowledged": 0, "ack": 0, "redeliver_get": 0},
            {"ready": 5000, "unacknowledged": 0, "ack": 0, "redeliver_get": 0},
        ]
        stats, convergence = wait_rabbit_queue_stats(object(), "gopulse.business-worker.v1", 5000)
        self.assertEqual(stats["ready"], 5000)
        self.assertEqual(len(convergence["samples"]), 2)
        self.assertEqual(queue_stats.call_count, 2)

    @mock.patch("phase18_scaling.rabbit_queue_stats", return_value={
        "ready": 5001, "unacknowledged": 0, "ack": 0, "redeliver_get": 0,
    })
    def test_rabbit_queue_count_rejects_messages_beyond_the_fixed_backlog(self, _queue_stats):
        with self.assertRaisesRegex(RuntimeError, "more messages than the fixed backlog"):
            wait_rabbit_queue_stats(object(), "gopulse.business-worker.v1", 5000)

    @mock.patch("phase18_scaling.rabbit_queue_stats", return_value={
        "ready": 0, "unacknowledged": 0, "ack": 5000, "redeliver_get": 0,
    })
    def test_rabbit_drain_wait_uses_a_start_time_before_health_wait(self, _queue_stats):
        started_at = time.monotonic() - 1.0

        elapsed, samples = wait_rabbit_stats(
            object(), "gopulse.business-worker.v1", started_at=started_at,
        )

        self.assertGreaterEqual(elapsed, 1.0)
        self.assertGreaterEqual(samples[0]["at"], 1.0)

    def test_rabbit_release_timer_starts_after_readiness_and_fixed_backlog(self):
        timeline = []
        clock_values = iter((10.0, 12.0, 20.0, 20.1, 20.4))

        def monotonic():
            value = next(clock_values)
            timeline.append(("timer", value))
            return value

        project = mock.Mock()
        project.up.side_effect = lambda *args, **kwargs: timeline.append(("up", *args))
        project.require.side_effect = lambda *args, **kwargs: timeline.append((args[0], *args[1:]))
        journal = mock.Mock()
        journal.update.side_effect = lambda **kwargs: timeline.append(("journal", kwargs["status"]))
        sampler = mock.Mock()
        sampler.start.side_effect = lambda: timeline.append(("sampler", "start"))
        fake_backlog = (
            "gopulse.business-worker.v1", 5000,
            {"ready": 5000, "unacknowledged": 0, "ack": 0, "redeliver_get": 0},
            digest("a"), {"elapsed_seconds": 0.5, "samples": []},
        )

        def capture_counters(*args):
            timeline.append(("counters", *args))
            return 0, {"business-worker": 0}

        with (
            mock.patch("phase18_scaling.time.monotonic", side_effect=monotonic),
            mock.patch("phase18_scaling.prepare_rabbit_backlog", return_value=fake_backlog) as prepare,
            mock.patch("phase18_scaling.component_counter", side_effect=capture_counters),
            mock.patch("phase18_scaling.wait_outbox_empty", side_effect=lambda _project: None),
        ):
            released = release_rabbit_backlog(
                project, "business-worker", ("business-worker",), sampler, journal,
            )

        self.assertEqual(released[5], 2.0)
        self.assertEqual(released[-1], 20.0)
        prepare.assert_called_once_with(project, "business-worker")
        self.assertLess(timeline.index(("up", "business-worker")),
                        timeline.index(("counters", project, "business-worker", ("business-worker",))))
        self.assertLess(timeline.index(("counters", project, "business-worker", ("business-worker",))),
                        timeline.index(("pause", "business-worker")))
        self.assertLess(timeline.index(("pause", "business-worker")),
                        timeline.index(("journal", "fixed_backlog_confirmed")))
        self.assertLess(timeline.index(("journal", "fixed_backlog_confirmed")),
                        timeline.index(("sampler", "start")))
        self.assertLess(timeline.index(("sampler", "start")),
                        timeline.index(("unpause", "business-worker")))

    def test_private_metrics_retry_a_transient_unavailable_snapshot(self):
        class Project:
            def __init__(self):
                self.calls = 0

            def compose(self, *args, **kwargs):
                self.calls += 1
                if self.calls == 1:
                    return SimpleNamespace(returncode=8, stdout="", stderr="503 Service Unavailable")
                return SimpleNamespace(returncode=0, stdout='instance="backend-local" 1\n', stderr="")

        project = Project()
        with mock.patch("phase18_scaling.time.sleep"):
            values = metric_values(project, "backend", ("backend",))
        self.assertEqual(values, {"backend": 'instance="backend-local" 1\n'})
        self.assertEqual(project.calls, 2)

    def test_replacement_restart_does_not_replay_one_shot_dependencies(self):
        project = object.__new__(Project)
        project.require = mock.Mock()
        project.up("backend-2", dependencies=False)
        project.require.assert_called_once_with(
            "up", "-d", "--no-deps", "--wait", "--wait-timeout", "900", "backend-2", timeout=1200,
        )

    def test_ratio_is_recomputed(self):
        document = scaling()
        document["pairs"]["backend"]["multi"]["counter_after"] += 1
        with self.assertRaisesRegex(ValueError, "counter delta"):
            validate_scaling(document)

        document = scaling()
        document["pairs"]["backend"]["ratio"] = 1.5
        with self.assertRaisesRegex(ValueError, "recomputed"):
            validate_scaling(document)

    def test_pair_inputs_and_counter_source_must_match(self):
        document = scaling()
        document["pairs"]["router"]["multi"]["configuration_sha256"] = digest("9")
        with self.assertRaisesRegex(ValueError, "same configuration_sha256"):
            validate_scaling(document)

        document = scaling()
        document["pairs"]["router"]["multi"]["counter_source"] = "component_metric"
        with self.assertRaisesRegex(ValueError, "external counter source"):
            validate_scaling(document)

    def test_replacement_loss_or_missing_progress_rejected(self):
        for key in ("lost", "duplicate_side_effects"):
            document = scaling()
            document["replacements"]["marshaller"][key] = 1
            with self.assertRaises(ValueError):
                validate_scaling(document)

        document = scaling()
        document["replacements"]["marshaller"]["survivor_progress"] = 0
        document["replacements"]["marshaller"]["instance_counters_during_removal"]["marshaller-local"] = 10
        with self.assertRaises(ValueError):
            validate_scaling(document)

    def test_rabbit_requires_observed_redelivery(self):
        document = scaling()
        document["ownership"]["rabbit_ack_redelivery"]["redelivery_attempts"] = 0
        with self.assertRaisesRegex(ValueError, "Rabbit ack/redelivery"):
            validate_scaling(document)

    def test_monitor_conflict_must_preserve_state(self):
        document = scaling()
        document["ownership"]["monitor_single_owner"]["registry_sha256_after"] = digest("9")
        with self.assertRaisesRegex(ValueError, "mutated"):
            validate_scaling(document)

    def test_raw_measurement_and_cleanup_required(self):
        document = copy.deepcopy(scaling())
        del document["pairs"]["backend"]["single"]["raw_samples_sha256"]
        with self.assertRaisesRegex(ValueError, "incomplete"):
            validate_scaling(document)

        document = scaling()
        document["cleanup"]["resource_inventory_after_sha256"] = digest("9")
        with self.assertRaisesRegex(ValueError, "cleanup"):
            validate_scaling(document)


if __name__ == "__main__":
    unittest.main()

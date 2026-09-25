import copy
import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from phase18_evidence import (
    BOTTLENECK_SCHEMA,
    QUALIFICATION_PARSER_CASES,
    QUALIFICATION_SCHEMA,
    validate_bottleneck_diagnostic,
    validate_qualification,
)
from phase18_qualification import parser_fixture_suite


def digest(character):
    return "sha256:" + character * 64


def attachment(path):
    return {"path": path, "sha256": digest("a"), "bytes": 1}


def candidate():
    return {
        "version": "2.0.2", "revision": "b" * 40,
        "manifest_sha256": digest("c"), "bundle_sha256": digest("d"),
        "image_digests": {"backend": digest("e")},
        "plugin_digests": {"redis-exporter:2.0.2:amd64": digest("f")},
    }


def staircase():
    return {
        "status": "passed",
        "steps": [
            {"concurrency": value, "accepted": 10, "elapsed_seconds": 1.0}
            for value in (1, 8, 16, 32, 64)
        ],
        "observations_sha256": digest("1"),
        "resource_samples_sha256": digest("2"),
    }


def replacement():
    return {
        "status": "passed", "target_progress_before": 1,
        "survivor_progress_during": 1, "replacement_progress_after": 1,
        "work_retained_samples": 1,
        "observations_sha256": digest("3"), "resource_samples_sha256": digest("4"),
    }


def qualification(work):
    candidate_value = candidate()
    diagnostic = {
        "schema": BOTTLENECK_SCHEMA, "candidate": candidate_value,
        "finding": "inconclusive", "reason_code": "fixture",
        "summary": "fixture supports no isolated bottleneck conclusion",
        "rates_per_second": {"router": 1.0},
        "evidence": [{"source": "fixture.jsonl", "sha256": digest("a")}],
    }
    diagnostic_path = work / "bottleneck.json"
    diagnostic_path.write_text(json.dumps(diagnostic) + "\n")
    resource_path = work / "resource.jsonl"
    resource_path.write_text("{}\n")
    diagnostic_attachment = {
        "path": "bottleneck.json", "sha256": digest("5"),
        "bytes": diagnostic_path.stat().st_size, "finding": "inconclusive",
    }
    diagnostic_attachment["sha256"] = "sha256:" + __import__("hashlib").sha256(diagnostic_path.read_bytes()).hexdigest()
    resource_attachment = {
        "path": "resource.jsonl", "sha256": "sha256:" + __import__("hashlib").sha256(resource_path.read_bytes()).hexdigest(),
        "bytes": resource_path.stat().st_size,
    }
    paired_side = {
        "cold_start_seconds": 1.0, "measurement_seconds": 2.0,
        "samples": 3, "observations_sha256": digest("6"),
        "resource_samples_sha256": digest("7"),
    }
    marshaller_side = {
        "status": "passed", "fixed_messages": 10000,
        "measurement_seconds": 30.0, "measurement_samples": 10,
        "member_count": 1, "partition_count": 4,
        "members_owning_partitions": 1,
        "observations_sha256": digest("8"), "resource_samples_sha256": digest("9"),
    }
    multi_marshaller = dict(marshaller_side)
    multi_marshaller.update({
        "member_count": 2, "members_owning_partitions": 2,
        "observations_sha256": digest("a"), "resource_samples_sha256": digest("b"),
    })
    fixture_cases = parser_fixture_suite()
    return {
        "schema": QUALIFICATION_SCHEMA, "status": "qualified",
        "started_at": 1.0, "finished_at": 2.0,
        "candidate": candidate_value,
        "host": {"platform": "linux/amd64"}, "topology": {},
        "inputs": {
            "snapshot_sha256": digest("c"), "corpus_sha256": digest("d"),
            "recipe_receipt_sha256": digest("e"), "recipe_binary_sha256": digest("f"),
            "load_binary_sha256": digest("1"), "router_publisher_binary_sha256": digest("2"),
            "workload_recipe_sha256": digest("3"),
            "fixed_backlog": {"rabbit_messages": 5000, "prefetch": 10, "release_barrier": "fixture"},
            "execution_order": ["worker", "indexer", "router", "marshaller"],
        },
        "script_digests": {"scripts/verify-phase18-scaling.sh": digest("4")},
        "parser_fixtures": {
            "mysql": fixture_cases["mysql"], "kafka": fixture_cases["kafka"],
            "live_mysql": {"status": "passed", "observations_sha256": digest("5")},
            "live_kafka": {"status": "passed", "observations_sha256": digest("6")},
        },
        "ownership": {
            "status": "passed", "observations_sha256": digest("7"),
            "outbox_future_lease": {"owner": "phase18-foreign", "status": "leased"},
            "outbox_expired_reclaim": {"status": "published", "published": 1},
            "alert_future_lease": {"owner": "phase18-foreign", "last_evaluated_at": None},
            "alert_expired_reclaim": {"status": "applied"},
            "monitor_single_owner": {
                "second_exit_code": 1,
                "registry_sha256_before": digest("8"), "registry_sha256_after": digest("8"),
                "process_record_sha256_before": digest("9"), "process_record_sha256_after": digest("9"),
            },
        },
        "paired_workloads": {
            component: {
                "status": "passed", "single_input_sha256": digest("a"),
                "multi_input_sha256": digest("a"), "single": dict(paired_side),
                "multi": dict(paired_side),
            }
            for component in ("business-worker", "search-indexer")
        },
        "marshaller": {
            "single": marshaller_side, "multi": multi_marshaller,
            "fixed_messages": 10000,
            "calibration": {"observations_sha256": digest("c")},
        },
        "router": {
            "single_staircase": staircase(), "multi_staircase": staircase(),
            "kafka_producer_ceiling": {
                "status": "observed", "records_per_second": 50000.0,
                "observations_sha256": digest("d"),
            },
        },
        "replacements": {key: replacement() for key in (
            "backend", "business-worker", "search-indexer", "router", "marshaller",
        )},
        "resources": {
            "status": "passed", "sample_count": 1,
            "host_observed": True, "containers_observed": True,
            "dependencies": {key: "observed" for key in (
                "mysql", "rabbitmq", "kafka", "elasticsearch", "victoriametrics",
            )},
            "attachments": [resource_attachment],
        },
        "bottleneck_diagnostic": diagnostic_attachment,
        "cleanup": {
            "status": "passed", "owned_projects_remaining": 0,
            "resource_inventory_before_sha256": digest("e"),
            "resource_inventory_after_sha256": digest("f"),
        },
        "secret_scan": "passed",
        "attachments": [diagnostic_attachment, resource_attachment],
    }


class QualificationFixtureTest(unittest.TestCase):
    def test_structured_mysql_and_kafka_fixture_catalog_is_complete(self):
        result = parser_fixture_suite()
        self.assertEqual(set(result["mysql"]), QUALIFICATION_PARSER_CASES["mysql"])
        self.assertEqual(set(result["kafka"]), QUALIFICATION_PARSER_CASES["kafka"])
        self.assertTrue(all(status == "passed" for family in result.values() for status in family.values()))

    def test_parser_failure_persists_fixture_input_before_invoking_parser(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "parser-fixtures.json"
            with mock.patch(
                "phase18_qualification.runner.parse_mysql_json_object",
                side_effect=RuntimeError("fixture rejection"),
            ):
                with self.assertRaisesRegex(RuntimeError, "fixture rejection"):
                    parser_fixture_suite(path)
            evidence = json.loads(path.read_text())
            self.assertEqual(evidence["mysql"]["future_lease"]["status"], "observed")
            self.assertIn(
                '"owner":"phase18-fixture"',
                evidence["mysql"]["future_lease"]["raw"]["input"],
            )

    def test_qualification_accepts_candidate_bound_receipt_and_attachments(self):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            value = qualification(work)
            validate_qualification(value, work=work)

    def test_qualification_rejects_short_marshaller_window(self):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            value = qualification(work)
            value["marshaller"]["multi"]["measurement_seconds"] = 29.9
            with self.assertRaisesRegex(ValueError, "shorter than 30 seconds"):
                validate_qualification(value, work=work)

    def test_qualification_rejects_missing_dual_marshaller_partition_owner(self):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            value = qualification(work)
            value["marshaller"]["multi"]["members_owning_partitions"] = 1
            with self.assertRaisesRegex(ValueError, "both Marshaller members"):
                validate_qualification(value, work=work)

    def test_qualification_rejects_replacement_without_retained_work(self):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            value = qualification(work)
            value["replacements"]["search-indexer"]["work_retained_samples"] = 0
            with self.assertRaisesRegex(ValueError, "lacks live-work evidence"):
                validate_qualification(value, work=work)

    def test_bottleneck_diagnostic_rejects_product_acceptance_category(self):
        diagnostic = {
            "schema": BOTTLENECK_SCHEMA, "candidate": candidate(),
            "finding": "product_slo", "reason_code": "bad", "summary": "bad",
            "evidence": [{"source": "x", "sha256": digest("a")}],
        }
        with self.assertRaisesRegex(ValueError, "finding is invalid"):
            validate_bottleneck_diagnostic(diagnostic)

    def test_qualification_rechecks_the_diagnostic_candidate(self):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            value = qualification(work)
            diagnostic_path = work / "bottleneck.json"
            diagnostic = json.loads(diagnostic_path.read_text())
            diagnostic["candidate"]["revision"] = "1" * 40
            diagnostic_path.write_text(json.dumps(diagnostic) + "\n")
            value["bottleneck_diagnostic"]["sha256"] = "sha256:" + __import__("hashlib").sha256(diagnostic_path.read_bytes()).hexdigest()
            value["bottleneck_diagnostic"]["bytes"] = diagnostic_path.stat().st_size
            value["attachments"][0] = dict(value["bottleneck_diagnostic"])
            with self.assertRaisesRegex(ValueError, "another candidate revision"):
                validate_qualification(value, work=work)


if __name__ == "__main__":
    unittest.main()

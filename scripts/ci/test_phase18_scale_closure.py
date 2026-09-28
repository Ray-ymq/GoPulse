"""Self-tests for the fixed Phase 18-05 closure runner."""

from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from phase18_scale_closure import (
    COMPONENT_METRICS_PATH,
    MATRIX_ORDER,
    average,
    capacity_metrics_path,
    final_result,
    _diagnostic_services,
    run_repetitions,
    validate_candidate_binding,
    validate_repetitions,
    write_json_once,
)


class ScaleClosureRunnerTest(unittest.TestCase):
    def test_capacity_metric_paths_match_component_contract(self) -> None:
        self.assertEqual(
            capacity_metrics_path(
                {"listeners": [{"name": "probe", "port": 8080}, {"name": "metrics", "port": 19101}]}
            ),
            COMPONENT_METRICS_PATH,
        )
        self.assertEqual(capacity_metrics_path({"listeners": [{"name": "probe", "port": 9121}]}), "/metrics")

    def test_scale_down_diagnostics_skip_stopped_instances(self) -> None:
        component = {"compose_services": ["backend", "backend-2"]}
        self.assertEqual(_diagnostic_services(component, {"backend"}), [(0, "backend")])
        self.assertEqual(_diagnostic_services(component, None), [(0, "backend"), (1, "backend-2")])

    def test_formal_mode_allows_only_two_runs(self) -> None:
        self.assertEqual(validate_repetitions(2), 2)
        for value in (0, 1, 3, 4):
            with self.assertRaisesRegex(ValueError, "exactly --repetitions 2"):
                validate_repetitions(value)

    def test_average_is_arithmetic_and_rejects_empty_input(self) -> None:
        self.assertEqual(average([1, 2, 4]), 2.333)
        with self.assertRaises(ValueError):
            average([])

    def test_failed_evidence_is_retained_and_cannot_be_overwritten(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "run-1" / "summary.json"
            write_json_once(path, {"status": "execution_failed", "failure_stage": "build"})
            with self.assertRaisesRegex(ValueError, "refusing to overwrite"):
                write_json_once(path, {"status": "target_met"})

    def test_candidate_binding_rejects_a_different_candidate(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "binding.json"
            candidate = {"version": "2.0.5", "revision": "a" * 40}
            validate_candidate_binding(path, candidate)
            with self.assertRaisesRegex(ValueError, "another candidate"):
                validate_candidate_binding(path, {"version": "2.0.5", "revision": "b" * 40})

    def test_matrix_order_is_fixed_and_contains_every_required_transition(self) -> None:
        self.assertEqual(MATRIX_ORDER[0], "normal-concurrency")
        self.assertEqual(MATRIX_ORDER[-1], "terminal-closure")
        self.assertIn("business-scale-up", MATRIX_ORDER)
        self.assertIn("observability-scale-down", MATRIX_ORDER)
        self.assertIn("rabbitmq-short-fault", MATRIX_ORDER)
        self.assertIn("kafka-short-fault", MATRIX_ORDER)
        self.assertIn("business-search-es-fault", MATRIX_ORDER)
        self.assertIn("observability-es-fault", MATRIX_ORDER)
        self.assertIn("victoriametrics-fault", MATRIX_ORDER)

    def test_result_classification_preserves_execution_failures(self) -> None:
        self.assertEqual(final_result(["target_met", "target_met"]), "target_met")
        self.assertEqual(final_result(["boundary_found", "boundary_found"]), "boundary_found")
        self.assertEqual(final_result(["boundary_found", "execution_failed"]), "execution_failed")

    def test_run_two_is_not_skipped_after_run_one_failure(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            summaries = [
                {"run": 1, "status": "execution_failed"},
                {"run": 2, "status": "boundary_found"},
            ]
            with mock.patch("phase18_scale_closure.run_one", side_effect=summaries) as run_one:
                result = run_repetitions(Path(directory), {"candidate": "fixed"}, 2)
            self.assertEqual([item["run"] for item in result], [1, 2])
            self.assertEqual(run_one.call_count, 2)

    def test_no_global_prune_or_third_run_is_embedded(self) -> None:
        wrapper = Path(__file__).resolve().parents[1] / "verify-phase18-scale-closure.sh"
        self.assertIn("$2 != 2", wrapper.read_text(encoding="utf-8"))
        runner = Path(__file__).resolve().parent / "phase18_scale_closure.py"
        source = runner.read_text(encoding="utf-8")
        self.assertNotIn("docker system prune", source)
        self.assertNotIn("repetitions 3", source)


if __name__ == "__main__":
    unittest.main()

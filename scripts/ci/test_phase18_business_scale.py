"""Safety and arithmetic tests for the Phase 18-03 runner."""

from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from phase18_business_scale import average, validate_candidate_binding, validate_repetitions, write_json_once


class BusinessScaleRunnerTest(unittest.TestCase):
    def test_formal_mode_rejects_any_count_other_than_two(self) -> None:
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
            write_json_once(path, {"status": "execution_failed", "reason": "Docker unavailable"})
            self.assertEqual(json.loads(path.read_text())["status"], "execution_failed")
            with self.assertRaisesRegex(ValueError, "refusing to overwrite"):
                write_json_once(path, {"status": "target_met"})

    def test_candidate_binding_rejects_a_different_candidate(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "binding.json"
            candidate = {"version": "2.0.3", "revision": "a" * 40}
            validate_candidate_binding(path, candidate)
            with self.assertRaisesRegex(ValueError, "another candidate"):
                validate_candidate_binding(path, {"version": "2.0.3", "revision": "b" * 40})

    def test_formal_shell_wrapper_has_no_third_run_argument(self) -> None:
        wrapper = Path(__file__).resolve().parents[1] / "verify-phase18-business-scale.sh"
        source = wrapper.read_text()
        self.assertIn("$2 != 2", source)
        self.assertNotIn("repetitions 3", source)


if __name__ == "__main__":
    unittest.main()

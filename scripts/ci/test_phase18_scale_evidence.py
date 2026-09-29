"""Tamper tests for Phase 18-05 closure evidence."""

from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from phase18_scale_closure import ALLOWED_FILES, MATRIX_ORDER, UNIT_NAMES
from phase18_scale_evidence import validate_evidence


def command(name: str = "check", exit_code: int = 0) -> dict[str, object]:
    return {
        "name": name,
        "command": ["true"],
        "cwd": ".",
        "exit_code": exit_code,
        "elapsed_seconds": 1.0,
        "stdout": f"{name}.stdout.log",
        "stderr": f"{name}.stderr.log",
        **(
            {
                "failure": {
                    "stage": name,
                    "exit_code": exit_code,
                    "stdout": f"{name}.stdout.log",
                    "stderr": f"{name}.stderr.log",
                }
            }
            if exit_code
            else {}
        ),
    }


def fixture(root: Path) -> None:
    candidate = {
        "version": "2.0.5",
        "revision": "a" * 40,
        "branch": "develop/2.0.5",
        "condition": {
            "runner": "scripts/verify-phase18-scale-closure.sh --repetitions 2",
            "matrix_order": list(MATRIX_ORDER),
            "diagnostics": "direct-private-probe",
        },
        "candidate_files": [],
        "file_ledger": [{"path": path, "status": "not_needed"} for path in ALLOWED_FILES],
    }
    binding = {"schema": "gopulse.phase18.scale-closure-binding.v1", "candidate": candidate}
    (root / "binding.json").write_text(json.dumps(binding), encoding="utf-8")
    run_summaries = []
    for number in (1, 2):
        run = root / f"run-{number}"
        for name in UNIT_NAMES:
            (run / name).mkdir(parents=True, exist_ok=True)
        for name in ("u1", "u2", "u3", "u4", "cleanup"):
            (run / "U3" / f"{name}.stdout.log").write_text("ok\n", encoding="utf-8")
            (run / "U3" / f"{name}.stderr.log").write_text("", encoding="utf-8")
        (run / "binding.json").write_text(json.dumps(binding), encoding="utf-8")
        units = {}
        for name in UNIT_NAMES:
            units[name] = {"status": "target_met", "passed": True, "commands": []}
        units["U3"] = {
            "status": "target_met",
            "passed": True,
            "commands": [command("u3")],
            "cleanup": command("cleanup"),
        }
        (run / "U3" / "u3.stdout.log").write_text("ok\n", encoding="utf-8")
        (run / "U3" / "u3.stderr.log").write_text("", encoding="utf-8")
        (run / "U3" / "matrix.json").write_text(
            json.dumps({"run": number, "status": "target_met", "cleanup": command("cleanup")}), encoding="utf-8"
        )
        summary = {
            "schema": "gopulse.phase18.scale-closure-run.v1",
            "run": number,
            "candidate": candidate,
            "units": units,
            "passed": {name: True for name in UNIT_NAMES},
            "status": "target_met",
        }
        (run / "summary.json").write_text(json.dumps(summary), encoding="utf-8")
        run_summaries.append(summary)
    (root / "summary.json").write_text(
        json.dumps(
            {
                "schema": "gopulse.phase18.scale-closure-summary.v1",
                "candidate": candidate,
                "runs": 2,
                "run_statuses": ["target_met", "target_met"],
                "unit_pass_counts": {name: 2 for name in UNIT_NAMES},
                "deterministic_counts": {name: "2/2" for name in UNIT_NAMES},
                "raw_numeric": {"elapsed_seconds": [1.0, 3.0]},
                "numeric_averages": {"elapsed_seconds": 2.0},
                "cleanup": [{"run": 1, "exit_code": 0}, {"run": 2, "exit_code": 0}],
                "run_summaries": ["run-1/summary.json", "run-2/summary.json"],
                "result": "target_met",
            }
        ),
        encoding="utf-8",
    )


class ScaleEvidenceTest(unittest.TestCase):
    def test_valid_two_run_evidence(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fixture(root)
            self.assertEqual(validate_evidence(root)["result"], "target_met")

    def test_missing_run_is_rejected(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fixture(root)
            import shutil

            shutil.rmtree(root / "run-2")
            with self.assertRaisesRegex(ValueError, "exactly run-1 and run-2"):
                validate_evidence(root)

    def test_third_run_is_rejected(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fixture(root)
            (root / "run-3").mkdir()
            with self.assertRaisesRegex(ValueError, "exactly run-1 and run-2"):
                validate_evidence(root)

    def test_wrong_average_is_rejected(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fixture(root)
            summary = json.loads((root / "summary.json").read_text())
            summary["numeric_averages"]["elapsed_seconds"] = 99
            (root / "summary.json").write_text(json.dumps(summary))
            with self.assertRaisesRegex(ValueError, "average"):
                validate_evidence(root)

    def test_candidate_drift_is_rejected(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fixture(root)
            run = json.loads((root / "run-2" / "binding.json").read_text())
            run["candidate"]["revision"] = "b" * 40
            (root / "run-2" / "binding.json").write_text(json.dumps(run))
            with self.assertRaisesRegex(ValueError, "binding drift"):
                validate_evidence(root)

    def test_failed_command_requires_original_output_and_failure_stage(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fixture(root)
            run = json.loads((root / "run-1" / "summary.json").read_text())
            run["units"]["U3"]["status"] = "execution_failed"
            run["units"]["U3"]["passed"] = False
            run["passed"]["U3"] = False
            failed = command("failed", 17)
            run["units"]["U3"]["commands"] = [failed]
            (root / "run-1" / "U3" / "failed.stdout.log").write_text("failure\n")
            (root / "run-1" / "U3" / "failed.stderr.log").write_text("failure\n")
            failed.pop("failure")
            (root / "run-1" / "summary.json").write_text(json.dumps(run))
            with self.assertRaisesRegex(ValueError, "failure record"):
                validate_evidence(root)


if __name__ == "__main__":
    unittest.main()

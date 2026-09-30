import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

try:
    import phase20_optimization as optimization
except ModuleNotFoundError:
    from scripts.ci import phase20_optimization as optimization


class OptimizationEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.contract_path = self.root / "contract.json"
        self.contract_path.write_text("{}\n", encoding="utf-8")
        self.binding = {
            "version": optimization.B0_VERSION,
            "revision": optimization.B0_REVISION,
            "manifest_sha256": "sha256:" + "a" * 64,
        }
        self.contract = {"candidate": self.binding}

    def tearDown(self):
        self.temp.cleanup()

    def _write_fixture(self, cells=None):
        cells = cells or [
            {"repeat": repeat, "stage": stage, "passed": True}
            for repeat in range(1, 4)
            for stage in ("rps-50", "rps-100", "rps-150", "rps-200")
        ]
        raw = self.root / "optimization-self-tests.txt"
        raw.write_text("O01 O02 O03 O05\n", encoding="utf-8")
        receipt = self.root / "optimization-self-tests.json"
        receipt.write_text(json.dumps({
            "cases": [{"case_id": case, "result": "pass"} for case in ("O01", "O02", "O03", "O05")],
            "raw_sha256": optimization.digest(raw),
        }), encoding="utf-8")
        opt = {
            "schema": optimization.SCHEMA,
            "mode": optimization.MODE,
            "formal": True,
            "contract_sha256": optimization.digest(self.contract_path),
            "profile_sha256": optimization.digest(optimization.OPT_PROFILE_PATH),
            "tools": optimization.optimization_tool_digests(),
            "candidate": self.binding,
            "b1": None,
            "improvement": None,
            "self_tests": {"path": receipt.name, "sha256": optimization.digest(receipt)},
            "execution_status": "complete",
            "optimization_status": "not_needed",
            "improvement_status": "not_applicable",
        }
        (self.root / "optimization.json").write_text(json.dumps(opt), encoding="utf-8")
        (self.root / "diagnostic.json").write_text(json.dumps({
            "candidate": self.binding,
            "contract_sha256": optimization.digest(self.contract_path),
        }), encoding="utf-8")
        result = {
            "execution_status": "complete",
            "capability_status": "target_met",
            "cells": cells,
            "measurements": [],
            "aggregates": {},
        }
        return opt, result

    def _verify(self, result, final=False):
        with patch.object(optimization, "verify_contract", return_value=self.contract), \
             patch.object(optimization.evidence, "verify_directory", return_value=result):
            return optimization.verify_optimization_directory(self.root, self.contract_path, expected_formal=True, final=final)

    def test_O01_rejects_missing_or_mixed_repetitions(self):
        _, result = self._write_fixture()
        result["cells"] = result["cells"][:-1]
        with self.assertRaises(optimization.Incomplete):
            self._verify(result)

    def test_O02_rejects_an_improvement_rate_in_verify_only(self):
        opt, result = self._write_fixture()
        opt["improvement_rate"] = 0.2
        (self.root / "optimization.json").write_text(json.dumps(opt), encoding="utf-8")
        with self.assertRaises(optimization.Incomplete):
            self._verify(result)

    def test_O03_rejects_a_fabricated_B1_candidate(self):
        opt, result = self._write_fixture()
        opt["b1"] = {"revision": "b" * 40}
        (self.root / "optimization.json").write_text(json.dumps(opt), encoding="utf-8")
        with self.assertRaises(optimization.Incomplete):
            self._verify(result)

    def test_O05_rejects_correctness_or_non_degradation_failure(self):
        _, result = self._write_fixture()
        result["capability_status"] = "boundary_found"
        with self.assertRaises(optimization.Incomplete):
            self._verify(result, final=True)


if __name__ == "__main__":
    unittest.main()

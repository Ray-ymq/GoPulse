import json
import contextlib
import io
import tempfile
import unittest
from pathlib import Path

from phase19_capacity import calibration, candidate_binding, failure_reason, load_profile, main, synthetic_sample


class CapacityRunnerTest(unittest.TestCase):
    def test_calibration_does_not_produce_capacity_conclusion(self):
        result = calibration(Path(__file__).resolve().parents[2] / "loadtest/capacity-profile.json")
        self.assertFalse(result["formal"])
        self.assertFalse(result["writes_formal_summary"])
        self.assertIsNone(result["capacity_status"])
        self.assertEqual(len(result["arrival"]), 4)
        self.assertEqual(result["resource_samples"][0]["schema"], "gopulse.phase19.resources.v1")

    def test_candidate_binding_uses_manifest_bytes_and_profile_version(self):
        profile, _ = load_profile(Path(__file__).resolve().parents[2] / "loadtest/capacity-profile.json")
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "manifest.json"
            path.write_text(json.dumps({"version": "2.1.3", "revision": "a" * 40, "images": {}}) + "\n")
            binding, _ = candidate_binding(path, profile)
        self.assertEqual(binding["version"], "2.1.3")
        self.assertTrue(binding["manifest_sha256"].startswith("sha256:"))

    def test_formal_entry_rejects_parameter_overrides_in_calibration(self):
        with contextlib.redirect_stderr(io.StringIO()):
            with self.assertRaises(SystemExit):
                main(["--calibration", "--candidate-manifest", "manifest.json"])

    def test_failure_reason_walks_sampler_error_cause(self):
        source = RuntimeError("Compose ownership was lost")
        wrapped = RuntimeError("resource sampler failed")
        wrapped.__cause__ = source
        self.assertEqual(failure_reason(wrapped), "ownership_lost")
        self.assertEqual(failure_reason(RuntimeError("OOM detected")), "oom")


if __name__ == "__main__":
    unittest.main()

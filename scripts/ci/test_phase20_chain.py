import copy
import json
import tempfile
import unittest
from pathlib import Path

try:
    from phase20_chain import ChainError, _candidate_from_manifest, fixture_document, verify_document
except ModuleNotFoundError:
    from scripts.ci.phase20_chain import ChainError, _candidate_from_manifest, fixture_document, verify_document


class ChainEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.manifest = self.root / "candidate.json"
        self.manifest.write_text(json.dumps({"version": "2.2.2", "revision": "a" * 40}) + "\n", encoding="utf-8")
        self.candidate = _candidate_from_manifest(self.manifest)
        self.document = fixture_document(self.candidate)

    def tearDown(self):
        self.temp.cleanup()

    def test_fixture_recomputes_all_cases(self):
        result = verify_document(self.document, self.candidate)
        self.assertEqual(result["case_count"], 7)
        self.assertEqual([case["case_id"] for case in result["cases"]], ["C01", "C02", "C03", "C04", "C05", "C06", "C07"])

    def test_trace_id_in_metric_labels_is_rejected(self):
        broken = copy.deepcopy(self.document)
        broken["cases"][0]["raw"]["metrics"][0]["labels"]["trace_id"] = "0" * 32
        with self.assertRaises(ChainError):
            verify_document(broken, self.candidate)

    def test_missing_case_is_rejected(self):
        broken = copy.deepcopy(self.document)
        broken["cases"].pop()
        with self.assertRaises(ChainError):
            verify_document(broken, self.candidate)


if __name__ == "__main__":
    unittest.main()

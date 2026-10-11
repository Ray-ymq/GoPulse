from __future__ import annotations

import unittest

from validate_quality_result import validate


class QualityResultTests(unittest.TestCase):
    def test_selected_failure_and_skip_are_blocking(self) -> None:
        checks = {"backend": True, "integration_business": True, "frontend": False}
        needs = {
            "governance": {"result": "success"},
            "docs": {"result": "success"},
            "module-verify": {"result": "success"},
            "integration": {"result": "skipped"},
        }
        errors = validate(checks, needs)
        self.assertTrue(any("integration_business" in error for error in errors))

    def test_unselected_jobs_may_be_skipped(self) -> None:
        checks = {"backend": False, "frontend": False}
        needs = {"governance": {"result": "success"}, "docs": {"result": "success"}}
        self.assertEqual(validate(checks, needs), [])

    def test_update_governance_does_not_require_product_jobs(self) -> None:
        checks = {"backend": True, "tools": True}
        needs = {"governance": {"result": "success"}, "docs": {"result": "success"}}
        self.assertEqual(validate(checks, needs, run_product_checks=False), [])


if __name__ == "__main__":
    unittest.main()

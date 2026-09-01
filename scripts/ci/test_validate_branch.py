from __future__ import annotations

import subprocess
import tempfile
import unittest
from pathlib import Path

from validate_branch import changed_files, validate


class BranchGovernanceTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.repo = Path(self.temp.name)
        plan = self.repo / "dev/imple/Phase-00/Phase-00-总实施方案.md"
        plan.parent.mkdir(parents=True)
        plan.write_text(
            "| 执行批次 | 目标版本 | 开发分支 | 当前状态 |\n"
            "| --- | --- | --- | --- |\n"
            "| Phase-00-06 | `0.1.6` | `develop/0.1.6` | 进行中 |\n",
            encoding="utf-8",
        )
        (self.repo / "VERSION").write_text("0.1.6\n", encoding="utf-8")

    def tearDown(self) -> None:
        self.temp.cleanup()

    def test_accepts_authoritative_development_branch(self) -> None:
        self.assertEqual(validate(self.repo, "develop/0.1.6", None, []), [])

    def test_rejects_illegal_branch_name(self) -> None:
        self.assertTrue(validate(self.repo, "feature/review", None, []))

    def test_rejects_unallocated_version(self) -> None:
        errors = validate(self.repo, "develop/0.1.7", None, [])
        self.assertIn("exactly one authoritative", errors[0])

    def test_rejects_version_file_mismatch(self) -> None:
        (self.repo / "VERSION").write_text("0.1.5\n", encoding="utf-8")
        errors = validate(self.repo, "develop/0.1.6", None, [])
        self.assertIn("expected '0.1.6'", errors[0])

    def test_accepts_update_planning_scope(self) -> None:
        files = ["dev/imple/Phase-01/plan.md", ".github/workflows/ci.yml", "AGENTS.md"]
        self.assertEqual(validate(self.repo, "update", None, files), [])

    def test_rejects_update_application_changes_and_version(self) -> None:
        errors = validate(self.repo, "update", None, ["backend/main.go", "frontend/src/App.vue", "VERSION"])
        self.assertIn("backend/main.go", errors[0])
        self.assertIn("frontend/src/App.vue", errors[0])
        self.assertIn("VERSION", errors[0])

    def test_rejects_duplicate_authoritative_allocation(self) -> None:
        duplicate = self.repo / "dev/imple/Phase-99/Phase-99-总实施方案.md"
        duplicate.parent.mkdir(parents=True)
        duplicate.write_text(
            "| Phase-99-01 | `0.1.6` | `develop/0.1.6` | 未开始 |\n",
            encoding="utf-8",
        )
        errors = validate(self.repo, "develop/0.1.6", None, [])
        self.assertIn("found 2", errors[0])

    def test_changed_files_preserves_non_ascii_paths(self) -> None:
        subprocess.run(["git", "init", "--quiet"], cwd=self.repo, check=True)
        subprocess.run(["git", "config", "user.name", "GoPulse CI"], cwd=self.repo, check=True)
        subprocess.run(["git", "config", "user.email", "ci@gopulse.invalid"], cwd=self.repo, check=True)
        subprocess.run(["git", "add", "."], cwd=self.repo, check=True)
        subprocess.run(["git", "commit", "--quiet", "-m", "baseline"], cwd=self.repo, check=True)
        base_ref = subprocess.run(
            ["git", "rev-parse", "HEAD"],
            cwd=self.repo,
            check=True,
            text=True,
            capture_output=True,
        ).stdout.strip()

        path = self.repo / "dev/imple/Phase-01/阶段实施方案.md"
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("# 阶段实施方案\n", encoding="utf-8")
        subprocess.run(["git", "add", str(path.relative_to(self.repo))], cwd=self.repo, check=True)
        subprocess.run(["git", "commit", "--quiet", "-m", "add plan"], cwd=self.repo, check=True)

        self.assertEqual(changed_files(self.repo, base_ref), ["dev/imple/Phase-01/阶段实施方案.md"])


if __name__ == "__main__":
    unittest.main()

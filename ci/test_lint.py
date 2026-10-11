from __future__ import annotations

import json
import os
import stat
import subprocess
import tempfile
import unittest
from pathlib import Path

from lint import Finding, parse_eslint_findings, run


class LintTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.repo = Path(self.temp.name)
        (self.repo / "devtools").mkdir()
        (self.repo / "devtools/toolchains.json").write_text('{"golangci_lint":{"version":"test"}}', encoding="utf-8")
        (self.repo / ".golangci.yml").write_text('version: "2"\n', encoding="utf-8")
        (self.repo / "backend").mkdir()
        (self.repo / "backend/main.go").write_text("package main\n", encoding="utf-8")
        (self.repo / "fake-lint").write_text(
            "#!/bin/sh\nprintf '%s\\n' 'main.go:1:1: controlled warning (fixture)'\nexit 1\n",
            encoding="utf-8",
        )
        self.repo.joinpath("fake-lint").chmod(stat.S_IRWXU)
        subprocess.run(["git", "init", "-q"], cwd=self.repo, check=True)
        subprocess.run(["git", "config", "user.name", "test"], cwd=self.repo, check=True)
        subprocess.run(["git", "config", "user.email", "test@example.invalid"], cwd=self.repo, check=True)
        subprocess.run(["git", "add", "."], cwd=self.repo, check=True)
        subprocess.run(["git", "commit", "-qm", "base"], cwd=self.repo, check=True)
        self.base = subprocess.run(
            ["git", "rev-parse", "HEAD"], cwd=self.repo, check=True, text=True, capture_output=True
        ).stdout.strip()
        self.old_tool = os.environ.get("GOPULSE_LINT_TOOL")
        os.environ["GOPULSE_LINT_TOOL"] = str(self.repo / "fake-lint")

    def tearDown(self) -> None:
        if self.old_tool is None:
            os.environ.pop("GOPULSE_LINT_TOOL", None)
        else:
            os.environ["GOPULSE_LINT_TOOL"] = self.old_tool
        self.temp.cleanup()

    def test_existing_findings_do_not_block_baseline_mode(self) -> None:
        code, summary = run(self.repo, "backend", self.base)
        self.assertEqual(code, 0)
        self.assertIsNotNone(summary)
        assert summary is not None
        self.assertEqual((summary.stored, summary.new, summary.raw_exit), (1, 0, 1))

    def test_changed_line_blocks_baseline_mode(self) -> None:
        (self.repo / "backend/main.go").write_text("package changed\n", encoding="utf-8")
        code, summary = run(self.repo, "backend", self.base)
        self.assertEqual(code, 1)
        self.assertIsNotNone(summary)
        assert summary is not None
        self.assertEqual(summary.new, 1)

    def test_eslint_json_is_classified_by_file_and_line(self) -> None:
        raw = json.dumps(
            [{"filePath": "/tmp/frontend/src/App.vue", "messages": [{"line": 7, "severity": 2, "message": "fixture"}]}]
        )
        findings = parse_eslint_findings(raw)
        self.assertEqual(findings, [Finding("/tmp/frontend/src/App.vue", 7, "fixture")])

    def test_unknown_module_is_rejected(self) -> None:
        with self.assertRaises(ValueError):
            run(self.repo, "unknown")


if __name__ == "__main__":
    unittest.main()

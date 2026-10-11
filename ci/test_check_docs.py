from __future__ import annotations

import subprocess
import tempfile
import unittest
from pathlib import Path

from check_docs import check, check_commands, check_links


class CheckDocsTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.repo = Path(self.temp.name)
        (self.repo / "dev/contracts").mkdir(parents=True)
        (self.repo / "Makefile").write_text(
            ".PHONY: docs-check verify\nGO_MODULES := backend\nNPM_MODULES := frontend\n"
            "docs-check:\n\t@true\nverify:\n\t@true\ntest:\n\t@true\n",
            encoding="utf-8",
        )
        (self.repo / "backend").mkdir()
        (self.repo / "backend/Makefile").write_text("test:\n\t@true\n", encoding="utf-8")
        (self.repo / "frontend").mkdir()
        (self.repo / "frontend/package.json").write_text('{"scripts":{"lint":"eslint ."}}', encoding="utf-8")
        (self.repo / "AGENTS.md").write_text(
            "# Root\n\n[contract](dev/contracts/README.md#有效合同)\n\n"
            "```text\n[missing](nope.md)\nmake test MODULE=backend\n```\n\n"
            "| command |\n| --- |\n| `make test MODULE=backend` |\n| `npm run lint` |\n",
            encoding="utf-8",
        )
        (self.repo / "dev/contracts/README.md").write_text("# 合同\n\n## 有效合同\n", encoding="utf-8")

    def tearDown(self) -> None:
        self.temp.cleanup()

    def test_links_and_commands_ignore_fenced_examples(self) -> None:
        self.assertEqual(check_links(self.repo, [Path("AGENTS.md"), Path("dev/contracts/README.md")]), [])
        self.assertEqual(check_commands(self.repo, [Path("AGENTS.md")]), [])

    def test_broken_link_and_unknown_module_are_reported(self) -> None:
        path = self.repo / "AGENTS.md"
        path.write_text("[missing](missing%20file.md#nope)\nmake verify MODULE=unknown\n", encoding="utf-8")
        errors = check_links(self.repo, [Path("AGENTS.md")]) + check_commands(self.repo, [Path("AGENTS.md")])
        self.assertTrue(any("missing local link" in error for error in errors))
        self.assertTrue(any("unknown MODULE" in error for error in errors))

    def test_frozen_document_changes_are_rejected(self) -> None:
        frozen = self.repo / "docs/GoPulse 高并发架构设计.md"
        frozen.parent.mkdir()
        frozen.write_text("base\n", encoding="utf-8")
        for path, content in {
            "docs/GoPulse 可观测架构设计.md": "observe\n",
            "dev/design/GoPulse-目标架构落地大纲.md": "outline\n",
        }.items():
            target = self.repo / path
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text(content, encoding="utf-8")
        subprocess.run(["git", "init", "-q"], cwd=self.repo, check=True)
        subprocess.run(["git", "config", "user.name", "test"], cwd=self.repo, check=True)
        subprocess.run(["git", "config", "user.email", "test@example.invalid"], cwd=self.repo, check=True)
        subprocess.run(["git", "add", "."], cwd=self.repo, check=True)
        subprocess.run(["git", "commit", "-qm", "base"], cwd=self.repo, check=True)
        base = subprocess.run(["git", "rev-parse", "HEAD"], cwd=self.repo, text=True, check=True, capture_output=True).stdout.strip()
        frozen.write_text("changed\n", encoding="utf-8")
        errors = check(self.repo, base)
        self.assertTrue(any("frozen document changed" in error for error in errors))


if __name__ == "__main__":
    unittest.main()

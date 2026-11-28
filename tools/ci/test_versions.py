from __future__ import annotations

import json
import os
import shutil
import subprocess
import sys
import unittest
from pathlib import Path

from commits import git
from test_commits import RepositoryFixture, structured_message
from versions import (
    VERSION_FILES, check_files, read_files, read_version_policy, replace_versions,
    stable_base, upgrade,
)


ROOT = Path(__file__).resolve().parents[2]


class UpgradeTests(unittest.TestCase):
    def setUp(self) -> None:
        self.policy = read_version_policy(ROOT)

    def test_every_type_has_the_agreed_upgrade(self) -> None:
        expected_levels = {
            "feat": "minor", "fix": "patch", "perf": "patch", "revert": "patch",
            "build": "none", "chore": "none", "ci": "none", "refactor": "none", "style": "none", "test": "none",
        }
        self.assertEqual(self.policy.bumps, expected_levels)
        for kind, level in expected_levels.items():
            expected = {"minor": "1.3.0", "patch": "1.2.4", "none": "1.2.3"}[level]
            with self.subTest(kind=kind):
                self.assertEqual(upgrade(structured_message(f"{kind}(产品): 更新行为"), "1.2.3", self.policy), (expected, level))

    def test_breaking_marker_overrides_every_type(self) -> None:
        for kind in self.policy.bumps:
            for message in (
                structured_message(f"{kind}!: 修改接口"),
                structured_message(f"{kind}(api)!: 修改接口"),
                structured_message(f"{kind}: 修改接口") + "\nBREAKING CHANGE: 删除旧接口\n",
                structured_message(f"{kind}: 修改接口") + "\nBREAKING-CHANGE: 删除旧接口\n",
            ):
                with self.subTest(message=message):
                    self.assertEqual(upgrade(message, "1.2.3", self.policy), ("2.0.0", "major"))

    def test_breaking_examples_and_mentions_do_not_upgrade_major(self) -> None:
        for suffix in (
            "\n```text\nBREAKING CHANGE: 示例\n```\n",
            "\n~~~~text\nBREAKING-CHANGE: 示例\n~~~~\n",
            "\n<!--\nBREAKING CHANGE: 示例\n-->\n",
            "\n说明：BREAKING CHANGE: 示例\n",
        ):
            with self.subTest(suffix=suffix):
                self.assertEqual(upgrade(structured_message("fix: 修复行为") + suffix, "1.2.3", self.policy)[0], "1.2.4")

    def test_only_stable_versions_are_accepted_by_default(self) -> None:
        for version in ("1.0.0-rc.1", "v1.0.0", "01.0.0", "1.0", "1.0.0+build", "1.0.-1", "１.0.0"):
            with self.subTest(version=version), self.assertRaises(ValueError):
                stable_base(version)
        self.assertEqual(stable_base("0.0.0"), "0.0.0")
        self.assertEqual(stable_base("1.0.0-rc.1", allow_legacy_rc=True), "1.0.0")


class VersionRepositoryTests(RepositoryFixture):
    def version_cli(self, action: str, *args: str, event: dict | None = None, event_name: str = "pull_request") -> subprocess.CompletedProcess:
        environment = {key: value for key, value in os.environ.items() if not key.startswith(("GITHUB_", "GOPULSE_VERSION_"))}
        if event:
            path = self.write("version-event.json", json.dumps(event))
            environment.update(GITHUB_EVENT_NAME=event_name, GITHUB_EVENT_PATH=str(path))
        return subprocess.run(
            [sys.executable, str(ROOT / "tools/ci/versions.py"), action, "--repo", str(self.repo), *args],
            text=True, capture_output=True, env=environment,
        )

    def enable_hook(self) -> None:
        for relative in ("Makefile", ".githooks/commit-msg", "tools/ci/commits.py", "tools/ci/versions.py"):
            target = self.repo / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(ROOT / relative, target)
        (self.repo / ".githooks/commit-msg").chmod(0o755)

    def test_preview_and_repeated_updates_preserve_dependency_versions(self) -> None:
        message = self.write("message.txt", structured_message("feat: 增加功能"))
        before = read_files(self.repo)
        result = self.version_cli("plan", "--message-file", str(message))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("1.0.0 -> 1.1.0", result.stdout)
        self.assertEqual(read_files(self.repo), before)
        for _ in range(2):
            result = self.version_cli("update", "--message-file", str(message))
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(check_files(read_files(self.repo)), "1.1.0")
        package = json.loads((self.repo / VERSION_FILES[2]).read_text())
        lock = json.loads((self.repo / VERSION_FILES[3]).read_text())
        self.assertEqual(package["dependencies"]["example"], "1.0.0")
        self.assertEqual(lock["packages"]["node_modules/example"]["version"], "1.0.0")
        self.assertEqual(git(self.repo, "diff", "--cached", "--name-only"), "")

    def test_policy_rejects_missing_types_unknown_levels_and_short_baselines(self) -> None:
        for changes in ({"types": {"feat": "minor"}}, {"types": {kind: "unknown" for kind in self.policy.types}}, {"required_after": "HEAD"}):
            self.write_policy()
            data = json.loads((self.repo / ".conventional-commits.json").read_text())
            data["versioning"].update(changes)
            self.write(".conventional-commits.json", json.dumps(data))
            with self.subTest(changes=changes), self.assertRaises(ValueError):
                read_version_policy(self.repo)

    def test_inconsistent_versions_and_prereleases_fail_current_check(self) -> None:
        self.write("VERSION", "1.0.1\n")
        self.assertNotEqual(self.version_cli("check", "--current-only").returncode, 0)
        self.write("VERSION", "1.0.0-rc.1\n")
        self.assertNotEqual(self.version_cli("check", "--current-only").returncode, 0)

    def test_commit_entry_stages_versions_and_preserves_staged_product_changes(self) -> None:
        self.enable_hook()
        spec = VERSION_FILES[1]
        self.write(spec, (self.repo / spec).read_text() + "  title: 新名称\n")
        git(self.repo, "add", "--", spec)
        self.write("unrelated.txt", "用户的修改\n")
        message = self.write("message.txt", structured_message("feat: 增加功能"))
        result = subprocess.run(["make", "commit", f"MESSAGE_FILE={message}"], cwd=self.repo, text=True, capture_output=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        revision = git(self.repo, "rev-parse", "HEAD").strip()
        self.assertEqual(check_files(read_files(self.repo, revision)), "1.1.0")
        self.assertIn("title: 新名称", git(self.repo, "show", f"{revision}:{spec}"))
        self.assertNotIn("unrelated.txt", git(self.repo, "ls-tree", "--name-only", revision))
        self.assertEqual(self.version_cli("check").returncode, 0)

    def test_commit_entry_rejects_partial_staging_without_mutating_any_file(self) -> None:
        self.enable_hook()
        self.write(VERSION_FILES[1], (self.repo / VERSION_FILES[1]).read_text() + "  title: 未暂存名称\n")
        before, index = read_files(self.repo), git(self.repo, "write-tree")
        message = self.write("message.txt", structured_message("fix: 修复行为"))
        result = subprocess.run(["make", "commit", f"MESSAGE_FILE={message}"], cwd=self.repo, text=True, capture_output=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("未暂存修改", result.stderr)
        self.assertEqual(read_files(self.repo), before)
        self.assertEqual(git(self.repo, "write-tree"), index)

    def test_prepared_versions_are_not_incremented_again_by_hook(self) -> None:
        self.enable_hook()
        message = self.write("message.txt", structured_message("fix: 修复行为"))
        self.assertEqual(self.version_cli("update", "--message-file", str(message)).returncode, 0)
        git(self.repo, "add", "--", *VERSION_FILES)
        revision = self.commit(message.read_text())
        self.assertEqual(check_files(read_files(self.repo, revision)), "1.0.1")

    def test_direct_git_commit_rejects_missing_version_updates(self) -> None:
        self.enable_hook()
        self.write("change.txt", "本次修改\n")
        git(self.repo, "add", "--", "change.txt")
        before, index = read_files(self.repo), git(self.repo, "write-tree")
        result = subprocess.run(
            ["git", "commit", "--quiet", "-m", structured_message("fix: 修复行为")],
            cwd=self.repo, text=True, capture_output=True,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("make commit", result.stderr)
        self.assertEqual(read_files(self.repo), before)
        self.assertEqual(git(self.repo, "write-tree"), index)
        self.assertEqual(git(self.repo, "rev-parse", "HEAD").strip(), self.legacy)

    def test_amend_uses_original_parent_and_does_not_increment_again(self) -> None:
        self.enable_hook()
        self.commit(structured_message("feat: 增加功能"))
        result = subprocess.run(
            ["git", "commit", "--quiet", "--amend", "-m", structured_message("feat: 补充功能说明")],
            cwd=self.repo, text=True, capture_output=True, env={**os.environ, "GOPULSE_VERSION_BASE": "HEAD^"},
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(check_files(read_files(self.repo, "HEAD")), "1.1.0")
        self.assertEqual(self.version_cli("check").returncode, 0)

    def test_ci_catches_an_earlier_bypassed_version_even_with_a_valid_tip(self) -> None:
        bad = self.commit(structured_message("feat: 漏更版本"))
        self.enable_hook()
        self.commit(structured_message("fix: 后续修复"))
        result = self.version_cli("check")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(bad[:12], result.stderr)

    def test_pr_and_push_events_check_actual_source_commits(self) -> None:
        self.enable_hook()
        revision = self.commit(structured_message("fix: 修复行为"))
        for event_name, event in (
            ("pull_request", {"pull_request": {"base": {"sha": self.legacy}, "head": {"sha": revision}, "title": "fix: 修复行为"}}),
            ("push", {"before": self.legacy, "after": revision}),
            ("push", {"before": "0" * 40, "after": revision}),
        ):
            with self.subTest(event_name=event_name, event=event):
                result = self.version_cli("check", event=event, event_name=event_name)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn("1 个", result.stdout)

    def test_real_merge_does_not_increment_again(self) -> None:
        self.enable_hook()
        git(self.repo, "checkout", "--quiet", "-b", "feature")
        self.commit(structured_message("feat: 增加功能"))
        git(self.repo, "checkout", "--quiet", "main")
        self.commit(structured_message("ci: 调整检查"))
        git(self.repo, "merge", "--quiet", "--no-ff", "feature", "-m", "Merge feature into main")
        self.assertEqual(check_files(read_files(self.repo, "HEAD")), "1.1.0")
        self.assertEqual(self.version_cli("check").returncode, 0)

    def test_missing_baseline_fails_instead_of_skipping_history(self) -> None:
        data = json.loads((self.repo / ".conventional-commits.json").read_text())
        data["versioning"]["required_after"] = "f" * 40
        self.write(".conventional-commits.json", json.dumps(data))
        self.assertNotEqual(self.version_cli("check").returncode, 0)

    def test_initial_rc_migration_is_allowed_only_at_configured_baseline(self) -> None:
        files = replace_versions(read_files(self.repo), "1.0.0")
        for path, content in files.items():
            self.write(path, content.replace("1.0.0", "1.0.0-rc.1"))
        git(self.repo, "add", "--", *VERSION_FILES)
        git(self.repo, "commit", "--quiet", "--amend", "--no-verify", "-m", "历史候选版本基准")
        self.legacy = self.commit("历史候选版本")
        self.write_policy()
        self.enable_hook()
        self.commit(structured_message("feat: 新增版本管理"))
        self.assertEqual(check_files(read_files(self.repo, "HEAD")), "1.1.0")
        self.assertEqual(self.version_cli("check").returncode, 0)
        self.assertNotEqual(self.version_cli("plan", "--base-ref", self.legacy + "^", "--message-file", str(self.write("message.txt", structured_message("feat: 新增功能")))).returncode, 0)


if __name__ == "__main__":
    unittest.main()

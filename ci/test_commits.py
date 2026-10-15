from __future__ import annotations

import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from commits import Policy, check_file, commit_ids, event_range, git, read_policy, validate_message, validate_title


ROOT = Path(__file__).resolve().parents[1]
TYPES = ("build", "chore", "ci", "docs", "feat", "fix", "perf", "refactor", "revert", "style", "test")
POLICY = Policy(TYPES, "a" * 40)


class MessageTests(unittest.TestCase):
    def test_supported_types(self) -> None:
        for kind in TYPES:
            with self.subTest(kind=kind):
                self.assertEqual(validate_title(f"{kind}: describe the change", POLICY), [])

    def test_scopes_breaking_marker_and_chinese(self) -> None:
        for title in (
            "fix(frontend): 修复发帖跳转",
            "refactor(api/v2)!: change response contract",
            "feat!: change defaults",
            "docs(文档): 更新用法 (#123)",
            "test: x",
        ):
            with self.subTest(title=title):
                self.assertEqual(validate_title(title, POLICY), [])

    def test_invalid_titles(self) -> None:
        for title in (
            "", "Add a feature", "Feat: add feature", "unknown: add feature",
            "fix: ", "fix:  description", "fix: description ", "fix:description",
            "fix(): description", "fix(two words): description", "fix(scope!): description",
            "fix : description", "fix: first\nsecond", "fix: first\rsecond",
            "Merge branch 'feature'", "fix!: ",
        ):
            with self.subTest(title=title):
                self.assertTrue(validate_title(title, POLICY))

    def test_body_requires_blank_separator(self) -> None:
        self.assertTrue(validate_message("fix: a change\nBody without separator\n", POLICY))
        self.assertEqual(validate_message("fix: a change\n\nDetailed explanation.\n", POLICY), [])

    def test_breaking_footer(self) -> None:
        for footer in ("BREAKING CHANGE: migrate clients", "BREAKING-CHANGE: 更新配置"):
            with self.subTest(footer=footer):
                self.assertEqual(validate_message(f"feat: a change\n\n{footer}\n", POLICY), [])

    def test_invalid_breaking_footer(self) -> None:
        for footer in ("breaking change: migrate", "Breaking-Change: migrate", "BREAKING CHANGE:", "BREAKING CHANGE: "):
            with self.subTest(footer=footer):
                self.assertTrue(validate_message(f"feat: a change\n\n{footer}\n", POLICY))

    def test_bang_does_not_require_breaking_footer(self) -> None:
        self.assertEqual(validate_message("feat!: change defaults\n", POLICY), [])

    def test_trailers_and_long_descriptions_are_allowed(self) -> None:
        message = "fix: " + "描述" * 100 + "\n\nBody.\n\nCloses: #123\nSigned-off-by: A <a@example.test>\n"
        self.assertEqual(validate_message(message, POLICY), [])


class RepositoryFixture(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(prefix="commit checks ")
        self.addCleanup(self.temporary.cleanup)
        self.repo = Path(self.temporary.name)
        git(self.repo, "init", "--quiet", "--initial-branch=main")
        git(self.repo, "config", "user.name", "Commit CI tests")
        git(self.repo, "config", "user.email", "ci@gopulse.invalid")
        # Tests must be independent of the developer's global hooks/signing.
        git(self.repo, "config", "core.hooksPath", ".githooks")
        git(self.repo, "config", "commit.gpgSign", "false")
        self.legacy = self.commit("Legacy title without a conventional prefix")
        self.write_policy()
        self.policy = read_policy(self.repo)

    def write(self, name: str, content: str) -> Path:
        path = self.repo / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding="utf-8")
        return path

    def write_policy(self, **overrides: object) -> None:
        data = {"types": list(TYPES), "legacy_ref": self.legacy, **overrides}
        self.write(".conventional-commits.json", json.dumps(data))

    def commit(self, message: str) -> str:
        git(self.repo, "commit", "--quiet", "--allow-empty", "-m", message)
        return git(self.repo, "rev-parse", "HEAD").strip()

    def cli(self, *args: str, event_name: str | None = None, event: dict | None = None) -> subprocess.CompletedProcess:
        environment = {name: value for name, value in os.environ.items() if not name.startswith("GITHUB_")}
        if event_name:
            path = self.write("event.json", json.dumps(event))
            environment.update(GITHUB_EVENT_NAME=event_name, GITHUB_EVENT_PATH=str(path))
        return subprocess.run(
            [sys.executable, str(ROOT / "ci/commits.py"), "--repo", str(self.repo), *args],
            text=True, capture_output=True, env=environment,
        )


class PolicyTests(RepositoryFixture):
    def test_policy_is_read_from_repository(self) -> None:
        self.assertEqual(self.policy, Policy(TYPES, self.legacy))

    def test_invalid_types(self) -> None:
        for types in ([], ["fix", "fix"], ["Fix"], [123], "fix", [{}]):
            with self.subTest(types=types):
                self.write_policy(types=types)
                with self.assertRaises(ValueError):
                    read_policy(self.repo)

    def test_baseline_requires_full_sha(self) -> None:
        for legacy_ref in ("main", "abc123", "z" * 40, 123):
            with self.subTest(legacy_ref=legacy_ref):
                self.write_policy(legacy_ref=legacy_ref)
                with self.assertRaises(ValueError):
                    read_policy(self.repo)


class RangeTests(RepositoryFixture):
    def test_old_history_is_exempt(self) -> None:
        self.assertEqual(commit_ids(self.repo, self.policy), [])
        result = self.cli()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Checked 0 new", result.stdout)

    def test_new_commits_are_all_checked(self) -> None:
        first = self.commit("fix: first change")
        second = self.commit("Bad new message")
        third = self.commit("test: last change")
        self.assertEqual(commit_ids(self.repo, self.policy), [first, second, third])
        result = self.cli("--base-ref", self.legacy)
        self.assertEqual(result.returncode, 1)
        self.assertIn(second[:12], result.stderr)
        self.assertIn("Checked 3 new", result.stdout)

    def test_base_excludes_already_accepted_commits(self) -> None:
        base = self.commit("A previous commit not in this change")
        head = self.commit("fix: current change")
        self.assertEqual(commit_ids(self.repo, self.policy, base), [head])
        self.assertEqual(self.cli("--base-ref", base).returncode, 0)

    def test_actual_merges_are_exempt_but_source_commits_are_checked(self) -> None:
        git(self.repo, "checkout", "--quiet", "-b", "feature")
        source = self.commit("Invalid feature commit")
        git(self.repo, "checkout", "--quiet", "main")
        main = self.commit("ci: another change")
        git(self.repo, "merge", "--quiet", "--no-ff", "feature", "-m", "Merge feature into main")
        merge = git(self.repo, "rev-parse", "HEAD").strip()
        revisions = commit_ids(self.repo, self.policy)
        self.assertEqual(set(revisions), {source, main})
        self.assertNotIn(merge, revisions)
        result = self.cli()
        self.assertEqual(result.returncode, 1)
        self.assertIn(source[:12], result.stderr)
        self.assertNotIn(merge[:12], result.stderr)

    def test_plain_commit_with_merge_title_is_not_exempt(self) -> None:
        self.commit("Merge feature into main")
        self.assertEqual(self.cli().returncode, 1)

    def test_missing_refs_fail_instead_of_skipping(self) -> None:
        self.assertEqual(self.cli("--base-ref", "missing-branch").returncode, 1)
        self.write_policy(legacy_ref="f" * 40)
        self.assertEqual(self.cli().returncode, 1)

    def test_nonancestor_base_excludes_only_its_own_history(self) -> None:
        git(self.repo, "checkout", "--quiet", "-b", "feature")
        feature = self.commit("feat: new endpoint")
        git(self.repo, "checkout", "--quiet", "main")
        base = self.commit("Base branch change outside the PR")
        self.assertEqual(commit_ids(self.repo, self.policy, base, feature), [feature])


class EventTests(RepositoryFixture):
    def pr_event(self, title: str, head: str | None = None) -> dict:
        return {"pull_request": {"base": {"sha": self.legacy}, "head": {"sha": head or self.legacy}, "title": title}}

    def test_pr_title_checked_even_without_new_commits(self) -> None:
        result = self.cli(event_name="pull_request", event=self.pr_event("Update documentation"))
        self.assertEqual(result.returncode, 1)
        self.assertIn("PR title:", result.stderr)
        self.assertIn("Checked 0 new", result.stdout)

    def test_valid_pr_title_and_commits(self) -> None:
        head = self.commit("fix(frontend): 修复跳转")
        result = self.cli(event_name="pull_request", event=self.pr_event("fix: 修复跳转", head))
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_push_checks_every_commit_not_just_tip(self) -> None:
        bad = self.commit("Invalid middle commit")
        head = self.commit("fix: tip")
        result = self.cli(event_name="push", event={"before": self.legacy, "after": head})
        self.assertEqual(result.returncode, 1)
        self.assertIn(bad[:12], result.stderr)
        self.assertIn("Checked 2 new", result.stdout)

    def test_initial_push_and_missing_old_sha_check_all_new_history(self) -> None:
        head = self.commit("Invalid newly reachable commit")
        for base in ("0" * 40, "f" * 40):
            with self.subTest(base=base):
                event = {"before": base, "after": head}
                self.assertEqual(event_range(self.repo, "push", event), (None, head, None))
                result = self.cli(event_name="push", event=event)
                self.assertEqual(result.returncode, 1)
                self.assertIn(head[:12], result.stderr)

    def test_merge_group_range(self) -> None:
        head = self.commit("ci: update workflow")
        event = {"merge_group": {"base_sha": self.legacy, "head_sha": head}}
        self.assertEqual(event_range(self.repo, "merge_group", event), (self.legacy, head, None))
        result = self.cli(event_name="merge_group", event=event)
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_manual_event_checks_new_history(self) -> None:
        self.commit("Invalid new title")
        self.assertEqual(self.cli(event_name="workflow_dispatch", event={}).returncode, 1)

    def test_unknown_or_malformed_events_fail_closed(self) -> None:
        for name in ("unknown", "pull_request", "push", "merge_group"):
            with self.subTest(name=name):
                result = self.cli(event_name=name, event={})
                self.assertEqual(result.returncode, 1)
                self.assertIn("validation failed", result.stderr)

    def test_pr_title_is_data_not_shell_code(self) -> None:
        title = "fix: $(touch SHOULD_NOT_EXIST) `touch ALSO_SHOULD_NOT_EXIST`"
        result = self.cli(event_name="pull_request", event=self.pr_event(title))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse((self.repo / "SHOULD_NOT_EXIST").exists())
        self.assertFalse((self.repo / "ALSO_SHOULD_NOT_EXIST").exists())


class HookTests(RepositoryFixture):
    def install_hook(self) -> None:
        for relative in (".githooks/commit-msg", "ci/commits.py"):
            target = self.repo / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(ROOT / relative, target)
        (self.repo / ".githooks/commit-msg").chmod(0o755)

    def test_real_hook_rejects_invalid_commit_and_accepts_valid_one(self) -> None:
        self.install_hook()
        rejected = subprocess.run(
            ["git", "commit", "--quiet", "--allow-empty", "-m", "Invalid title"],
            cwd=self.repo, text=True, capture_output=True,
        )
        self.assertNotEqual(rejected.returncode, 0)
        self.assertIn("Commit format error:", rejected.stderr)
        self.assertEqual(git(self.repo, "rev-parse", "HEAD").strip(), self.legacy)
        self.assertNotEqual(self.commit("fix(frontend): 修复跳转"), self.legacy)

    def test_editor_comments_and_trailing_whitespace_use_git_cleanup(self) -> None:
        message = self.write("message with spaces.txt", "# Editor hint\nfix: a change  \n\nBody.  \n# Another hint\n")
        self.assertEqual(check_file(self.repo, message, self.policy), [])
        result = self.cli("--message-file", str(message))
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_custom_comment_character(self) -> None:
        git(self.repo, "config", "core.commentChar", ";")
        message = self.write("message.txt", "; Editor hint\nfix: a change\n\n; Another hint\n")
        self.assertEqual(check_file(self.repo, message, self.policy), [])

    def test_blank_message_is_rejected(self) -> None:
        message = self.write("message.txt", "\n# Only a comment\n")
        self.assertEqual(self.cli("--message-file", str(message)).returncode, 1)

    def test_git_no_verify_does_not_bypass_ci(self) -> None:
        self.install_hook()
        git(self.repo, "commit", "--quiet", "--allow-empty", "--no-verify", "-m", "Invalid title")
        self.assertEqual(self.cli().returncode, 1)

    def test_real_merge_hook_is_exempt(self) -> None:
        self.install_hook()
        git(self.repo, "checkout", "--quiet", "-b", "feature")
        self.commit("feat: feature change")
        git(self.repo, "checkout", "--quiet", "main")
        self.commit("ci: main change")
        git(self.repo, "merge", "--quiet", "--no-ff", "feature", "-m", "Merge feature into main")
        self.assertEqual(self.cli().returncode, 0)


if __name__ == "__main__":
    unittest.main()

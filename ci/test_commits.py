from __future__ import annotations

import json
import os
import shlex
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from commits import Policy, check_file, commit_ids, event_range, git, read_policy, validate_message, validate_pr_body, validate_title


ROOT = Path(__file__).resolve().parents[1]
TYPES = ("build", "chore", "ci", "feat", "fix", "perf", "refactor", "revert", "style", "test")
POLICY = Policy(TYPES, "a" * 40, "b" * 40, "c" * 40)
BODY = "背景：\n重复请求会增加计数。\n\n改动：\n仅在新增记录后更新计数。\n\n验证：\ngo test ./...：通过。\n"
PR_BODY = BODY + "\n影响与风险：\n涉及点赞接口，无数据库结构变更。\n"


def structured_message(title: str) -> str:
    return f"{title}\n\n{BODY}"


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
            "ci(文档): 更新提交校验及用法 (#123)",
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

    def test_docs_types_are_rejected_with_actionable_guidance(self) -> None:
        for title in (
            "docs: clarify policy", "docs(ci): clarify policy",
            "docs!: change policy", "docs(文档)!: 更新规范",
        ):
            with self.subTest(title=title):
                self.assertIn("文档修改并入对应改动", validate_title(title, POLICY)[0])
                self.assertTrue(validate_message(structured_message(title), POLICY))
                self.assertTrue(validate_message(title, POLICY, require_body=False))

    def test_legacy_docs_still_require_valid_titles_and_bodies(self) -> None:
        self.assertEqual(validate_message(structured_message("docs(ci): clarify policy"), POLICY, allow_legacy_docs=True), [])
        self.assertTrue(validate_message("docs: clarify policy", POLICY, allow_legacy_docs=True))
        self.assertTrue(validate_title("docs: ", POLICY, allow_legacy_docs=True))
        self.assertTrue(validate_title("unknown: clarify policy", POLICY, allow_legacy_docs=True))

    def test_body_requires_blank_separator(self) -> None:
        self.assertTrue(validate_message("fix: a change\nBody without separator\n", POLICY))
        self.assertEqual(validate_message(structured_message("fix: a change"), POLICY), [])

    def test_breaking_footer(self) -> None:
        for footer in ("BREAKING CHANGE: migrate clients", "BREAKING-CHANGE: 更新配置"):
            with self.subTest(footer=footer):
                self.assertEqual(validate_message(structured_message("feat: a change") + f"\n{footer}\n", POLICY), [])

    def test_invalid_breaking_footer(self) -> None:
        for footer in ("breaking change: migrate", "Breaking-Change: migrate", "BREAKING CHANGE:", "BREAKING CHANGE: "):
            with self.subTest(footer=footer):
                self.assertTrue(validate_message(structured_message("feat: a change") + f"\n{footer}\n", POLICY))

    def test_bang_does_not_require_breaking_footer(self) -> None:
        self.assertEqual(validate_message(structured_message("feat!: change defaults"), POLICY), [])

    def test_trailers_and_long_descriptions_are_allowed(self) -> None:
        message = structured_message("fix: " + "描述" * 100) + "\nCloses: #123\nSigned-off-by: A <a@example.test>\n"
        self.assertEqual(validate_message(message, POLICY), [])

    def test_new_messages_require_all_sections(self) -> None:
        for body in ("", "Only a description.", BODY.replace("改动：", "内容：")):
            with self.subTest(body=body):
                self.assertTrue(validate_message(f"fix: a change\n\n{body}", POLICY))

    def test_legacy_body_remains_optional(self) -> None:
        self.assertEqual(validate_message("fix: a change\n", POLICY, require_body=False), [])
        self.assertTrue(validate_message("Invalid title\n", POLICY, require_body=False))

    def test_english_and_inline_sections(self) -> None:
        message = "fix: count once\n\nBackground: duplicate requests inflate counts\nChanges: insert once\nValidation: go test ./... passed\n"
        self.assertEqual(validate_message(message, POLICY), [])

    def test_unrun_validation_requires_an_explanation_instead_of_a_placeholder(self) -> None:
        message = structured_message("ci: clarify the policy")
        for explanation in ("未执行：仅修改文档，未运行产品测试。", "不适用：没有可执行代码，已人工检查链接。"):
            with self.subTest(explanation=explanation):
                self.assertEqual(validate_message(message.replace("go test ./...：通过。", explanation), POLICY), [])


class PRBodyTests(unittest.TestCase):
    def test_chinese_and_english_markdown_sections(self) -> None:
        for body in (
            "## 背景\n重复请求。\n## 改动\n仅新增一次。\n## 验证\n单测通过。\n## 影响与风险\n无迁移要求。",
            "## Background\nDuplicate requests.\n## Changes\nInsert once.\n## Validation\nTests passed.\n## Impact and risks\nNo migration required.",
            PR_BODY,
        ):
            with self.subTest(body=body):
                self.assertEqual(validate_pr_body(body), [])

    def test_missing_empty_and_placeholder_sections_are_rejected(self) -> None:
        for body in (None, "", BODY, "## 背景\n## 改动\n## 验证\n## 影响与风险"):
            with self.subTest(body=body):
                self.assertTrue(validate_pr_body(body))
        for placeholder in ("", "TODO", "TBD", "待补充", "- [ ]", "- **待填写**", "未执行", "不适用", "..."):
            with self.subTest(placeholder=placeholder):
                self.assertTrue(validate_pr_body(PR_BODY.replace("go test ./...：通过。", placeholder)))

    def test_comments_and_code_examples_do_not_supply_missing_sections(self) -> None:
        for body in (
            f"<!-- {PR_BODY} -->",
            f"```markdown\n{PR_BODY}\n```",
            f"~~~~markdown\n{PR_BODY}\n~~~~",
            "## 背景\n<!-- hint -->\n## 改动\n<!-- hint -->\n## 验证\n<!-- hint -->\n## 影响与风险\n<!-- hint -->",
            "## 背景\n<!-- unfinished hint\n" + PR_BODY,
        ):
            with self.subTest(body=body):
                self.assertTrue(validate_pr_body(body))

    def test_content_under_another_heading_does_not_fill_an_empty_section(self) -> None:
        body = PR_BODY.replace("go test ./...：通过。", "## 其他\n单测通过。")
        self.assertTrue(validate_pr_body(body))

    def test_validation_can_include_a_fenced_command(self) -> None:
        body = PR_BODY.replace("go test ./...：通过。", "```sh\ngo test ./...\n```\n结果：通过。")
        self.assertEqual(validate_pr_body(body), [])

    def test_wrong_body_type_fails_validation(self) -> None:
        self.assertTrue(validate_pr_body(123))

    def test_unfilled_repository_templates_are_rejected(self) -> None:
        self.assertTrue(validate_pr_body((ROOT / ".github/pull_request_template.md").read_text()))
        message = git(ROOT, "stripspace", "--strip-comments", input_text=(ROOT / ".gitmessage").read_text())
        self.assertTrue(validate_message(message, POLICY))


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
        data = {
            "types": list(TYPES), "legacy_ref": self.legacy,
            "body_required_after": self.legacy, "docs_forbidden_after": self.legacy, **overrides,
        }
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
        self.assertEqual(self.policy, Policy(TYPES, self.legacy, self.legacy, self.legacy))

    def test_invalid_types(self) -> None:
        for types in ([], ["fix", "fix"], ["Fix"], [123], "fix", [{}]):
            with self.subTest(types=types):
                self.write_policy(types=types)
                with self.assertRaises(ValueError):
                    read_policy(self.repo)

    def test_baseline_requires_full_sha(self) -> None:
        for field in ("legacy_ref", "body_required_after", "docs_forbidden_after"):
            for value in ("main", "abc123", "z" * 40, 123):
                with self.subTest(field=field, value=value):
                    self.write_policy(**{field: value})
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
        head = self.commit(structured_message("fix: current change"))
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

    def test_body_policy_does_not_rewrite_existing_history(self) -> None:
        previous = self.commit("fix: existing title-only change")
        self.write_policy(body_required_after=previous)
        self.assertEqual(self.cli().returncode, 0)
        new = self.commit("fix: missing new body")
        result = self.cli()
        self.assertEqual(result.returncode, 1)
        self.assertIn(new[:12], result.stderr)
        self.assertNotIn(previous[:12], result.stderr)

    def test_body_baseline_does_not_exempt_invalid_old_titles(self) -> None:
        previous = self.commit("Invalid title after original policy adoption")
        self.write_policy(body_required_after=previous)
        result = self.cli()
        self.assertEqual(result.returncode, 1)
        self.assertIn(previous[:12], result.stderr)

    def test_missing_body_baseline_fails_instead_of_skipping(self) -> None:
        self.write_policy(body_required_after="f" * 40)
        self.assertEqual(self.cli().returncode, 1)

    def test_body_policy_also_applies_to_commits_from_a_diverged_branch(self) -> None:
        git(self.repo, "checkout", "--quiet", "-b", "feature")
        feature = self.commit("fix: missing feature body")
        git(self.repo, "checkout", "--quiet", "main")
        baseline = self.commit("ci: old main change")
        self.write_policy(body_required_after=baseline)
        result = self.cli("--base-ref", baseline, "--head-ref", feature)
        self.assertEqual(result.returncode, 1)
        self.assertIn(feature[:12], result.stderr)

    def test_docs_policy_preserves_existing_history_but_rejects_new_docs(self) -> None:
        previous = self.commit(structured_message("docs: existing documentation"))
        baseline = self.commit(structured_message("ci: previous change"))
        self.write_policy(docs_forbidden_after=baseline)
        result = self.cli()
        self.assertEqual(result.returncode, 0, result.stderr)
        new = self.commit(structured_message("docs(ci): new documentation"))
        self.commit(structured_message("fix: later code change"))
        result = self.cli()
        self.assertEqual(result.returncode, 1)
        self.assertIn(new[:12], result.stderr)
        self.assertNotIn(previous[:12], result.stderr)
        self.assertIn("文档修改并入对应改动", result.stderr)

    def test_docs_baseline_does_not_exempt_body_or_title_checks(self) -> None:
        previous = self.commit("docs: missing body")
        self.write_policy(docs_forbidden_after=previous)
        result = self.cli()
        self.assertEqual(result.returncode, 1)
        self.assertIn("required section", result.stderr)
        self.assertNotIn("文档修改并入对应改动", result.stderr)

    def test_docs_policy_also_applies_to_commits_from_a_diverged_branch(self) -> None:
        git(self.repo, "checkout", "--quiet", "-b", "feature")
        feature = self.commit(structured_message("docs: feature documentation"))
        git(self.repo, "checkout", "--quiet", "main")
        baseline = self.commit(structured_message("ci: main change"))
        self.write_policy(docs_forbidden_after=baseline)
        result = self.cli("--base-ref", baseline, "--head-ref", feature)
        self.assertEqual(result.returncode, 1)
        self.assertIn(feature[:12], result.stderr)

    def test_missing_docs_baseline_fails_instead_of_skipping(self) -> None:
        self.write_policy(docs_forbidden_after="f" * 40)
        self.assertEqual(self.cli().returncode, 1)


class EventTests(RepositoryFixture):
    def pr_event(self, title: str, head: str | None = None) -> dict:
        return {"pull_request": {"base": {"sha": self.legacy}, "head": {"sha": head or self.legacy}, "title": title, "body": PR_BODY}}

    def test_pr_title_checked_even_without_new_commits(self) -> None:
        result = self.cli(event_name="pull_request", event=self.pr_event("Update documentation"))
        self.assertEqual(result.returncode, 1)
        self.assertIn("PR title:", result.stderr)
        self.assertIn("Checked 0 new", result.stdout)

    def test_valid_pr_title_and_commits(self) -> None:
        head = self.commit(structured_message("fix(frontend): 修复跳转"))
        result = self.cli(event_name="pull_request", event=self.pr_event("fix: 修复跳转", head))
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_pr_body_checked_even_without_new_commits(self) -> None:
        for body in (None, "", BODY):
            with self.subTest(body=body):
                event = self.pr_event("ci: clarify policy")
                event["pull_request"]["body"] = body
                result = self.cli(event_name="pull_request", event=event)
                self.assertEqual(result.returncode, 1)
                self.assertIn("PR body:", result.stderr)
                self.assertIn("Checked 0 new", result.stdout)

    def test_editing_pr_body_rechecks_the_description(self) -> None:
        event = self.pr_event("ci: clarify policy")
        event["action"] = "edited"
        event["pull_request"]["body"] = "## 背景\nTODO"
        self.assertEqual(self.cli(event_name="pull_request", event=event).returncode, 1)
        event["pull_request"]["body"] = PR_BODY
        self.assertEqual(self.cli(event_name="pull_request", event=event).returncode, 0)

    def test_docs_pr_title_is_rejected_even_without_new_commits(self) -> None:
        result = self.cli(event_name="pull_request", event=self.pr_event("docs(ci): clarify policy"))
        self.assertEqual(result.returncode, 1)
        self.assertIn("PR title:", result.stderr)
        self.assertIn("文档修改并入对应改动", result.stderr)

    def test_valid_pr_title_does_not_hide_a_docs_commit(self) -> None:
        head = self.commit(structured_message("docs: standalone documentation"))
        result = self.cli(event_name="pull_request", event=self.pr_event("ci: update checks", head))
        self.assertEqual(result.returncode, 1)
        self.assertIn(head[:12], result.stderr)

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
        head = self.commit(structured_message("ci: update workflow"))
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

    def test_pr_body_is_data_not_shell_code(self) -> None:
        event = self.pr_event("fix: count once")
        event["pull_request"]["body"] = PR_BODY + "\n$(touch SHOULD_NOT_EXIST) `touch ALSO_SHOULD_NOT_EXIST`"
        result = self.cli(event_name="pull_request", event=event)
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
        self.assertNotEqual(self.commit(structured_message("fix(frontend): 修复跳转")), self.legacy)

    def test_real_hook_rejects_title_only_commit(self) -> None:
        self.install_hook()
        rejected = subprocess.run(
            ["git", "commit", "--quiet", "--allow-empty", "-m", "fix: missing explanation"],
            cwd=self.repo, text=True, capture_output=True,
        )
        self.assertNotEqual(rejected.returncode, 0)
        self.assertIn("required section", rejected.stderr)
        self.assertEqual(git(self.repo, "rev-parse", "HEAD").strip(), self.legacy)

    def test_real_hook_rejects_docs_even_with_a_complete_body(self) -> None:
        self.install_hook()
        message = structured_message("docs(ci)!: standalone documentation")
        rejected = subprocess.run(
            ["git", "commit", "--quiet", "--allow-empty", "-m", message],
            cwd=self.repo, text=True, capture_output=True,
        )
        self.assertNotEqual(rejected.returncode, 0)
        self.assertIn("文档修改并入对应改动", rejected.stderr)
        self.assertEqual(git(self.repo, "rev-parse", "HEAD").strip(), self.legacy)
        git(self.repo, "commit", "--quiet", "--allow-empty", "--no-verify", "-m", message)
        self.assertEqual(self.cli().returncode, 1)

    def test_editor_comments_and_trailing_whitespace_use_git_cleanup(self) -> None:
        message = self.write("message with spaces.txt", "# Editor hint\n" + structured_message("fix: a change  ") + "# Another hint\n")
        self.assertEqual(check_file(self.repo, message, self.policy), [])
        result = self.cli("--message-file", str(message))
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_custom_comment_character(self) -> None:
        git(self.repo, "config", "core.commentChar", ";")
        message = self.write("message.txt", "; Editor hint\n" + structured_message("fix: a change") + "; Another hint\n")
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
        self.commit(structured_message("feat: feature change"))
        git(self.repo, "checkout", "--quiet", "main")
        self.commit(structured_message("ci: main change"))
        git(self.repo, "merge", "--quiet", "--no-ff", "feature", "-m", "Merge feature into main")
        self.assertEqual(self.cli().returncode, 0)


class TemplateInstallationTests(RepositoryFixture):
    def setUp(self) -> None:
        super().setUp()
        for relative in ("Makefile", ".gitmessage", ".githooks/commit-msg", "ci/commits.py"):
            target = self.repo / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(ROOT / relative, target)

    def install(self) -> subprocess.CompletedProcess:
        return subprocess.run(["make", "hooks-install"], cwd=self.repo, text=True, capture_output=True)

    def test_install_sets_hook_and_template_and_is_repeatable(self) -> None:
        for _ in range(2):
            result = self.install()
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(git(self.repo, "config", "--get", "core.hooksPath").strip(), ".githooks")
            self.assertEqual(git(self.repo, "config", "--get", "commit.template").strip(), str(self.repo / ".gitmessage"))

    def test_install_preserves_custom_template_and_hook_path(self) -> None:
        for key, value in (("core.hooksPath", "custom-hooks"), ("commit.template", "custom-message.txt")):
            with self.subTest(key=key):
                git(self.repo, "config", key, value)
                result = self.install()
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("refusing to overwrite", result.stderr)
                self.assertEqual(git(self.repo, "config", "--get", key).strip(), value)
                git(self.repo, "config", "--unset", key)

    def test_commit_from_subdirectory_opens_the_template_and_runs_the_hook(self) -> None:
        result = self.install()
        self.assertEqual(result.returncode, 0, result.stderr)
        editor = self.write("test editor.py", (
            f"#!{sys.executable}\n"
            "import sys\nfrom pathlib import Path\n"
            "message = Path(sys.argv[1])\n"
            "assert '背景：' in message.read_text(encoding='utf-8')\n"
            f"message.write_text({structured_message('fix: count once')!r}, encoding='utf-8')\n"
        ))
        editor.chmod(0o755)
        directory = self.repo / "nested directory"
        directory.mkdir()
        environment = {**os.environ, "GIT_EDITOR": shlex.quote(str(editor))}
        result = subprocess.run(
            ["git", "commit", "--quiet", "--allow-empty"], cwd=directory,
            text=True, capture_output=True, env=environment,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.cli().returncode, 0)


if __name__ == "__main__":
    unittest.main()

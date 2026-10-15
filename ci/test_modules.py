from __future__ import annotations

import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from modules import changed_files, command, event_labels, read_modules, select, select_changed, split_labels, write_workspace


class ModuleFixture(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.repo = Path(self.temporary.name)
        self.go_modules = ["shared", "api", "worker", "exporters/probe", "devtools"]
        self.npm_modules = ["frontend", "admin-frontend"]
        self.write("Makefile", (
            "list-go-modules:\n\t@echo " + " ".join(self.go_modules)
            + "\nlist-npm-modules:\n\t@echo " + " ".join(self.npm_modules) + "\n"
        ))
        for name in self.go_modules:
            self.write(f"{name}/go.mod", f"module example.test/{name}\n\ngo 1.26\n")
            self.write(f"{name}/Makefile", "test:\n\t@true\n")
        for name in self.npm_modules:
            self.write(f"{name}/package.json", "{}\n")
            self.write(f"{name}/Makefile", "test:\n\t@true\n")
        self.write("api/go.mod", "module example.test/api\n\ngo 1.26\n\nrequire (\n example.test/shared v0.0.0 // local library\n)\n")
        self.write("worker/go.mod", "module example.test/worker\n\ngo 1.26\n\nrequire example.test/api v0.0.0\n")

    def write(self, name: str, content: str) -> None:
        target = self.repo / name
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(content, encoding="utf-8")

    def selected(self, labels: list[str]) -> set[str]:
        return set(select(self.repo, labels)["modules"])


class ModuleTests(ModuleFixture):
    def test_no_modules_requires_an_explicit_none_label(self) -> None:
        plan = select(self.repo, ["ci:none", "documentation"])
        self.assertEqual(plan["matrix"], {"include": []})
        self.assertFalse(plan["has_modules"])

    def test_single_label_selects_exactly_one_module(self) -> None:
        self.assertEqual(self.selected(["ci:exporters/probe"]), {"exporters/probe"})

    def test_shared_module_label_does_not_select_consumers(self) -> None:
        self.assertEqual(self.selected(["ci:shared"]), {"shared"})

    def test_consumer_label_does_not_select_dependencies(self) -> None:
        self.assertEqual(self.selected(["ci:worker"]), {"worker"})

    def test_replace_dependencies_do_not_expand_selection(self) -> None:
        self.write("exporters/probe/go.mod", (
            "module example.test/probe\n\ngo 1.26\n\n"
            "replace (\n example.test/other => ../../shared\n)\n"
        ))
        self.assertEqual(self.selected(["ci:shared"]), {"shared"})

    def test_dependency_cycles_do_not_expand_selection(self) -> None:
        self.write("shared/go.mod", "module example.test/shared\n\ngo 1.26\n\nrequire example.test/worker v0.0.0\n")
        self.assertEqual(self.selected(["ci:worker"]), {"worker"})

    def test_multiple_labels_select_only_named_modules_in_registry_order(self) -> None:
        plan = select(self.repo, ["ci:worker", "bug", "ci:api", "ci:worker"])
        self.assertEqual(plan["modules"], ["api", "worker"])
        self.assertEqual(plan["labels"], ["ci:api", "ci:worker"])

    def test_development_helper_does_not_select_frontends(self) -> None:
        self.assertEqual(self.selected(["ci:devtools"]), {"devtools"})

    def test_frontend_manifests_and_makefiles_are_not_required_by_ci(self) -> None:
        for name in self.npm_modules:
            (self.repo / name / "package.json").unlink()
            (self.repo / name / "Makefile").unlink()
        self.assertEqual(self.selected(["ci:api"]), {"api"})

    def test_frontend_and_unknown_ci_labels_fail_instead_of_selecting_all(self) -> None:
        for label in ("ci:frontend", "ci:admin-frontend", "ci:all", "ci:ap", "ci:", "ci:api-extra", "ci:*"):
            with self.subTest(label=label), self.assertRaises(ValueError):
                select(self.repo, ["ci:api", label])

    def test_missing_ci_labels_fail_instead_of_skipping(self) -> None:
        for labels in ([], ["bug"], ["frontend"], ["CI:api"]):
            with self.subTest(labels=labels), self.assertRaises(ValueError):
                select(self.repo, labels)

    def test_none_cannot_be_combined_with_module_labels(self) -> None:
        with self.assertRaises(ValueError):
            select(self.repo, ["ci:none", "ci:api"])

    def test_labels_are_not_interpreted_as_shell_or_glob_patterns(self) -> None:
        for label in ("ci:api$(touch SHOULD_NOT_EXIST)", "ci:api\nci:worker", "ci:../api"):
            with self.subTest(label=label), self.assertRaises(ValueError):
                select(self.repo, [label])
        self.assertFalse((self.repo / "SHOULD_NOT_EXIST").exists())

    @patch("modules.go_manifests", side_effect=AssertionError("label selection must not parse dependencies"))
    def test_selection_does_not_parse_dependency_graph(self, _manifests) -> None:
        self.assertEqual(self.selected(["ci:api"]), {"api"})

    def test_matrix_contains_only_go_modules_and_their_go_versions(self) -> None:
        matrix = select(self.repo, ["ci:worker"])["matrix"]["include"]
        self.assertEqual(matrix, [
            {"module": "worker", "go_mod": "worker/go.mod"},
        ])

    def test_module_registry_comes_only_from_make(self) -> None:
        self.write("extra/go.mod", "module example.test/extra\n\ngo 1.26\n")
        self.write("extra/Makefile", "test:\n\t@true\n")
        makefile = (self.repo / "Makefile").read_text()
        self.write("Makefile", makefile.replace("shared api", "shared extra api"))
        self.assertEqual(self.selected(["ci:extra"]), {"extra"})

    def test_duplicate_module_registration_is_an_error(self) -> None:
        self.write("Makefile", "list-go-modules:\n\t@echo api api\nlist-npm-modules:\n\t@echo frontend\n")
        with self.assertRaises(ValueError):
            read_modules(self.repo)

    def test_invalid_module_paths_are_rejected(self) -> None:
        self.write("Makefile", "list-go-modules:\n\t@echo ../api\nlist-npm-modules:\n\t@echo frontend\n")
        with self.assertRaises(ValueError):
            read_modules(self.repo)

    def test_missing_manifest_fails_closed(self) -> None:
        (self.repo / "api/go.mod").unlink()
        with self.assertRaises(ValueError):
            select(self.repo, ["ci:api"])

    def test_local_diff_checks_only_directly_changed_modules(self) -> None:
        paths = ["shared/metrics.go", "api/go.mod", "Makefile", ".github/workflows/ci.yml"]
        self.assertEqual(select_changed(self.repo, paths)["modules"], ["shared", "api"])

    def test_local_unknown_and_frontend_paths_never_select_everything(self) -> None:
        paths = ["api-extra/main.go", "frontend/src/main.ts", "frontend-shared/tokens.css", "new-module/main.go", "deploy/compose.yaml", "docs/design.md", "api/README.md"]
        plan = select_changed(self.repo, paths)
        self.assertFalse(plan["has_modules"])

    def test_local_deleted_sources_and_runtime_text_select_their_modules(self) -> None:
        self.assertEqual(select_changed(self.repo, ["worker/deleted.go", "exporters/probe/template.txt"])["modules"], ["worker", "exporters/probe"])

    def test_workspace_uses_current_checkout_without_touching_user_workspace(self) -> None:
        self.write("go.work", "developer workspace, do not change\n")
        workspace = write_workspace(self.repo)
        self.assertEqual(workspace, self.repo / ".run/ci/go.work")
        self.assertEqual((self.repo / "go.work").read_text(), "developer workspace, do not change\n")
        for name in self.go_modules:
            self.assertIn(str(self.repo / name), workspace.read_text())
        environment = {**os.environ, "GOWORK": str(workspace)}
        result = subprocess.run(
            ["go", "list", "-m", "-f", "{{.Dir}}", "example.test/shared"],
            cwd=self.repo / "api", env=environment, text=True, capture_output=True, check=True,
        )
        self.assertEqual(result.stdout.strip(), str(self.repo / "shared"))

    def test_unchanged_workspace_is_not_rewritten(self) -> None:
        workspace = write_workspace(self.repo)
        modified = workspace.stat().st_mtime_ns
        write_workspace(self.repo)
        self.assertEqual(workspace.stat().st_mtime_ns, modified)


class GitChangesTests(ModuleFixture):
    def setUp(self) -> None:
        super().setUp()
        command(self.repo, "git", "init", "--quiet", "--initial-branch=main")
        command(self.repo, "git", "config", "user.name", "Module CI tests")
        command(self.repo, "git", "config", "user.email", "ci@gopulse.invalid")
        self.base = self.commit("initial")

    def commit(self, message: str) -> str:
        command(self.repo, "git", "add", ".")
        command(self.repo, "git", "commit", "--quiet", "-m", message)
        return command(self.repo, "git", "rev-parse", "HEAD").strip()

    def test_pull_request_diff_excludes_base_branch_changes(self) -> None:
        command(self.repo, "git", "checkout", "--quiet", "-b", "feature")
        self.write("api/main.go", "package api\n")
        head = self.commit("api change")
        command(self.repo, "git", "checkout", "--quiet", "main")
        self.write("worker/main.go", "package worker\n")
        base = self.commit("unrelated base change")
        self.assertEqual(changed_files(self.repo, base, head), ["api/main.go"])

    def test_cross_module_rename_selects_both_paths(self) -> None:
        self.write("api/move.go", "package example\n")
        before = self.commit("source")
        command(self.repo, "git", "mv", "api/move.go", "worker/move.go")
        after = self.commit("move")
        self.assertEqual(changed_files(self.repo, before, after), ["api/move.go", "worker/move.go"])

    def test_multiple_push_commits_are_all_included(self) -> None:
        self.write("api/a.go", "package api\n")
        self.commit("api")
        self.write("worker/b.go", "package worker\n")
        head = self.commit("worker")
        self.assertEqual(changed_files(self.repo, self.base, head, merge_base=False), ["api/a.go", "worker/b.go"])

    def test_deleted_files_are_included(self) -> None:
        (self.repo / "worker/Makefile").unlink()
        head = self.commit("delete")
        self.assertEqual(changed_files(self.repo, self.base, head), ["worker/Makefile"])

    def test_missing_local_base_is_an_error(self) -> None:
        with self.assertRaises(subprocess.CalledProcessError):
            changed_files(self.repo, "does-not-exist")


class CLITests(ModuleFixture):
    def run_cli(self, *args: str, extra_environment: dict | None = None) -> subprocess.CompletedProcess:
        environment = {key: value for key, value in os.environ.items() if not key.startswith("GITHUB_") and key != "GOPULSE_CI_LABELS"}
        environment.update(extra_environment or {})
        return subprocess.run(
            ["python3", str(Path(__file__).with_name("modules.py")), "plan", "--repo", str(self.repo), *args],
            text=True, capture_output=True, env=environment,
        )

    def test_explicit_label_plan_is_valid_json(self) -> None:
        result = self.run_cli("--labels", "ci:api, ci:worker")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(result.stdout)["modules"], ["api", "worker"])

    def test_missing_local_labels_fail_with_no_plan(self) -> None:
        result = self.run_cli()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")

    def test_make_label_environment_is_validated_without_shell_interpolation(self) -> None:
        result = self.run_cli(extra_environment={"GOPULSE_CI_LABELS": "ci:worker ci:api"})
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(result.stdout)["modules"], ["api", "worker"])
        result = self.run_cli(extra_environment={"GOPULSE_CI_LABELS": "ci:api$(touch SHOULD_NOT_EXIST)"})
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.repo / "SHOULD_NOT_EXIST").exists())

    def test_all_fallback_is_not_supported(self) -> None:
        result = self.run_cli("--all")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")

    def test_github_output_is_appended_and_manual_event_selects_only_its_labels(self) -> None:
        self.write("event.json", json.dumps({"inputs": {"labels": "ci:worker,ci:api"}}))
        self.write("output.txt", "existing=value\n")
        result = self.run_cli(extra_environment={
            "GITHUB_EVENT_NAME": "workflow_dispatch",
            "GITHUB_EVENT_PATH": str(self.repo / "event.json"),
            "GITHUB_OUTPUT": str(self.repo / "output.txt"),
        })
        self.assertEqual(result.returncode, 0, result.stderr)
        lines = (self.repo / "output.txt").read_text().splitlines()
        self.assertEqual(lines[0], "existing=value")
        outputs = dict(line.split("=", 1) for line in lines[1:])
        self.assertEqual(outputs["has_modules"], "true")
        self.assertEqual(json.loads(outputs["modules"]), ["api", "worker"])
        self.assertEqual(len(json.loads(outputs["matrix"])["include"]), 2)

    def test_pr_labeled_and_unlabeled_events_use_the_entire_current_label_list(self) -> None:
        for action, labels, expected in (
            ("labeled", ["ci:api", "ci:worker"], ["api", "worker"]),
            ("unlabeled", ["ci:worker"], ["worker"]),
            ("synchronize", ["ci:shared"], ["shared"]),
        ):
            with self.subTest(action=action):
                event = {"action": action, "label": {"name": "ci:api"}, "pull_request": {"labels": [{"name": name} for name in labels]}}
                self.write("event.json", json.dumps(event))
                result = self.run_cli(extra_environment={"GITHUB_EVENT_NAME": "pull_request", "GITHUB_EVENT_PATH": str(self.repo / "event.json")})
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(json.loads(result.stdout)["modules"], expected)

    def test_none_label_produces_empty_matrix_and_false_output(self) -> None:
        self.write("output.txt", "")
        result = self.run_cli("--labels", "ci:none", extra_environment={"GITHUB_OUTPUT": str(self.repo / "output.txt")})
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(result.stdout)["matrix"], {"include": []})
        self.assertIn("has_modules=false\n", (self.repo / "output.txt").read_text())

    def test_invalid_labels_do_not_publish_a_successful_plan(self) -> None:
        self.write("output.txt", "existing=value\n")
        result = self.run_cli("--labels", "ci:typo", extra_environment={"GITHUB_OUTPUT": str(self.repo / "output.txt")})
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")
        self.assertEqual((self.repo / "output.txt").read_text(), "existing=value\n")

    def test_missing_labels_on_github_fail_closed(self) -> None:
        for name, event in (("pull_request", {"pull_request": {"labels": []}}), ("workflow_dispatch", {"inputs": {"labels": ""}})):
            with self.subTest(name=name):
                self.write("event.json", json.dumps(event))
                result = self.run_cli(extra_environment={"GITHUB_EVENT_NAME": name, "GITHUB_EVENT_PATH": str(self.repo / "event.json")})
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(result.stdout, "")


class EventTests(unittest.TestCase):
    def test_manual_labels_accept_commas_spaces_and_newlines(self) -> None:
        value = "ci:api, ci:worker\nci:devtools"
        self.assertEqual(split_labels(value), ["ci:api", "ci:worker", "ci:devtools"])
        self.assertEqual(event_labels("workflow_dispatch", {"inputs": {"labels": value}}), split_labels(value))

    def test_pr_label_names_are_read_as_data(self) -> None:
        self.assertEqual(event_labels("pull_request", {"pull_request": {"labels": [{"name": "ci:api"}, {"name": "bug"}]}}), ["ci:api", "bug"])

    def test_unknown_event_is_not_silently_skipped(self) -> None:
        for name in ("unexpected", "push", "merge_group"):
            with self.subTest(name=name), self.assertRaises(ValueError):
                event_labels(name, {})

    def test_invalid_pr_payload_is_not_silently_skipped(self) -> None:
        with self.assertRaises(KeyError):
            event_labels("pull_request", {})

    def test_malformed_label_payloads_fail_closed(self) -> None:
        for labels in (None, "ci:api", ["ci:api"], [{"name": 123}], [{}]):
            with self.subTest(labels=labels), self.assertRaises(ValueError):
                event_labels("pull_request", {"pull_request": {"labels": labels}})
        with self.assertRaises(ValueError):
            event_labels("workflow_dispatch", {"inputs": {"labels": None}})


if __name__ == "__main__":
    unittest.main()

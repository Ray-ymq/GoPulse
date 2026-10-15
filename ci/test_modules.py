from __future__ import annotations

import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from modules import changed_files, command, event_changes, read_modules, select, write_workspace


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

    def selected(self, paths: list[str] | None) -> set[str]:
        return set(select(self.repo, paths)["modules"])


class ModuleTests(ModuleFixture):
    def test_empty_diff_selects_no_modules(self) -> None:
        plan = select(self.repo, [])
        self.assertEqual(plan["matrix"], {"include": []})
        self.assertFalse(plan["has_modules"])

    def test_documentation_only_selects_no_modules(self) -> None:
        self.assertEqual(self.selected(["docs/design.md", "README.md", "api/README.md", "LICENSE"]), set())

    def test_single_independent_module(self) -> None:
        self.assertEqual(self.selected(["exporters/probe/main.go"]), {"exporters/probe"})

    def test_shared_source_selects_transitive_consumers(self) -> None:
        self.assertEqual(self.selected(["shared/metrics.go"]), {"shared", "api", "worker"})

    def test_module_manifest_selects_consumers(self) -> None:
        self.assertEqual(self.selected(["api/go.mod"]), {"api", "worker"})

    def test_block_replace_resolves_local_paths(self) -> None:
        self.write("exporters/probe/go.mod", (
            "module example.test/probe\n\ngo 1.26\n\n"
            "replace (\n example.test/other => ../../shared\n)\n"
        ))
        self.assertEqual(self.selected(["shared/metrics.go"]), {"shared", "api", "worker", "exporters/probe"})

    def test_dependency_cycles_terminate(self) -> None:
        self.write("shared/go.mod", "module example.test/shared\n\ngo 1.26\n\nrequire example.test/worker v0.0.0\n")
        self.assertEqual(self.selected(["worker/main.go"]), {"shared", "api", "worker"})

    def test_frontends_are_independent(self) -> None:
        self.assertEqual(self.selected(["frontend/src/main.ts"]), {"frontend"})
        self.assertEqual(self.selected(["admin-frontend/package-lock.json"]), {"admin-frontend"})

    def test_shared_frontend_code_selects_only_both_frontends(self) -> None:
        self.assertEqual(self.selected(["frontend-shared/ProductState.vue"]), {"frontend", "admin-frontend"})

    def test_development_helper_also_selects_frontends(self) -> None:
        self.assertEqual(self.selected(["devtools/internal/frontenddeps/deps.go"]), {"devtools", "frontend", "admin-frontend"})

    def test_shared_and_unknown_paths_select_every_module(self) -> None:
        expected = set(self.go_modules + self.npm_modules)
        for path in ("Makefile", "make/go-lint.mk", ".github/workflows/ci.yml", "ci/modules.py", ".golangci.yml", "deploy/compose.yaml", "new-module/main.go", "VERSION"):
            with self.subTest(path=path):
                self.assertEqual(self.selected([path]), expected)

    def test_path_prefix_is_not_a_module_match(self) -> None:
        self.assertEqual(self.selected(["api-extra/main.go"]), set(self.go_modules + self.npm_modules))

    def test_runtime_text_files_are_not_treated_as_documentation(self) -> None:
        self.assertEqual(self.selected(["exporters/probe/template.txt"]), {"exporters/probe"})

    def test_deleted_source_paths_still_select_their_module(self) -> None:
        self.assertFalse((self.repo / "worker/deleted.go").exists())
        self.assertEqual(self.selected(["worker/deleted.go"]), {"worker"})

    def test_duplicate_paths_do_not_duplicate_matrix_entries(self) -> None:
        self.assertEqual(select(self.repo, ["worker/a.go", "worker/a.go"])["modules"], ["worker"])

    def test_manual_selection_includes_every_registered_module(self) -> None:
        self.assertEqual(self.selected(None), set(self.go_modules + self.npm_modules))

    def test_matrix_uses_module_go_version_and_frontend_helper(self) -> None:
        matrix = select(self.repo, ["frontend/src/main.ts", "worker/a.go"])["matrix"]["include"]
        self.assertEqual(matrix, [
            {"module": "worker", "kind": "go", "go_mod": "worker/go.mod"},
            {"module": "frontend", "kind": "npm", "go_mod": "devtools/go.mod"},
        ])

    def test_module_registry_comes_only_from_make(self) -> None:
        self.write("extra/go.mod", "module example.test/extra\n\ngo 1.26\n")
        self.write("extra/Makefile", "test:\n\t@true\n")
        makefile = (self.repo / "Makefile").read_text()
        self.write("Makefile", makefile.replace("shared api", "shared extra api"))
        self.assertIn("extra", self.selected(None))

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
            select(self.repo, ["api/main.go"])

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
        self.assertEqual(event_changes(self.repo, "push", {"before": self.base, "after": head}), ["api/a.go", "worker/b.go"])

    def test_deleted_files_are_included(self) -> None:
        (self.repo / "worker/Makefile").unlink()
        head = self.commit("delete")
        self.assertEqual(changed_files(self.repo, self.base, head), ["worker/Makefile"])

    def test_missing_local_base_is_an_error(self) -> None:
        with self.assertRaises(subprocess.CalledProcessError):
            changed_files(self.repo, "does-not-exist")


class CLITests(ModuleFixture):
    def run_cli(self, *args: str, extra_environment: dict | None = None) -> subprocess.CompletedProcess:
        environment = {key: value for key, value in os.environ.items() if not key.startswith("GITHUB_")}
        environment.update(extra_environment or {})
        return subprocess.run(
            ["python3", str(Path(__file__).with_name("modules.py")), "plan", "--repo", str(self.repo), *args],
            text=True, capture_output=True, env=environment,
        )

    def test_full_plan_is_valid_json(self) -> None:
        result = self.run_cli("--all")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(json.loads(result.stdout)["has_modules"])

    def test_missing_local_base_fails_with_no_plan(self) -> None:
        result = self.run_cli()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")

    def test_github_output_is_appended_and_manual_event_selects_all(self) -> None:
        self.write("event.json", "{}")
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
        self.assertEqual(json.loads(outputs["modules"]), self.go_modules + self.npm_modules)
        self.assertEqual(len(json.loads(outputs["matrix"])["include"]), 7)


class EventTests(unittest.TestCase):
    def test_manual_and_new_branch_runs_select_all(self) -> None:
        self.assertIsNone(event_changes(Path.cwd(), "workflow_dispatch", {}))
        self.assertIsNone(event_changes(Path.cwd(), "push", {"before": "0" * 40, "after": "a" * 40}))

    @patch("modules.changed_files", return_value=["api/a.go"])
    def test_pr_uses_base_and_head_from_event(self, changed) -> None:
        repo = Path("/repo")
        result = event_changes(repo, "pull_request", {"pull_request": {"base": {"sha": "base"}, "head": {"sha": "head"}}})
        changed.assert_called_once_with(repo, "base", "head")
        self.assertEqual(result, ["api/a.go"])

    @patch("modules.changed_files", return_value=[])
    def test_merge_queue_uses_exact_diff(self, changed) -> None:
        repo = Path("/repo")
        event_changes(repo, "merge_group", {"merge_group": {"base_sha": "base", "head_sha": "head"}})
        changed.assert_called_once_with(repo, "base", "head", merge_base=False)

    @patch("modules.changed_files", side_effect=subprocess.CalledProcessError(128, "git"))
    def test_unavailable_push_base_falls_back_to_all(self, _changed) -> None:
        self.assertIsNone(event_changes(Path.cwd(), "push", {"before": "abc", "after": "def"}))

    def test_unknown_event_is_not_silently_skipped(self) -> None:
        with self.assertRaises(ValueError):
            event_changes(Path.cwd(), "unexpected", {})

    def test_invalid_pr_payload_is_not_silently_skipped(self) -> None:
        with self.assertRaises(KeyError):
            event_changes(Path.cwd(), "pull_request", {})


if __name__ == "__main__":
    unittest.main()

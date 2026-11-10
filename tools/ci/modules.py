#!/usr/bin/env python3
"""按标签或推送差异选择 CI 模块，或准备独立的 Go workspace。"""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path, PurePosixPath


@dataclass(frozen=True)
class Module:
    name: str

    def matrix_entry(self) -> dict[str, str]:
        return {
            "module": self.name,
            "go_mod": f"{self.name}/go.mod",
        }


def command(repo: Path, *args: str) -> str:
    return subprocess.run(
        args, cwd=repo, check=True, text=True, encoding="utf-8", capture_output=True,
    ).stdout


def read_modules(repo: Path) -> tuple[Module, ...]:
    modules = tuple(
        Module(name)
        for name in command(repo, "make", "--no-print-directory", "list-go-modules").split()
    )
    if not modules or len({module.name for module in modules}) != len(modules):
        raise ValueError("Makefile Go module list must be non-empty and unique")
    for module in modules:
        if not re.fullmatch(r"[a-zA-Z0-9_-]+(?:/[a-zA-Z0-9_-]+)*", module.name):
            raise ValueError(f"invalid module path: {module.name!r}")
        for name in ("go.mod", "Makefile"):
            if not (repo / module.name / name).is_file():
                raise ValueError(f"registered module is missing {module.name}/{name}")
    return modules


def go_manifests(repo: Path, modules: tuple[Module, ...]) -> dict[str, dict]:
    # The Go parser handles require/replace blocks, comments and quoted paths.
    # `go mod edit -json` only reads the manifest; it does not download dependencies.
    return {
        module.name: json.loads(command(repo / module.name, "go", "mod", "edit", "-json"))
        for module in modules
    }


def changed_files(repo: Path, base: str, head: str = "HEAD", *, merge_base: bool = True) -> list[str]:
    if merge_base:
        base = command(repo, "git", "merge-base", base, head).strip()
    # Disabling rename detection includes both sides of a cross-module move.
    paths = command(repo, "git", "diff", "--no-renames", "--name-only", "-z", base, head, "--")
    return sorted(set(path for path in paths.split("\0") if path))


def split_labels(value: str) -> list[str]:
    if not isinstance(value, str):
        raise ValueError("labels must be a comma- or whitespace-separated string")
    return value.replace(",", " ").split()


def event_labels(event_name: str, event: dict) -> list[str]:
    if event_name == "pull_request":
        labels = event["pull_request"]["labels"]
        if not isinstance(labels, list):
            raise ValueError("PR labels must be a list")
        names = []
        for label in labels:
            if not isinstance(label, dict) or not isinstance(label.get("name"), str):
                raise ValueError("every PR label must have a string name")
            names.append(label["name"])
        return names
    if event_name == "workflow_dispatch":
        return split_labels(event["inputs"]["labels"])
    raise ValueError(f"unsupported CI event: {event_name!r}")


def module_plan(modules: tuple[Module, ...], selected: set[str]) -> dict:
    entries = [module.matrix_entry() for module in modules if module.name in selected]
    return {
        "modules": [entry["module"] for entry in entries],
        "matrix": {"include": entries},
        "has_modules": bool(entries),
    }


def select(repo: Path, labels: list[str]) -> dict:
    """Only explicitly labelled modules run: no paths, dependency or all fallback."""
    modules = read_modules(repo)
    known = {module.name for module in modules}
    if any(not isinstance(label, str) for label in labels):
        raise ValueError("label names must be strings")
    ci_labels = {label for label in labels if label.startswith("ci:")}
    if not ci_labels:
        raise ValueError("add at least one ci:<module> label, or ci:none to explicitly skip module jobs")
    unknown = ci_labels - {f"ci:{name}" for name in known} - {"ci:none"}
    if unknown:
        raise ValueError(f"unknown CI label(s): {', '.join(sorted(unknown))}")
    if "ci:none" in ci_labels and len(ci_labels) > 1:
        raise ValueError("ci:none cannot be combined with module labels")
    selected = {label.removeprefix("ci:") for label in ci_labels if label != "ci:none"}
    return {"labels": sorted(ci_labels), **module_plan(modules, selected)}


def is_documentation(path: str) -> bool:
    return path.startswith("docs/") or PurePosixPath(path).suffix.lower() in {".md", ".rst", ".adoc"} or path == "LICENSE"


def select_changed(repo: Path, paths: list[str]) -> dict:
    """按文件所属目录选择直接变化的 Go 模块。"""
    modules = read_modules(repo)
    selected: set[str] = set()
    module_roots = sorted(modules, key=lambda item: -len(item.name))
    for path in paths:
        path = path.removeprefix("./")
        if is_documentation(path):
            continue
        owner = next((module.name for module in module_roots
                      if path.startswith(module.name + "/")), None)
        if owner:
            selected.add(owner)
    return {"changed_files": paths, **module_plan(modules, selected)}


def select_push(repo: Path, event: dict) -> dict:
    """按完整推送差异选择模块，无法确定旧版本时检查全部模块。"""
    modules = read_modules(repo)
    if event.get("deleted") is True:
        return {"reason": "deleted_branch", **module_plan(modules, set())}
    before, after = event["before"], event["after"]
    for revision in (before, after):
        if not isinstance(revision, str) or not re.fullmatch(r"[0-9a-f]{40}|[0-9a-f]{64}", revision):
            raise ValueError("push 的 before 和 after 必须是完整的提交 SHA")
    if not after.strip("0"):
        raise ValueError("push 的 after 必须指向提交")
    command(repo, "git", "rev-parse", "--verify", f"{after}^{{commit}}")
    all_modules = {module.name for module in modules}
    if not before.strip("0"):
        return {"reason": "new_branch", **module_plan(modules, all_modules)}
    try:
        command(repo, "git", "rev-parse", "--verify", f"{before}^{{commit}}")
    except subprocess.CalledProcessError:
        return {"reason": "missing_base", **module_plan(modules, all_modules)}
    paths = changed_files(repo, before, after, merge_base=False)
    shared_files = {"Makefile", ".golangci.yml", ".conventional-commits.json", "tools/devtools/toolchains.json"}
    shared_prefixes = (".github/workflows/", "tools/make/", "tools/ci/")
    if any(not is_documentation(path) and (path in shared_files or path.startswith(shared_prefixes)) for path in paths):
        return {"reason": "shared_ci", "changed_files": paths, **module_plan(modules, all_modules)}
    return {"reason": "push_diff", **select_changed(repo, paths)}


def event_plan(repo: Path, event_name: str, event: dict) -> dict:
    if event_name == "push":
        return select_push(repo, event)
    return select(repo, event_labels(event_name, event))


def write_workspace(repo: Path) -> Path:
    modules = read_modules(repo)
    manifests = go_manifests(repo, modules)
    version = max(
        (data["Go"] for data in manifests.values()),
        key=lambda value: tuple(int(part) for part in value.split(".")),
    )
    # Never create or overwrite the developer's own root go.work.
    target = repo / ".run/ci/go.work"
    target.parent.mkdir(parents=True, exist_ok=True)
    uses = "".join(f"\t{json.dumps(str((repo / name).resolve()))}\n" for name in manifests)
    content = f"go {version}\n\nuse (\n{uses})\n"
    if not target.exists() or target.read_text(encoding="utf-8") != content:
        with tempfile.NamedTemporaryFile(mode="w", encoding="utf-8", dir=target.parent, delete=False) as temporary:
            temporary.write(content)
        Path(temporary.name).replace(target)
    return target


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("plan", "workspace"))
    parser.add_argument("--repo", type=Path, default=Path.cwd())
    group = parser.add_mutually_exclusive_group()
    group.add_argument("--labels", help="explicit ci:<module> labels, separated by commas or whitespace")
    group.add_argument("--base-ref")
    parser.add_argument("--head-ref", default="HEAD")
    args = parser.parse_args()
    repo = args.repo.resolve()
    try:
        if args.action == "workspace":
            print(write_workspace(repo))
            return 0
        if args.labels is not None:
            plan = select(repo, split_labels(args.labels))
        elif args.base_ref:
            paths = changed_files(repo, args.base_ref, args.head_ref)
            plan = select_changed(repo, paths)
        elif os.environ.get("GOPULSE_CI_LABELS"):
            plan = select(repo, split_labels(os.environ["GOPULSE_CI_LABELS"]))
        elif os.environ.get("GITHUB_EVENT_PATH"):
            event = json.loads(Path(os.environ["GITHUB_EVENT_PATH"]).read_text(encoding="utf-8"))
            plan = event_plan(repo, os.environ["GITHUB_EVENT_NAME"], event)
        else:
            raise ValueError("supply LABELS locally, for example LABELS=ci:backend/business")
        print(json.dumps(plan, ensure_ascii=False))
        if os.environ.get("GITHUB_OUTPUT"):
            with Path(os.environ["GITHUB_OUTPUT"]).open("a", encoding="utf-8") as output:
                for name in ("matrix", "modules", "has_modules"):
                    output.write(f"{name}={json.dumps(plan[name], separators=(',', ':'))}\n")
        return 0
    except (OSError, ValueError, KeyError, subprocess.CalledProcessError) as exc:
        print(f"CI planning failed: {exc}", file=sys.stderr)
        if isinstance(exc, subprocess.CalledProcessError) and exc.stderr:
            print(exc.stderr, file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())

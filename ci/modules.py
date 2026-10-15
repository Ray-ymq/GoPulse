#!/usr/bin/env python3
"""Plan module CI from Git changes, or prepare an isolated local Go workspace."""

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
    kind: str

    def matrix_entry(self) -> dict[str, str]:
        return {
            "module": self.name,
            "kind": self.kind,
            # Frontend dependency preparation builds the native development helper.
            "go_mod": f"{self.name if self.kind == 'go' else 'devtools'}/go.mod",
        }


def command(repo: Path, *args: str) -> str:
    return subprocess.run(
        args, cwd=repo, check=True, text=True, encoding="utf-8", capture_output=True,
    ).stdout


def read_modules(repo: Path) -> tuple[Module, ...]:
    modules = tuple(
        Module(name, kind)
        for kind, target in (("go", "list-go-modules"), ("npm", "list-npm-modules"))
        for name in command(repo, "make", "--no-print-directory", target).split()
    )
    if not modules or len({module.name for module in modules}) != len(modules):
        raise ValueError("Makefile module lists must be non-empty and unique")
    for module in modules:
        if not re.fullmatch(r"[a-zA-Z0-9_-]+(?:/[a-zA-Z0-9_-]+)*", module.name):
            raise ValueError(f"invalid module path: {module.name!r}")
        manifest = "go.mod" if module.kind == "go" else "package.json"
        for name in (manifest, "Makefile"):
            if not (repo / module.name / name).is_file():
                raise ValueError(f"registered module is missing {module.name}/{name}")
    return modules


def go_manifests(repo: Path, modules: tuple[Module, ...]) -> dict[str, dict]:
    # The Go parser handles require/replace blocks, comments and quoted paths.
    # `go mod edit -json` only reads the manifest; it does not download dependencies.
    return {
        module.name: json.loads(command(repo / module.name, "go", "mod", "edit", "-json"))
        for module in modules if module.kind == "go"
    }


def consumers(repo: Path, modules: tuple[Module, ...], manifests: dict[str, dict]) -> dict[str, set[str]]:
    graph = {module.name: set() for module in modules}
    identities = {data["Module"]["Path"]: name for name, data in manifests.items()}
    roots = {(repo / name).resolve(): name for name in manifests}
    for name, data in manifests.items():
        for dependency in data.get("Require") or []:
            if dependency["Path"] in identities:
                graph[identities[dependency["Path"]]].add(name)
        for replacement in data.get("Replace") or []:
            new = replacement["New"]
            if not new.get("Version"):
                dependency = roots.get((repo / name / new["Path"]).resolve())
                if dependency:
                    graph[dependency].add(name)
    # Both frontends invoke devtools through their Makefile deps target.
    if "devtools" in graph:
        graph["devtools"].update(module.name for module in modules if module.kind == "npm")
    return graph


def changed_files(repo: Path, base: str, head: str = "HEAD", *, merge_base: bool = True) -> list[str]:
    if merge_base:
        base = command(repo, "git", "merge-base", base, head).strip()
    # Disabling rename detection includes both sides of a cross-module move.
    paths = command(repo, "git", "diff", "--no-renames", "--name-only", "-z", base, head, "--")
    return sorted(set(path for path in paths.split("\0") if path))


def event_changes(repo: Path, event_name: str, event: dict) -> list[str] | None:
    if event_name == "workflow_dispatch":
        return None  # Manual runs always verify every registered module.
    if event_name == "pull_request":
        pr = event["pull_request"]
        return changed_files(repo, pr["base"]["sha"], pr["head"]["sha"])
    if event_name == "merge_group":
        group = event["merge_group"]
        return changed_files(repo, group["base_sha"], group["head_sha"], merge_base=False)
    if event_name == "push":
        before, after = event["before"], event["after"]
        if not before.strip("0"):
            return None
        try:
            return changed_files(repo, before, after, merge_base=False)
        except subprocess.CalledProcessError:
            # A force-push can leave `before` unavailable. Never turn this into
            # an empty/green matrix: fall back to checking every module.
            print("Push base unavailable; selecting all modules", file=sys.stderr)
            return None
    raise ValueError(f"unsupported CI event: {event_name!r}")


def select(repo: Path, paths: list[str] | None) -> dict:
    modules = read_modules(repo)
    manifests = go_manifests(repo, modules)
    graph = consumers(repo, modules, manifests)
    selected: set[str] = set()
    reasons: list[str] = []
    module_roots = sorted(modules, key=lambda item: -len(item.name))
    for path in paths or []:
        path = path.removeprefix("./")
        if path.startswith("docs/") or PurePosixPath(path).suffix.lower() in {".md", ".rst", ".adoc"} or path == "LICENSE":
            continue
        if path.startswith("frontend-shared/"):
            selected.update(module.name for module in modules if module.kind == "npm")
            continue
        owner = next((module.name for module in module_roots
                      if path.startswith(module.name + "/")), None)
        if owner:
            selected.add(owner)
        else:
            # Root Make/config, CI, deployment files and new unknown source
            # directories affect the whole repository until explicitly scoped.
            selected.update(module.name for module in modules)
            reasons.append(f"{path}: shared or unclassified file; checking all modules")
    if paths is None:
        selected.update(module.name for module in modules)
        reasons.append("full verification requested")
    pending = list(selected)
    while pending:
        for consumer in graph[pending.pop()]:
            if consumer not in selected:
                selected.add(consumer)
                pending.append(consumer)
    entries = [module.matrix_entry() for module in modules if module.name in selected]
    return {
        "changed_files": paths,
        "modules": [entry["module"] for entry in entries],
        "matrix": {"include": entries},
        "has_modules": bool(entries),
        "reasons": reasons,
    }


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
    group.add_argument("--base-ref")
    group.add_argument("--all", action="store_true")
    parser.add_argument("--head-ref", default="HEAD")
    args = parser.parse_args()
    repo = args.repo.resolve()
    try:
        if args.action == "workspace":
            print(write_workspace(repo))
            return 0
        if args.all:
            paths = None
        elif args.base_ref:
            paths = changed_files(repo, args.base_ref, args.head_ref)
        elif os.environ.get("GITHUB_EVENT_PATH"):
            event = json.loads(Path(os.environ["GITHUB_EVENT_PATH"]).read_text(encoding="utf-8"))
            paths = event_changes(repo, os.environ["GITHUB_EVENT_NAME"], event)
        else:
            raise ValueError("supply BASE_REF or ALL=1 when planning locally")
        plan = select(repo, paths)
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

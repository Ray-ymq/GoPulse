#!/usr/bin/env python3
"""Validate GoPulse branch names, authoritative version allocation, and update scope."""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Iterable

DEVELOP_BRANCH = re.compile(r"^develop/(?P<version>[0-9]+\.[0-9]+\.[0-9]+)$")
ALLOCATION_ROW = re.compile(
    r"^\|\s*(?P<batch>Phase-[0-9]{2}-[0-9]{2})\s*\|\s*`(?P<version>[0-9]+\.[0-9]+\.[0-9]+)`\s*"
    r"\|\s*`(?P<branch>develop/[0-9]+\.[0-9]+\.[0-9]+)`\s*\|"
)
UPDATE_ROOT_FILES = {
    ".editorconfig",
    ".gitattributes",
    ".gitignore",
    "AGENTS.md",
    "README.md",
}
UPDATE_PREFIXES = (".github/", "dev/", "docs/", "scripts/ci/")


@dataclass(frozen=True)
class Allocation:
    batch: str
    version: str
    branch: str
    plan: Path


def load_allocations(repo: Path) -> list[Allocation]:
    allocations: list[Allocation] = []
    plans = sorted((repo / "dev" / "imple").glob("Phase-*/Phase-*-总实施方案.md"))
    for plan in plans:
        for line in plan.read_text(encoding="utf-8").splitlines():
            match = ALLOCATION_ROW.match(line)
            if match:
                allocations.append(
                    Allocation(
                        batch=match.group("batch"),
                        version=match.group("version"),
                        branch=match.group("branch"),
                        plan=plan.relative_to(repo),
                    )
                )
    return allocations


def changed_files(repo: Path, base_ref: str) -> list[str]:
    result = subprocess.run(
        ["git", "-c", "core.quotepath=false", "diff", "--name-only", "-z", f"{base_ref}...HEAD"],
        cwd=repo,
        check=True,
        text=True,
        encoding="utf-8",
        capture_output=True,
    )
    return [path.replace("\\", "/") for path in result.stdout.split("\0") if path]


def update_path_allowed(path: str) -> bool:
    normalized = path.replace("\\", "/")
    while normalized.startswith("./"):
        normalized = normalized[2:]
    if normalized in UPDATE_ROOT_FILES:
        return True
    if normalized.startswith(UPDATE_PREFIXES):
        return True
    # Planning documents may be added at repository root, but product VERSION may not change on update.
    return "/" not in normalized and normalized.lower().endswith(".md")


def validate(repo: Path, branch: str, base_ref: str | None, supplied_changes: Iterable[str]) -> list[str]:
    errors: list[str] = []
    supplied = [path.replace("\\", "/") for path in supplied_changes]

    if branch == "update":
        files = supplied if supplied else (changed_files(repo, base_ref) if base_ref else [])
        if not files:
            errors.append("update validation requires --base-ref or at least one --changed-file")
            return errors
        forbidden = sorted(path for path in files if not update_path_allowed(path))
        if forbidden:
            errors.append("update contains files outside the planning-only scope: " + ", ".join(forbidden))
        return errors

    branch_match = DEVELOP_BRANCH.fullmatch(branch)
    if not branch_match:
        return ["branch must be exactly update or match develop/x.x.x"]

    allocations = [item for item in load_allocations(repo) if item.branch == branch]
    if len(allocations) != 1:
        errors.append(f"{branch} must map to exactly one authoritative Phase allocation; found {len(allocations)}")
        return errors

    allocation = allocations[0]
    branch_version = branch_match.group("version")
    if allocation.version != branch_version:
        errors.append(
            f"branch version {branch_version} does not match {allocation.batch} target {allocation.version} in {allocation.plan}"
        )

    version_file = repo / "VERSION"
    if not version_file.is_file():
        errors.append("VERSION is required for a completed development batch")
    else:
        completed_version = version_file.read_text(encoding="utf-8").strip()
        if completed_version != branch_version:
            errors.append(f"VERSION is {completed_version!r}; expected {branch_version!r} for {branch}")

    return errors


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo", type=Path, default=Path.cwd())
    parser.add_argument("--branch", required=True)
    parser.add_argument("--base-ref")
    parser.add_argument("--changed-file", action="append", default=[])
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    repo = args.repo.resolve()
    try:
        errors = validate(repo, args.branch, args.base_ref, args.changed_file)
    except (OSError, subprocess.CalledProcessError) as exc:
        print(f"branch governance validation could not run: {exc}", file=sys.stderr)
        return 2
    if errors:
        for error in errors:
            print(f"ERROR: {error}", file=sys.stderr)
        return 1
    print(f"Branch governance passed for {args.branch}.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

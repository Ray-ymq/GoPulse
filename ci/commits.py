#!/usr/bin/env python3
"""Check new commit messages and PR titles against the repository's commit policy."""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path


HEADER = re.compile(r"(?P<type>[a-z]+)(?:\((?P<scope>[^\s()!]+)\))?!?: (?P<description>\S(?:.*\S)?)")
EXAMPLE = "ci: rebuild module-based checks"


@dataclass(frozen=True)
class Policy:
    types: tuple[str, ...]
    legacy_ref: str


def git(repo: Path, *args: str, input_text: str | None = None) -> str:
    return subprocess.run(
        ["git", *args], cwd=repo, input=input_text, text=True, encoding="utf-8",
        capture_output=True, check=True,
    ).stdout


def read_policy(repo: Path) -> Policy:
    data = json.loads((repo / ".conventional-commits.json").read_text(encoding="utf-8"))
    types = data["types"]
    legacy_ref = data["legacy_ref"]
    if (
        not isinstance(types, list) or not types
        or any(not isinstance(value, str) or not re.fullmatch("[a-z]+", value) for value in types)
        or len(set(types)) != len(types)
    ):
        raise ValueError("commit policy types must be unique lowercase words")
    if not isinstance(legacy_ref, str) or not re.fullmatch(r"[0-9a-f]{40}|[0-9a-f]{64}", legacy_ref):
        raise ValueError("commit policy legacy_ref must be a full commit SHA")
    return Policy(tuple(types), legacy_ref)


def validate_title(title: str, policy: Policy) -> list[str]:
    if "\n" in title or "\r" in title:
        return ["title must be a single line"]
    match = HEADER.fullmatch(title)
    if not match:
        return ["expected <type>(<optional-scope>)!: <description>; scope and ! are optional"]
    if match["type"] not in policy.types:
        return [f"unsupported type {match['type']!r}; allowed: {', '.join(policy.types)}"]
    return []


def validate_message(message: str, policy: Policy) -> list[str]:
    lines = message.splitlines()
    errors = validate_title(lines[0] if lines else "", policy)
    if len(lines) > 1 and lines[1].strip():
        errors.append("separate the title and body with a blank line")
    for line in lines[2:]:
        if re.match(r"breaking[ -]change:", line, flags=re.IGNORECASE):
            if not re.match(r"BREAKING[ -]CHANGE: \S", line):
                errors.append("use uppercase BREAKING CHANGE: <description> (or BREAKING-CHANGE)")
    return errors


def commit_ids(repo: Path, policy: Policy, base: str | None = None, head: str = "HEAD") -> list[str]:
    # Resolve every ref separately so a missing/shallow baseline cannot silently
    # turn an invalid range into an empty, successful check.
    head_sha = git(repo, "rev-parse", "--verify", f"{head}^{{commit}}").strip()
    legacy_sha = git(repo, "rev-parse", "--verify", f"{policy.legacy_ref}^{{commit}}").strip()
    excluded = [legacy_sha]
    if base:
        excluded.append(git(repo, "rev-parse", "--verify", f"{base}^{{commit}}").strip())
    return git(repo, "rev-list", "--reverse", "--no-merges", head_sha, "--not", *excluded).split()


def event_range(repo: Path, event_name: str, event: dict) -> tuple[str | None, str, str | None]:
    if event_name == "pull_request":
        pr = event["pull_request"]
        return pr["base"]["sha"], pr["head"]["sha"], pr["title"]
    if event_name == "merge_group":
        group = event["merge_group"]
        return group["base_sha"], group["head_sha"], None
    if event_name == "push":
        base, head = event["before"], event["after"]
        if not base.strip("0"):
            return None, head, None
        try:
            git(repo, "rev-parse", "--verify", f"{base}^{{commit}}")
        except subprocess.CalledProcessError:
            # Force-pushes must not evade validation when the old SHA vanished.
            # In that case check all reachable commits since policy adoption.
            return None, head, None
        return base, head, None
    if event_name == "workflow_dispatch":
        return None, "HEAD", None
    raise ValueError(f"unsupported commit-check event: {event_name!r}")


def check_file(repo: Path, path: Path, policy: Policy) -> list[str]:
    merge_head = Path(git(repo, "rev-parse", "--git-path", "MERGE_HEAD").strip())
    if not merge_head.is_absolute():
        merge_head = repo / merge_head
    if merge_head.is_file():
        print("Merge commit: message format is exempt")
        return []
    # Use Git's own cleanup rules, including core.commentChar, for editor hints.
    message = git(repo, "stripspace", "--strip-comments", input_text=path.read_text(encoding="utf-8"))
    return validate_message(message, policy)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", type=Path, default=Path.cwd())
    group = parser.add_mutually_exclusive_group()
    group.add_argument("--message-file", type=Path)
    group.add_argument("--base-ref")
    parser.add_argument("--head-ref", default="HEAD")
    args = parser.parse_args()
    repo = args.repo.resolve()
    try:
        policy = read_policy(repo)
        if args.message_file:
            errors = check_file(repo, args.message_file, policy)
        else:
            base, head, title = args.base_ref, args.head_ref, None
            if not base and os.environ.get("GITHUB_EVENT_PATH"):
                event = json.loads(Path(os.environ["GITHUB_EVENT_PATH"]).read_text(encoding="utf-8"))
                base, head, title = event_range(repo, os.environ["GITHUB_EVENT_NAME"], event)
            errors = [f"PR title: {error}" for error in validate_title(title, policy)] if title is not None else []
            revisions = commit_ids(repo, policy, base, head)
            for revision in revisions:
                message = git(repo, "show", "--no-patch", "--format=%B", revision)
                errors.extend(f"{revision[:12]}: {error}" for error in validate_message(message, policy))
            print(f"Checked {len(revisions)} new non-merge commit(s)")
        if errors:
            for error in errors:
                print(f"Commit format error: {error}", file=sys.stderr)
            print(f"Example: {EXAMPLE}", file=sys.stderr)
            return 1
        print("Conventional Commits checks passed")
        return 0
    except (OSError, ValueError, KeyError, subprocess.CalledProcessError) as exc:
        print(f"Commit validation failed: {exc}", file=sys.stderr)
        if isinstance(exc, subprocess.CalledProcessError) and exc.stderr:
            print(exc.stderr, file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
